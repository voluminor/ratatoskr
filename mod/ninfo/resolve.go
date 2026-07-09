package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
)

// // // // // // // // // //

var (
	// 64 hex chars
	reHexKey = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	// [ip6]:port
	reBracketIPv6 = regexp.MustCompile(`^\[([0-9a-fA-F:]+)\]:\d{1,5}$`)
	// bare ipv6 (must contain at least one colon)
	reBareIPv6 = regexp.MustCompile(`^[0-9a-fA-F]*:[0-9a-fA-F:]*$`)
)

// // // // // // // // // //

func (obj *Obj) resolveAddr(ctx context.Context, addr string) (ed25519.PublicKey, error) {
	if obj.isClosed(obj.ctx) {
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

const (
	maxLookupInterval = time.Second
	// maxConcurrentAddrLookups bounds distinct in-flight address lookups; each
	// detached flight polls SendLookup for up to MaxLookupTime, so a flood of
	// unique addresses must not accumulate goroutines unbounded.
	maxConcurrentAddrLookups = 256
	// Negative cache: repeated lookups of a recently-unresolvable address return
	// fast instead of respawning a polling flight.
	negativeAddrTTL        = 2 * time.Second
	maxNegativeAddrEntries = 1024
)

// //

func (obj *Obj) resolveIPv6(ctx context.Context, addr string) (ed25519.PublicKey, error) {
	if obj.isClosed(obj.ctx) {
		return nil, ErrClosed
	}
	ctx = ensureCallerContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ip := extractIPv6(addr)
	if ip == nil {
		return nil, ErrInvalidAddr
	}

	if key := obj.findKeyByIP(ip); key != nil {
		return key, nil
	}

	// Derive partial key from IPv6 and trigger network lookup
	var yggIP yggaddr.Address
	copy(yggIP[:], ip.To16())
	if !yggIP.IsValid() {
		return nil, fmt.Errorf("%w: not a yggdrasil address", ErrInvalidAddr)
	}

	partial := yggIP.GetKey()
	flightKey := string(yggIP[:])
	flight, lead, err := obj.joinAddrLookup(flightKey)
	if err != nil {
		return nil, err
	}
	if lead {
		// The shared lookup runs detached under the node lifetime, so a leader
		// with a short deadline cannot abort work the other waiters still need;
		// each caller observes its own ctx in waitAddrLookup.
		go func() {
			key, err := obj.runAddrLookup(ip, partial)
			obj.finishAddrLookup(flightKey, flight, key, err)
		}()
	}
	return obj.waitAddrLookup(ctx, flight)
}

func (obj *Obj) runAddrLookup(ip net.IP, partial ed25519.PublicKey) (ed25519.PublicKey, error) {
	// Bounded by the node lifetime plus MaxLookupTime (always > 0 after New),
	// not by any single caller.
	lookupCtx, cancel := context.WithTimeout(obj.ctx, obj.maxLookupTime)
	defer cancel()

	lookupInterval := obj.lookupInterval
	if lookupInterval <= 0 {
		lookupInterval = defaultLookupInterval
	}
	timer := time.NewTimer(lookupInterval)
	defer timer.Stop()

	obj.source.SendLookup(partial)
	for {
		select {
		case <-lookupCtx.Done():
			if obj.ctx.Err() != nil {
				return nil, ErrClosed
			}
			return nil, ErrUnresolvableAddr
		case <-timer.C:
			if key := obj.findKeyByIP(ip); key != nil {
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

func (obj *Obj) joinAddrLookup(key string) (*addrLookupFlightObj, bool, error) {
	obj.lookupMu.Lock()
	defer obj.lookupMu.Unlock()
	if obj.lookupFlights == nil {
		obj.lookupFlights = make(map[string]*addrLookupFlightObj)
	}
	if flight := obj.lookupFlights[key]; flight != nil {
		return flight, false, nil
	}
	if exp, ok := obj.unresolvedAddrs[key]; ok {
		if time.Now().Before(exp) {
			return nil, false, ErrUnresolvableAddr
		}
		delete(obj.unresolvedAddrs, key)
	}
	if len(obj.lookupFlights) >= maxConcurrentAddrLookups {
		return nil, false, ErrLookupBusy
	}
	flight := &addrLookupFlightObj{done: make(chan struct{})}
	obj.lookupFlights[key] = flight
	return flight, true, nil
}

func (obj *Obj) finishAddrLookup(key string, flight *addrLookupFlightObj, result ed25519.PublicKey, err error) {
	obj.lookupMu.Lock()
	if obj.lookupFlights[key] == flight {
		delete(obj.lookupFlights, key)
	}
	if err != nil && errors.Is(err, ErrUnresolvableAddr) {
		obj.rememberUnresolvedLocked(key)
	}
	flight.key = result
	flight.err = err
	close(flight.done)
	obj.lookupMu.Unlock()
}

// rememberUnresolvedLocked records a recently-unresolvable address for the
// negative cache; caller holds lookupMu. Best-effort bounded eviction.
func (obj *Obj) rememberUnresolvedLocked(key string) {
	if obj.unresolvedAddrs == nil {
		obj.unresolvedAddrs = make(map[string]time.Time)
	}
	if len(obj.unresolvedAddrs) >= maxNegativeAddrEntries {
		for k := range obj.unresolvedAddrs {
			delete(obj.unresolvedAddrs, k)
			break
		}
	}
	obj.unresolvedAddrs[key] = time.Now().Add(negativeAddrTTL)
}

func (obj *Obj) waitAddrLookup(ctx context.Context, flight *addrLookupFlightObj) (ed25519.PublicKey, error) {
	select {
	case <-obj.ctx.Done():
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.key, flight.err
	}
}

// //

func (obj *Obj) findKeyByIP(ip net.IP) ed25519.PublicKey {
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
	for _, p := range obj.source.GetPaths() {
		if matchYggAddr(p.Key, ip) {
			return p.Key
		}
	}
	return nil
}

// //

func extractIPv6(addr string) net.IP {
	// try [ip6]:port
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return net.ParseIP(host)
	}
	// try bare ip6
	return net.ParseIP(addr)
}

// //

func matchYggAddr(key ed25519.PublicKey, target net.IP) bool {
	a := yggaddr.AddrForKey(key)
	if a == nil {
		return false
	}
	return net.IP(a[:]).Equal(target)
}
