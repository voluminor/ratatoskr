package common

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// // // // // // // // // //

type deadlineRecorderObj struct {
	setDeadline     int
	setReadDeadline int
	lastDeadline    time.Time
	err             error
}

func (r *deadlineRecorderObj) SetDeadline(t time.Time) error {
	r.setDeadline++
	r.lastDeadline = t
	return r.err
}

func (r *deadlineRecorderObj) SetReadDeadline(t time.Time) error {
	r.setReadDeadline++
	r.lastDeadline = t
	return r.err
}

func TestRefreshDeadlineArmSkipClear(t *testing.T) {
	recorder := &deadlineRecorderObj{}
	var gate DeadlineGateObj
	now := time.Now()

	RefreshDeadline(now, time.Minute, &gate, recorder, false)
	if recorder.setDeadline != 1 {
		t.Fatalf("first refresh should arm once, got %d", recorder.setDeadline)
	}
	RefreshDeadline(now, time.Minute, &gate, recorder, false)
	if recorder.setDeadline != 1 {
		t.Fatalf("second refresh should skip, got %d", recorder.setDeadline)
	}
	RefreshDeadline(now, 0, &gate, recorder, false)
	if recorder.setDeadline != 2 || !recorder.lastDeadline.IsZero() {
		t.Fatalf("clear should fire once with zero deadline, got %d last=%s", recorder.setDeadline, recorder.lastDeadline)
	}
	if gate.state.Load() != 0 {
		t.Fatalf("state should reset after clear, got %d", gate.state.Load())
	}
	RefreshDeadline(now, 0, &gate, recorder, false)
	if recorder.setDeadline != 2 {
		t.Fatalf("redundant clear should not touch the conn, got %d", recorder.setDeadline)
	}
}

func TestRefreshDeadlineReadOnlyUsesReadDeadline(t *testing.T) {
	recorder := &deadlineRecorderObj{}
	var gate DeadlineGateObj
	RefreshDeadline(time.Now(), time.Minute, &gate, recorder, true)
	if recorder.setReadDeadline != 1 || recorder.setDeadline != 0 {
		t.Fatalf("readOnly should use SetReadDeadline only, got read=%d write=%d", recorder.setReadDeadline, recorder.setDeadline)
	}
}

func TestRefreshDeadlineRetriesAfterSetError(t *testing.T) {
	recorder := &deadlineRecorderObj{err: errors.New("set failed")}
	var gate DeadlineGateObj
	now := time.Now()
	RefreshDeadline(now, time.Minute, &gate, recorder, false)
	recorder.err = nil
	RefreshDeadline(now, time.Minute, &gate, recorder, false)
	if recorder.setDeadline != 2 {
		t.Fatalf("SetDeadline calls = %d, want retry after failure", recorder.setDeadline)
	}
	if gate.state.Load() == 0 {
		t.Fatal("successful retry did not store the armed deadline")
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
	recorder := &orderedDeadlineRecorderObj{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	var gate DeadlineGateObj
	base := time.Now()
	done := make(chan struct{}, 2)
	go func() {
		RefreshDeadline(base, time.Minute, &gate, recorder, false)
		done <- struct{}{}
	}()
	<-recorder.firstEntered
	if gate.mu.TryLock() {
		gate.mu.Unlock()
		t.Fatal("side-effect lock was not held during SetDeadline")
	}
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		RefreshDeadline(base.Add(time.Minute), time.Minute, &gate, recorder, false)
		done <- struct{}{}
	}()
	<-secondStarted
	close(recorder.releaseFirst)
	<-done
	<-done

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.calls) != 2 || !recorder.calls[1].After(recorder.calls[0]) {
		t.Fatalf("deadlines were not applied in increasing order: %v", recorder.calls)
	}
}
