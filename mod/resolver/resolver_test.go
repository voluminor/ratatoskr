package resolver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"testing"

	"golang.org/x/net/proxy"
)

// // // // // // // // // //

// failDialerObj — proxy.ContextDialer that always returns an error
type failDialerObj struct{}

func (failDialerObj) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	return nil, net.ErrClosed
}

var _ proxy.ContextDialer = failDialerObj{}

// //

func generatePKHex(t *testing.T) string {
	t.Helper()
	pk, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return hex.EncodeToString(pk)
}

// //

func TestResolve_pkYgg(t *testing.T) {
	r := New(failDialerObj{}, "")
	name := generatePKHex(t) + NameMappingSuffix
	_, ip, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ip) < 16 {
		t.Errorf("expected IPv6 address, got %v", ip)
	}
}

func TestResolve_pkYgg_subdomain(t *testing.T) {
	r := New(failDialerObj{}, "")
	pkHex := generatePKHex(t)
	// subdomain.pubkey.pk.ygg — subdomain prefix should be stripped
	name := "subdomain." + pkHex + NameMappingSuffix
	_, ip, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
}

func TestResolve_pkYgg_invalidHex(t *testing.T) {
	r := New(failDialerObj{}, "")
	name := "zzznotvalidhex" + NameMappingSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestResolve_pkYgg_shortKey(t *testing.T) {
	r := New(failDialerObj{}, "")
	name := hex.EncodeToString(make([]byte, 16)) + NameMappingSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected error for too-short key")
	}
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Errorf("expected ErrInvalidKeyLength, got: %v", err)
	}
}

func TestResolve_pkYgg_longKey(t *testing.T) {
	r := New(failDialerObj{}, "")
	name := hex.EncodeToString(make([]byte, 64)) + NameMappingSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected error for too-long key")
	}
}

func TestResolve_ipv6Literal(t *testing.T) {
	r := New(failDialerObj{}, "")
	_, ip, err := r.Resolve(context.Background(), "200::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip.String() != "200::1" {
		t.Errorf("expected 200::1, got %s", ip)
	}
}

func TestResolve_ipv4Literal(t *testing.T) {
	r := New(failDialerObj{}, "")
	_, ip, err := r.Resolve(context.Background(), "192.168.1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
}

func TestResolve_noDNS_hostname(t *testing.T) {
	r := New(failDialerObj{}, "")
	_, _, err := r.Resolve(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error: no nameserver configured")
	}
	if !errors.Is(err, ErrNoNameserver) {
		t.Errorf("expected ErrNoNameserver, got: %v", err)
	}
}

func TestNew_hasDNS_withNameserver(t *testing.T) {
	r := New(failDialerObj{}, "[200::1]:53")
	if !r.hasDNS {
		t.Error("expected hasDNS=true")
	}
}

func TestNew_hasDNS_noNameserver(t *testing.T) {
	r := New(failDialerObj{}, "")
	if r.hasDNS {
		t.Error("expected hasDNS=false")
	}
}

func TestResolve_pkYgg_sameKeyDeterministic(t *testing.T) {
	r := New(failDialerObj{}, "")
	pkHex := generatePKHex(t)
	name := pkHex + NameMappingSuffix
	_, ip1, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	_, ip2, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if ip1.String() != ip2.String() {
		t.Errorf("same key produced different IPs: %s vs %s", ip1, ip2)
	}
}

// //

func TestResolvePublicKey_exactLength(t *testing.T) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	ip, err := resolvePublicKey(hex.EncodeToString(pk))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ip) < 16 {
		t.Error("expected IPv6 address")
	}
}

func TestResolvePublicKey_invalidHex(t *testing.T) {
	_, err := resolvePublicKey("!!not-hex!!")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolvePublicKey_tooShort(t *testing.T) {
	_, err := resolvePublicKey(hex.EncodeToString(make([]byte, 10)))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestResolvePublicKey_tooLong(t *testing.T) {
	_, err := resolvePublicKey(hex.EncodeToString(make([]byte, 64)))
	if err == nil {
		t.Fatal("expected error for long key")
	}
}

func TestResolvePublicKey_subdomainStripped(t *testing.T) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	pkHex := hex.EncodeToString(pk)
	// "subdomain.pubkey" → should use the last segment (pubkey)
	ip, err := resolvePublicKey("some.subdomain." + pkHex)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Compare to resolving the bare key
	ipDirect, _ := resolvePublicKey(pkHex)
	if ip.String() != ipDirect.String() {
		t.Errorf("subdomain stripping changed result: %s vs %s", ip, ipDirect)
	}
}

// //

func BenchmarkResolve_pkYgg(b *testing.B) {
	r := New(failDialerObj{}, "")
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	name := hex.EncodeToString(pk) + NameMappingSuffix
	ctx := context.Background()
	for b.Loop() {
		r.Resolve(ctx, name)
	}
}

func BenchmarkResolve_ipLiteral(b *testing.B) {
	r := New(failDialerObj{}, "")
	ctx := context.Background()
	for b.Loop() {
		r.Resolve(ctx, "200::1")
	}
}

func BenchmarkResolvePublicKey(b *testing.B) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	pkHex := hex.EncodeToString(pk)
	for b.Loop() {
		resolvePublicKey(pkHex)
	}
}
