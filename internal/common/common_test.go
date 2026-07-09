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

// //

func TestDynamicLimitAcquireOrReadyWakesOnRelease(t *testing.T) {
	limit := NewDynamicLimit(1)
	if !limit.Acquire() {
		t.Fatal("failed to acquire initial slot")
	}

	acquired, ready := limit.AcquireOrReady()
	if acquired {
		t.Fatal("second acquire should wait while the slot is occupied")
	}
	select {
	case <-ready:
		t.Fatal("ready channel closed before a slot was released")
	default:
	}

	limit.Release()
	select {
	case <-ready:
	default:
		t.Fatal("ready channel did not close after release")
	}

	acquired, _ = limit.AcquireOrReady()
	if !acquired {
		t.Fatal("slot should be acquired after release")
	}
	limit.Release()
}

func TestDynamicLimitAcquireOrReadyWakesOnLimitIncrease(t *testing.T) {
	limit := NewDynamicLimit(1)
	if !limit.Acquire() {
		t.Fatal("failed to acquire initial slot")
	}

	acquired, ready := limit.AcquireOrReady()
	if acquired {
		t.Fatal("second acquire should wait while the limit is one")
	}

	limit.Set(2)
	select {
	case <-ready:
	default:
		t.Fatal("ready channel did not close after limit increase")
	}

	acquired, _ = limit.AcquireOrReady()
	if !acquired {
		t.Fatal("slot should be acquired after limit increase")
	}
	limit.Release()
	limit.Release()
}

func TestDynamicLimitUnlimitedAndLazyReady(t *testing.T) {
	limit := NewDynamicLimit(0)
	if limit.ready != nil {
		t.Fatal("ready channel should be allocated only for waiters")
	}
	for range 4 {
		if !limit.Acquire() {
			t.Fatal("zero limit should mean unlimited")
		}
	}
	limit.Set(-1)
	if limit.ready != nil {
		t.Fatal("Set without waiters should not allocate a ready channel")
	}
	for range 4 {
		if !limit.Acquire() {
			t.Fatal("negative limit should mean unlimited")
		}
	}
	for range 8 {
		limit.Release()
	}
	if limit.Active() != 0 {
		t.Fatalf("active slots = %d, want 0", limit.Active())
	}
	if limit.ready != nil {
		t.Fatal("Release without waiters should not allocate a ready channel")
	}
}
