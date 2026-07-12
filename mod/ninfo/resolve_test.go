package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net"
	"testing"
	"time"

	coremod "github.com/voluminor/ratatoskr/mod/core"
	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// // // // // // // // // //
func TestRegex_hexKey(t *testing.T) {
	valid := hex.EncodeToString(make([]byte, 32))
	if !reHexKey.MatchString(valid) {
		t.Fatal("should match 64 hex chars")
	}
	if reHexKey.MatchString("abcd") {
		t.Fatal("should reject short hex")
	}
	if reHexKey.MatchString(valid + "aa") {
		t.Fatal("should reject 66 hex chars")
	}
}

func TestRegex_bracketIPv6(t *testing.T) {
	if !reBracketIPv6.MatchString("[200:abcd::1]:8080") {
		t.Fatal("should match [ip6]:port")
	}
	if reBracketIPv6.MatchString("200:abcd::1") {
		t.Fatal("should not match bare ip6")
	}
	if reBracketIPv6.MatchString("[200:abcd::1]") {
		t.Fatal("should not match [ip6] without port")
	}
}

func TestRegex_bareIPv6(t *testing.T) {
	if !reBareIPv6.MatchString("200:abcd::1") {
		t.Fatal("should match bare ip6")
	}
	if !reBareIPv6.MatchString("::1") {
		t.Fatal("should match ::1")
	}
	if reBareIPv6.MatchString("abcd") {
		t.Fatal("should reject hex without colon")
	}
}

// // // // // // // // // //
// parsePkYggCandidate

func TestParsePkYggCandidate_valid(t *testing.T) {
	key := genKey(t)
	addr := hex.EncodeToString(key) + ".pk.ygg"
	got, matched, err := parsePkYggCandidate(addr)
	if !matched {
		t.Fatal("expected candidate match")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
	}
}

func TestParsePkYggCandidate_uppercaseSuffix(t *testing.T) {
	key := genKey(t)
	addr := hex.EncodeToString(key) + ".PK.YGG"
	got, matched, err := parsePkYggCandidate(addr)
	if !matched {
		t.Fatal("expected candidate match")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
	}
}

func TestParsePkYggCandidate_rejectsSubdomain(t *testing.T) {
	key := genKey(t)
	addr := "subdomain." + hex.EncodeToString(key) + ".pk.ygg"
	if _, matched, err := parsePkYggCandidate(addr); !matched || !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected matched candidate with ErrInvalidKeyLength, got matched=%v err=%v", matched, err)
	}
}

// // // // // // // // // //
// parseHexKey

func TestParseHexKey_valid(t *testing.T) {
	key := genKey(t)
	got, err := parseHexKey(hex.EncodeToString(key))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
	}
}

func TestParseHexKey_wrongLength(t *testing.T) {
	_, err := parseHexKey("abcd")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestParseHexKey_invalidHex(t *testing.T) {
	bad := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	_, err := parseHexKey(bad)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

// // // // // // // // // //
// extractIPv6

func TestExtractIPv6_bracket(t *testing.T) {
	ip := extractIPv6("[200:abcd::1]:8080")
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
	if ip.String() != "200:abcd::1" {
		t.Fatalf("unexpected IP: %s", ip)
	}
}

func TestExtractIPv6_bare(t *testing.T) {
	ip := extractIPv6("200:abcd::1")
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
}

func TestExtractIPv6_invalid(t *testing.T) {
	ip := extractIPv6("not-an-ip")
	if ip != nil {
		t.Fatalf("expected nil for invalid input, got %s", ip)
	}
}

// // // // // // // // // //
// matchYggAddr

func TestMatchYggAddr_match(t *testing.T) {
	key := genKey(t)
	addr := yggaddr.AddrForKey(key)
	if addr == nil {
		t.Fatal("AddrForKey returned nil")
	}
	ip := net.IP(addr[:])
	if !matchYggAddr(key, ip) {
		t.Fatal("expected match")
	}
}

func TestMatchYggAddr_noMatch(t *testing.T) {
	key1 := genKey(t)
	key2 := genKey(t)
	addr := yggaddr.AddrForKey(key2)
	ip := net.IP(addr[:])
	if matchYggAddr(key1, ip) {
		t.Fatal("expected no match for different keys")
	}
}

func TestMatchYggAddr_invalidKey(t *testing.T) {
	if matchYggAddr(nil, net.ParseIP("::1")) {
		t.Fatal("expected false for nil key")
	}
}

// // // // // // // // // //
// resolveIPv6 context handling

func newResolveCore(t *testing.T) *coremod.Obj {
	t.Helper()
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := coremod.New(coremod.ConfigObj{
		Config:          cfg,
		CoreStopTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })
	return node
}

func testYggIPv6(t *testing.T) string {
	t.Helper()
	addr := yggaddr.AddrForKey(genKey(t))
	if addr == nil {
		t.Fatal("AddrForKey returned nil")
	}
	return net.IP(addr[:]).String()
}

func TestAskAddr_nilContextIPv6DoesNotPanic(t *testing.T) {
	coreObj := newResolveCore(t)
	obj := newTestObj()
	obj.source = coreObj
	obj.maxLookupTime = 20 * time.Millisecond
	obj.lookupInterval = time.Millisecond
	//lint:ignore SA1012 this test verifies the documented nil-context contract.
	_, err := obj.AskAddr(nil, testYggIPv6(t)) //nolint:staticcheck // Verifies the documented nil-context contract.
	if !errors.Is(err, ErrUnresolvableAddr) {
		t.Fatalf("expected ErrUnresolvableAddr, got %v", err)
	}
}

func TestAskAddr_canceledIPv6ContextDoesNotTouchCore(t *testing.T) {
	obj := newTestObj()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := obj.AskAddr(ctx, testYggIPv6(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// // // // // // // // // //

func BenchmarkParseHexKey(b *testing.B) {
	h := hex.EncodeToString(make([]byte, 32))
	for b.Loop() {
		if _, err := parseHexKey(h); err != nil {
			b.Fatalf("parseHexKey: %v", err)
		}
	}
}

func BenchmarkMatchYggAddr(b *testing.B) {
	key := make(ed25519.PublicKey, 32)
	addr := yggaddr.AddrForKey(key)
	ip := net.IP(addr[:])
	for b.Loop() {
		matchYggAddr(key, ip)
	}
}
