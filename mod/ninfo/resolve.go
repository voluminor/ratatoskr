package ninfo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
)

// // // // // // // // // //

var (
	reHexKey      = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	reBracketIPv6 = regexp.MustCompile(`^\[(?:[0-9a-fA-F:]+)\]:\d{1,5}$`)
	reBareIPv6    = regexp.MustCompile(`^[0-9a-fA-F]*:[0-9a-fA-F:]*$`)
)

// // // // // // // // // //

func (obj *Obj) resolveAddr(ctx context.Context, addr string) (ed25519.PublicKey, error) {
	if obj.isClosed() {
		return nil, ErrClosed
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, ErrInvalidAddr
	}

	if key, matched, err := parsePkYggCandidate(addr); matched {
		return key, err
	}

	switch {
	case reHexKey.MatchString(addr):
		return parseHexKey(addr)
	case reBracketIPv6.MatchString(addr), reBareIPv6.MatchString(addr):
		return obj.resolveIPv6(ctx, addr)
	}

	return nil, fmt.Errorf("%w: %q", ErrInvalidAddr, addr)
}

// // // // // // // // // //

func parsePkYggCandidate(addr string) (ed25519.PublicKey, bool, error) {
	key, matched, err := common.ParsePublicKeyDomain(addr)
	if err != nil {
		if errors.Is(err, common.ErrInvalidPublicKeyLength) {
			return nil, true, ErrInvalidKeyLength
		}
		return nil, true, fmt.Errorf("%w: %v", ErrInvalidAddr, err)
	}
	if !matched {
		return nil, false, nil
	}
	return key, true, nil
}

// //

func parseHexKey(s string) (ed25519.PublicKey, error) {
	if len(s) != ed25519.PublicKeySize*2 {
		return nil, ErrInvalidKeyLength
	}
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidAddr, err)
	}
	return key, nil
}

// //

const maxLookupInterval = time.Second

// //

func (obj *Obj) resolveIPv6(ctx context.Context, addr string) (ed25519.PublicKey, error) {
	callerCtx := ensureCallerContext(ctx)
	if err := callerCtx.Err(); err != nil {
		return nil, err
	}
	ip := extractIPv6(addr)
	if ip == nil {
		return nil, ErrInvalidAddr
	}
	canonical, ok := netip.AddrFromSlice(ip)
	if !ok || !canonical.Is6() {
		return nil, ErrInvalidAddr
	}
	canonical = canonical.Unmap()
	partial, lookupAddr, ok := yggLookupKey(canonical)
	if !ok {
		return nil, fmt.Errorf("%w: not a yggdrasil address", ErrInvalidAddr)
	}
	canonical = lookupAddr

	obj.askMu.Lock()
	if obj.closedLocked() {
		obj.askMu.Unlock()
		return nil, ErrClosed
	}
	if obj.resolveFlights == nil {
		obj.resolveFlights = make(map[netip.Addr]*resolveFlightObj)
	}
	if flight := obj.resolveFlights[canonical]; flight != nil {
		obj.askMu.Unlock()
		return waitResolveFlight(callerCtx, flight)
	}
	if len(obj.resolveFlights) >= maxConcurrentResolves {
		obj.askMu.Unlock()
		return nil, ErrResolveBusy
	}
	flight := &resolveFlightObj{done: make(chan struct{})}
	obj.resolveFlights[canonical] = flight
	tasks := obj.tasks
	obj.askMu.Unlock()
	if !tasks.Go(func(context.Context) { obj.runResolveFlight(canonical, partial, flight) }) {
		obj.finishResolveFlight(canonical, flight, ErrClosed)
	}
	return waitResolveFlight(callerCtx, flight)
}

func waitResolveFlight(ctx context.Context, flight *resolveFlightObj) (ed25519.PublicKey, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		if flight.err != nil {
			return nil, flight.err
		}
		return append(ed25519.PublicKey(nil), flight.key...), nil
	}
}

func (obj *Obj) finishResolveFlight(addr netip.Addr, flight *resolveFlightObj, err error) {
	if err != nil {
		flight.err = err
	}
	obj.askMu.Lock()
	if obj.resolveFlights[addr] == flight {
		delete(obj.resolveFlights, addr)
	}
	obj.askMu.Unlock()
	close(flight.done)
}

func (obj *Obj) runResolveFlight(addr netip.Addr, partial ed25519.PublicKey, flight *resolveFlightObj) {
	defer obj.finishResolveFlight(addr, flight, nil)

	workCtx := obj.tasks.Context()
	cancel := func() {}
	if obj.maxLookupTime > 0 {
		workCtx, cancel = context.WithTimeout(workCtx, obj.maxLookupTime)
	}
	defer cancel()
	flight.key, flight.err = obj.resolveIPv6Flight(workCtx, addr, partial)
}

func (obj *Obj) resolveIPv6Flight(ctx context.Context, addr netip.Addr, partial ed25519.PublicKey) (ed25519.PublicKey, error) {
	ip := net.IP(addr.AsSlice())
	if key := obj.findKeyByIP(ip, true); key != nil {
		return key, nil
	}

	lookupInterval := obj.lookupInterval
	if lookupInterval <= 0 {
		lookupInterval = defaultLookupInterval
	}
	timer := time.NewTimer(lookupInterval)
	defer timer.Stop()

	obj.source.SendLookup(partial)
	for {
		select {
		case <-obj.tasks.Context().Done():
			return nil, ErrClosed
		case <-ctx.Done():
			if obj.tasks.Context().Err() != nil {
				return nil, ErrClosed
			}
			return nil, ErrUnresolvableAddr
		case <-timer.C:
			if key := obj.findKeyByIP(ip, false); key != nil {
				return key, nil
			}
			obj.source.SendLookup(partial)
			if lookupInterval < maxLookupInterval {
				lookupInterval *= 2
				if lookupInterval > maxLookupInterval {
					lookupInterval = maxLookupInterval
				}
			}
			timer.Reset(lookupInterval)
		}
	}
}

// //

func (obj *Obj) findKeyByIP(ip net.IP, includeDirect bool) ed25519.PublicKey {
	if includeDirect {
		for _, p := range obj.source.GetPeers() {
			if matchYggAddr(p.Key, ip) {
				return p.Key
			}
		}
		for _, s := range obj.source.GetSessions() {
			if matchYggAddr(s.Key, ip) {
				return s.Key
			}
		}
	}
	for _, p := range obj.source.GetPaths() {
		if matchYggAddr(p.Key, ip) {
			return p.Key
		}
	}
	return nil
}

func yggLookupKey(addr netip.Addr) (ed25519.PublicKey, netip.Addr, bool) {
	raw := addr.As16()
	var nodeAddr yggaddr.Address
	copy(nodeAddr[:], raw[:])
	if nodeAddr.IsValid() {
		return nodeAddr.GetKey(), addr, true
	}
	var subnet yggaddr.Subnet
	copy(subnet[:], raw[:len(subnet)])
	if !subnet.IsValid() {
		return nil, netip.Addr{}, false
	}
	canonical := raw
	clear(canonical[len(subnet):])
	return subnet.GetKey(), netip.AddrFrom16(canonical), true
}

// //

func extractIPv6(addr string) net.IP {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(addr)
}

// //

func matchYggAddr(key ed25519.PublicKey, target net.IP) bool {
	target16 := target.To16()
	if target16 == nil {
		return false
	}
	var nodeAddr yggaddr.Address
	copy(nodeAddr[:], target16)
	if nodeAddr.IsValid() {
		derived := yggaddr.AddrForKey(key)
		return derived != nil && bytes.Equal(derived[:], target16)
	}
	var subnet yggaddr.Subnet
	copy(subnet[:], target16[:len(subnet)])
	if !subnet.IsValid() {
		return false
	}
	derived := yggaddr.SubnetForKey(key)
	return derived != nil && bytes.Equal(derived[:], target16[:len(subnet)])
}
