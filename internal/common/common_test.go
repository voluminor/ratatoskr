package common

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"sync"
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
	var gate DeadlineGateObj
	now := time.Now()

	// First refresh arms the deadline.
	RefreshDeadline(now, time.Minute, &gate, rec, false)
	if rec.setDeadline != 1 {
		t.Fatalf("first refresh should arm once, got %d", rec.setDeadline)
	}
	// Immediate second refresh is within the half-budget window: no syscall.
	RefreshDeadline(now, time.Minute, &gate, rec, false)
	if rec.setDeadline != 1 {
		t.Fatalf("second refresh should skip, got %d", rec.setDeadline)
	}
	// Disabling the timeout clears the armed deadline exactly once, to zero.
	RefreshDeadline(now, 0, &gate, rec, false)
	if rec.setDeadline != 2 || !rec.lastDeadline.IsZero() {
		t.Fatalf("clear should fire once with zero deadline, got %d last=%s", rec.setDeadline, rec.lastDeadline)
	}
	if gate.state.Load() != 0 {
		t.Fatalf("state should reset after clear, got %d", gate.state.Load())
	}
	// A redundant clear touches nothing (no deadline armed).
	RefreshDeadline(now, 0, &gate, rec, false)
	if rec.setDeadline != 2 {
		t.Fatalf("redundant clear should not touch the conn, got %d", rec.setDeadline)
	}
}

func TestRefreshDeadlineReadOnlyUsesReadDeadline(t *testing.T) {
	rec := &deadlineRecorderObj{}
	var gate DeadlineGateObj
	RefreshDeadline(time.Now(), time.Minute, &gate, rec, true)
	if rec.setReadDeadline != 1 || rec.setDeadline != 0 {
		t.Fatalf("readOnly should use SetReadDeadline only, got read=%d write=%d", rec.setReadDeadline, rec.setDeadline)
	}
}

func TestRefreshDeadlineDisabledFastPathDoesNotTakeSideEffectLock(t *testing.T) {
	recorder := &deadlineRecorderObj{}
	var gate DeadlineGateObj
	gate.mu.Lock()
	done := make(chan struct{})
	go func() {
		RefreshDeadline(time.Now(), -1, &gate, recorder, false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		gate.mu.Unlock()
		t.Fatal("disabled fast path waited for the side-effect lock")
	}
	gate.mu.Unlock()
}

type orderedDeadlineRecorderObj struct {
	mu           sync.Mutex
	firstEntered chan struct{}
	releaseFirst chan struct{}
	calls        []time.Time
}

func (r *orderedDeadlineRecorderObj) SetDeadline(deadline time.Time) error {
	r.mu.Lock()
	first := len(r.calls) == 0
	r.calls = append(r.calls, deadline)
	r.mu.Unlock()
	if first {
		close(r.firstEntered)
		<-r.releaseFirst
	}
	return nil
}

func (*orderedDeadlineRecorderObj) SetReadDeadline(time.Time) error { return nil }

func TestRefreshDeadlineSerializesSideEffects(t *testing.T) {
	rec := &orderedDeadlineRecorderObj{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	var gate DeadlineGateObj
	base := time.Now()
	done := make(chan struct{}, 2)
	go func() {
		RefreshDeadline(base, time.Minute, &gate, rec, false)
		done <- struct{}{}
	}()
	<-rec.firstEntered
	go func() {
		RefreshDeadline(base.Add(time.Minute), time.Minute, &gate, rec, false)
		done <- struct{}{}
	}()

	// The second side effect must not overtake the blocked first call.
	time.Sleep(10 * time.Millisecond)
	rec.mu.Lock()
	gotBeforeRelease := len(rec.calls)
	rec.mu.Unlock()
	if gotBeforeRelease != 1 {
		t.Fatalf("SetDeadline calls before release = %d, want 1", gotBeforeRelease)
	}
	close(rec.releaseFirst)
	<-done
	<-done

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 2 || !rec.calls[1].After(rec.calls[0]) {
		t.Fatalf("deadlines were not applied in increasing order: %v", rec.calls)
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

func TestCloneNodeInfoDeepCopiesTypedContainers(t *testing.T) {
	src := map[string]any{
		"groups": map[string][]string{"a": {"one", "two"}},
		"nested": []any{map[string]any{"value": "before"}},
		"nil":    nil,
	}
	clone, err := CloneNodeInfo(src)
	if err != nil {
		t.Fatalf("CloneNodeInfo: %v", err)
	}
	src["groups"].(map[string][]string)["a"][0] = "changed"
	src["nested"].([]any)[0].(map[string]any)["value"] = "changed"
	if got := clone["groups"].(map[string][]string)["a"][0]; got != "one" {
		t.Fatalf("typed nested slice leaked: %q", got)
	}
	if got := clone["nested"].([]any)[0].(map[string]any)["value"]; got != "before" {
		t.Fatalf("nested map leaked: %q", got)
	}
	if clone["nil"] != nil {
		t.Fatal("nil value changed")
	}
}

func TestCloneNodeInfoRejectsCycles(t *testing.T) {
	mapCycle := make(map[string]any)
	mapCycle["self"] = mapCycle
	sliceCycle := make([]any, 1)
	sliceCycle[0] = sliceCycle
	for name, src := range map[string]map[string]any{
		"map":   mapCycle,
		"slice": {"self": sliceCycle},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CloneNodeInfo(src); !errors.Is(err, ErrNodeInfoCycle) {
				t.Fatalf("CloneNodeInfo error = %v, want ErrNodeInfoCycle", err)
			}
		})
	}
}

func TestCloneNodeInfoRejectsExcessiveDepth(t *testing.T) {
	src := map[string]any{}
	cursor := src
	for i := 0; i <= maxNodeInfoDepth; i++ {
		next := map[string]any{}
		cursor["next"] = next
		cursor = next
	}
	if _, err := CloneNodeInfo(src); !errors.Is(err, ErrNodeInfoTooDeep) {
		t.Fatalf("CloneNodeInfo error = %v, want ErrNodeInfoTooDeep", err)
	}
}

func TestCloneNodeInfoAllowsSharedAcyclicValues(t *testing.T) {
	shared := map[string]any{"value": "before"}
	clone, err := CloneNodeInfo(map[string]any{"a": shared, "b": shared})
	if err != nil {
		t.Fatalf("CloneNodeInfo: %v", err)
	}
	clone["a"].(map[string]any)["value"] = "changed"
	if got := clone["b"].(map[string]any)["value"]; got != "before" {
		t.Fatalf("shared DAG branches alias after clone: %q", got)
	}
}
