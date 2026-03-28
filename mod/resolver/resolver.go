package resolver

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

const NameMappingSuffix = ".pk.ygg"

// //

// Obj — name resolver supporting .pk.ygg and DNS over Yggdrasil
type Obj struct {
	resolver *net.Resolver
	hasDNS   bool
}

// New creates a resolver; empty nameserver = only .pk.ygg and literals
func New(dialer proxy.ContextDialer, nameserver string) *Obj {
	r := &Obj{
		resolver: &net.Resolver{PreferGo: true},
	}
	if nameserver != "" {
		r.hasDNS = true
		ns := nameserver
		r.resolver.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(ns)
			if err != nil {
				host = ns
				port = "53"
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		}
	}
	return r
}

// //

// Resolve — <pubkey>.pk.ygg, IPv6 literals, DNS (when nameserver is configured)
func (r *Obj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	// Public key → IPv6
	if pkName, ok := strings.CutSuffix(name, NameMappingSuffix); ok {
		ip, err := resolvePublicKey(pkName)
		if err != nil {
			return ctx, nil, err
		}
		return ctx, ip, nil
	}

	// IPv6 literal
	if ip := net.ParseIP(name); ip != nil {
		return ctx, ip, nil
	}

	// DNS — only if nameserver is configured
	if !r.hasDNS {
		return ctx, nil, fmt.Errorf("%w: cannot resolve %q", ErrNoNameserver, name)
	}
	addrs, err := r.resolver.LookupIP(ctx, "ip6", name)
	if err != nil {
		return ctx, nil, fmt.Errorf("lookup %q: %w", name, err)
	}
	if len(addrs) == 0 {
		return ctx, nil, fmt.Errorf("%w for %q", ErrNoAddresses, name)
	}
	return ctx, addrs[0], nil
}

// //

func resolvePublicKey(name string) (net.IP, error) {
	// subdomain.<pubkey> → take only the last segment
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	b, err := hex.DecodeString(name)
	if err != nil {
		return nil, fmt.Errorf("hex.DecodeString: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidKeyLength, ed25519.PublicKeySize, len(b))
	}
	var pk [ed25519.PublicKeySize]byte
	copy(pk[:], b)
	return net.IP(address.AddrForKey(pk[:])[:]), nil
}
