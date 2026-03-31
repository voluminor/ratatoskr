package probe

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

// // // // // // // // // //

func TestValidateKey_valid(t *testing.T) {
	if err := validateKey(genKey(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKey_short(t *testing.T) {
	err := validateKey(ed25519.PublicKey(make([]byte, 16)))
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected ErrInvalidKeyLength, got: %v", err)
	}
}

func TestValidateKey_nil(t *testing.T) {
	err := validateKey(nil)
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected ErrInvalidKeyLength, got: %v", err)
	}
}
