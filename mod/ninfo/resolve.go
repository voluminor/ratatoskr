package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/voluminor/ratatoskr/mod/sigils"
	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
)

// // // // // // // // // //

var (
	// 64 hex chars + ".pk.ygg"
	rePkYgg = regexp.MustCompile(`(?i)^[0-9a-f]{64}\.pk\.ygg$`)
	// 64 hex chars
	reHexKey = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	// [ip6]:port
	reBracketIPv6 = regexp.MustCompile(`^\[([0-9a-fA-F:]+)\]:\d{1,5}$`)
	// bare ipv6
	reBareIPv6 = regexp.MustCompile(`^[0-9a-fA-F:]+$`)
)

// // // // // // // // // //

// AskAddr resolves an address string to a public key, then calls Ask.
// Supported formats:
//   - "<hex>.pk.ygg" — hex-encoded public key domain
//   - "[ip6]:port" or "ip6" — yggdrasil IPv6 resolved via peers/sessions
//   - raw 64-char hex string — public key directly
func (obj *Obj) AskAddr(ctx context.Context, addr string, sg ...sigils.Interface) (*AskResultObj, error) {
	key, err := obj.resolveAddr(ctx, addr)
	if err != nil {
		return nil, err
	}
	return obj.Ask(ctx, key, sg...)
}

// // // // // // // // // //

func (obj *Obj) resolveAddr(ctx context.Context, addr string) (ed25519.PublicKey, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, ErrInvalidAddr
	}

	switch {
	case rePkYgg.MatchString(addr):
		return parsePkYgg(addr)
	case reHexKey.MatchString(addr):
		return parseHexKey(addr)
	case reBracketIPv6.MatchString(addr), reBareIPv6.MatchString(addr):
		return obj.resolveIPv6(ctx, addr)
	}

	return nil, fmt.Errorf("%w: %q", ErrInvalidAddr, addr)
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

// LookupInterval controls how often resolveIPv6 polls for a resolved key.
var LookupInterval = 100 * time.Millisecond

// //

func (obj *Obj) resolveIPv6(ctx context.Context, addr string) (ed25519.PublicKey, error) {
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
	obj.core.PacketConn.PacketConn.SendLookup(partial)

	ticker := time.NewTicker(LookupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ErrUnresolvableAddr
		case <-ticker.C:
			if key := obj.findKeyByIP(ip); key != nil {
				return key, nil
			}
		}
	}
}

// //

func (obj *Obj) findKeyByIP(ip net.IP) ed25519.PublicKey {
	for _, p := range obj.core.GetPeers() {
		if matchYggAddr(p.Key, ip) {
			return p.Key
		}
	}
	for _, s := range obj.core.GetSessions() {
		if matchYggAddr(s.Key, ip) {
			return s.Key
		}
	}
	for _, p := range obj.core.GetPaths() {
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
