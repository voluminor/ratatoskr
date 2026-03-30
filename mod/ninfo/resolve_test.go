package ninfo

import (
	"crypto/ed25519"
	"encoding/hex"
	"net"
	"testing"

	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
)

// // // // // // // // // //
// regex validation

func TestRegex_pkYgg(t *testing.T) {
	valid := hex.EncodeToString(make([]byte, 32)) + ".pk.ygg"
	if !rePkYgg.MatchString(valid) {
		t.Fatalf("should match: %s", valid)
	}
	if !rePkYgg.MatchString(hex.EncodeToString(make([]byte, 32)) + ".PK.YGG") {
		t.Fatal("should be case-insensitive")
	}
	if rePkYgg.MatchString("short.pk.ygg") {
		t.Fatal("should reject short hex")
	}
	if rePkYgg.MatchString(hex.EncodeToString(make([]byte, 32))) {
		t.Fatal("should reject bare hex without .pk.ygg")
	}
}

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
// parsePkYgg

func TestParsePkYgg_valid(t *testing.T) {
	key := genKey(t)
	addr := hex.EncodeToString(key) + ".pk.ygg"
	got, err := parsePkYgg(addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
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
	// 64 chars but not valid hex
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

func BenchmarkParseHexKey(b *testing.B) {
	h := hex.EncodeToString(make([]byte, 32))
	for b.Loop() {
		parseHexKey(h)
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
