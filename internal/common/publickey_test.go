package common

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"
)

// // // // // // // // // //

func TestParsePublicKeyDomainStrict(t *testing.T) {
	key := make(ed25519.PublicKey, ed25519.PublicKeySize)
	name := hex.EncodeToString(key) + PublicKeyDomainSuffix
	got, matched, err := ParsePublicKeyDomain(name)
	if err != nil {
		t.Fatalf("ParsePublicKeyDomain: %v", err)
	}
	if !matched {
		t.Fatal("expected public key domain match")
	}
	if !got.Equal(key) {
		t.Fatal("public key mismatch")
	}

	_, matched, err = ParsePublicKeyDomain("subdomain." + name)
	if !matched {
		t.Fatal("subdomain should still be recognized as a public key domain candidate")
	}
	if !errors.Is(err, ErrInvalidPublicKeyLength) {
		t.Fatalf("expected ErrInvalidPublicKeyLength for subdomain, got %v", err)
	}
}

func TestParsePublicKeyDomainNoMatch(t *testing.T) {
	_, matched, err := ParsePublicKeyDomain("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Fatal("non-.pk.ygg name should not match")
	}
}
