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

const maxLookupInterval = time.Second

// //

// resolveIPv6 maps a yggdrasil IPv6 to a public key. It first scans known peers,
// sessions and paths; on a miss it triggers a network lookup and polls in the
// caller's goroutine until a match appears, the caller context ends, or the
// module closes. When the caller sets no deadline the poll is bounded by
// MaxLookupTime so a lookup for an offline node cannot run forever.
func (obj *Obj) resolveIPv6(ctx context.Context, addr string) (ed25519.PublicKey, error) {
	if obj.isClosed(obj.ctx) {
		return nil, ErrClosed
	}
	callerCtx := ensureCallerContext(ctx)
	if err := callerCtx.Err(); err != nil {
		return nil, err
	}
	ip := extractIPv6(addr)
	if ip == nil {
		return nil, ErrInvalidAddr
	}

	if key := obj.findKeyByIP(ip); key != nil {
		return key, nil
	}

	// Derive partial key from IPv6 and trigger network lookup.
	var yggIP yggaddr.Address
	copy(yggIP[:], ip.To16())
	if !yggIP.IsValid() {
		return nil, fmt.Errorf("%w: not a yggdrasil address", ErrInvalidAddr)
	}
	partial := yggIP.GetKey()

	pollCtx := callerCtx
	if _, ok := callerCtx.Deadline(); !ok && obj.maxLookupTime > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(callerCtx, obj.maxLookupTime)
		defer cancel()
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
		case <-obj.ctx.Done():
			return nil, ErrClosed
		case <-pollCtx.Done():
			// A caller-supplied deadline surfaces as its own error; the internal
			// MaxLookupTime bound surfaces as an unresolvable address.
			if err := callerCtx.Err(); err != nil {
				return nil, err
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
