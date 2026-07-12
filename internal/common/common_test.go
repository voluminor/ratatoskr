package common

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"sync/atomic"
	"testing"
	"time"
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

// //

type deadlineRecorderObj struct {
	setDeadline     int
	setReadDeadline int
	lastDeadline    time.Time
}

func (r *deadlineRecorderObj) SetDeadline(t time.Time) error {
	r.setDeadline++
	r.lastDeadline = t
	return nil
}

func (r *deadlineRecorderObj) SetReadDeadline(t time.Time) error {
	r.setReadDeadline++
	r.lastDeadline = t
	return nil
}

func TestRefreshDeadlineArmSkipClear(t *testing.T) {
	rec := &deadlineRecorderObj{}
	var state atomic.Int64
	now := time.Now()

	// First refresh arms the deadline.
	RefreshDeadline(now, time.Minute, &state, rec, false)
	if rec.setDeadline != 1 {
		t.Fatalf("first refresh should arm once, got %d", rec.setDeadline)
	}
	// Immediate second refresh is within the half-budget window: no syscall.
	RefreshDeadline(now, time.Minute, &state, rec, false)
	if rec.setDeadline != 1 {
		t.Fatalf("second refresh should skip, got %d", rec.setDeadline)
	}
	// Disabling the timeout clears the armed deadline exactly once, to zero.
	RefreshDeadline(now, 0, &state, rec, false)
	if rec.setDeadline != 2 || !rec.lastDeadline.IsZero() {
		t.Fatalf("clear should fire once with zero deadline, got %d last=%s", rec.setDeadline, rec.lastDeadline)
	}
	if state.Load() != 0 {
		t.Fatalf("state should reset after clear, got %d", state.Load())
	}
	// A redundant clear touches nothing (no deadline armed).
	RefreshDeadline(now, 0, &state, rec, false)
	if rec.setDeadline != 2 {
		t.Fatalf("redundant clear should not touch the conn, got %d", rec.setDeadline)
	}
}

func TestRefreshDeadlineReadOnlyUsesReadDeadline(t *testing.T) {
	rec := &deadlineRecorderObj{}
	var state atomic.Int64
	RefreshDeadline(time.Now(), time.Minute, &state, rec, true)
	if rec.setReadDeadline != 1 || rec.setDeadline != 0 {
		t.Fatalf("readOnly should use SetReadDeadline only, got read=%d write=%d", rec.setReadDeadline, rec.setDeadline)
	}
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
