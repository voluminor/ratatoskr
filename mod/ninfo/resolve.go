package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/voluminor/ratatoskr/mod/sigils"
	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
)

// // // // // // // // // //

// AskAddr resolves an address string to a public key, then calls Ask.
// Supported formats:
//   - "<hex>.pk.ygg" — hex-encoded public key domain
//   - "[ip6]:port" or "ip6" — yggdrasil IPv6 resolved via peers/sessions
//   - raw 64-char hex string — public key directly
func (obj *Obj) AskAddr(ctx context.Context, addr string, sg ...sigils.Interface) (*AskResultObj, error) {
	key, err := obj.resolveAddr(addr)
	if err != nil {
		return nil, err
	}
	return obj.Ask(ctx, key, sg...)
}

// // // // // // // // // //

func (obj *Obj) resolveAddr(addr string) (ed25519.PublicKey, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, ErrInvalidAddr
	}

	// <hex>.pk.ygg
	if key, err := parsePkYgg(addr); err == nil {
		return key, nil
	}

	// raw hex key (64 chars)
	if key, err := parseHexKey(addr); err == nil {
		return key, nil
	}

	// [ip6]:port or bare ip6
	if key, err := obj.resolveIPv6(addr); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("%w: %q", ErrUnresolvableAddr, addr)
}

// // // // // // // // // //

func parsePkYgg(addr string) (ed25519.PublicKey, error) {
	lower := strings.ToLower(addr)
	if !strings.HasSuffix(lower, ".pk.ygg") {
		return nil, ErrInvalidAddr
	}
	hexPart := addr[:len(addr)-len(".pk.ygg")]
	return parseHexKey(hexPart)
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

func (obj *Obj) resolveIPv6(addr string) (ed25519.PublicKey, error) {
	ip := extractIPv6(addr)
	if ip == nil {
		return nil, ErrInvalidAddr
	}

	// Search peers first, then sessions
	for _, p := range obj.core.GetPeers() {
		if matchYggAddr(p.Key, ip) {
			return p.Key, nil
		}
	}
	for _, s := range obj.core.GetSessions() {
		if matchYggAddr(s.Key, ip) {
			return s.Key, nil
		}
	}

	return nil, ErrUnresolvableAddr
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
