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

// Obj — резолвер имён с поддержкой .pk.ygg и DNS через Yggdrasil
type Obj struct {
	resolver *net.Resolver
	hasDNS   bool
}

// New создаёт резолвер.
// dialer используется для DNS-запросов через сеть Yggdrasil.
// nameserver — адрес DNS-сервера; пустая строка = только .pk.ygg и литералы
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

// Resolve разрешает имя в IP-адрес.
// Поддерживает: <pubkey>.pk.ygg, IPv6-литералы, DNS-имена (при наличии nameserver)
func (r *Obj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	// Публичный ключ → IPv6
	if pkName, ok := strings.CutSuffix(name, NameMappingSuffix); ok {
		ip, err := resolvePublicKey(pkName)
		if err != nil {
			return ctx, nil, err
		}
		return ctx, ip, nil
	}

	// IPv6-литерал
	if ip := net.ParseIP(name); ip != nil {
		return ctx, ip, nil
	}

	// DNS — только если настроен nameserver
	if !r.hasDNS {
		return ctx, nil, fmt.Errorf("cannot resolve %q: no nameserver configured", name)
	}
	addrs, err := r.resolver.LookupIP(ctx, "ip6", name)
	if err != nil {
		return ctx, nil, fmt.Errorf("lookup %q: %w", name, err)
	}
	if len(addrs) == 0 {
		return ctx, nil, fmt.Errorf("no addresses for %q", name)
	}
	return ctx, addrs[0], nil
}

// //

func resolvePublicKey(name string) (net.IP, error) {
	// subdomain.<pubkey> → берём только последний сегмент
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	b, err := hex.DecodeString(name)
	if err != nil {
		return nil, fmt.Errorf("hex.DecodeString: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(b))
	}
	var pk [ed25519.PublicKeySize]byte
	copy(pk[:], b)
	return net.IP(address.AddrForKey(pk[:])[:]), nil
}
