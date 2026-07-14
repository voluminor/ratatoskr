package common

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseWithDeadlineOrdersFinalAfterDependencies(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var finalCalls atomic.Int64
	done := make(chan error, 1)
	go func() {
		err, timedOut := CloseWithDeadline(time.Second, []NamedCloseObj{{Name: "dependency", Close: func() error {
			close(started)
			<-release
			return errors.New("dependency error")
		}}}, NamedCloseObj{Name: "final", Close: func() error {
			finalCalls.Add(1)
			return nil
		}})
		if timedOut {
			done <- errors.New("unexpected timeout")
			return
		}
		done <- err
	}()
	<-started
	if finalCalls.Load() != 0 {
		t.Fatal("final close started before dependency completed")
	}
	close(release)
	if err := <-done; err == nil || err.Error() != "dependency: dependency error" {
		t.Fatalf("joined error = %v", err)
	}
	if finalCalls.Load() != 1 {
		t.Fatalf("final calls = %d, want 1", finalCalls.Load())
	}
}

func TestCloseWithDeadlineStartsFinalAfterTimeout(t *testing.T) {
	blocked := make(chan struct{})
	finalStarted := make(chan struct{})
	_, timedOut := CloseWithDeadline(10*time.Millisecond, []NamedCloseObj{{Close: func() error {
		<-blocked
		return nil
	}}}, NamedCloseObj{Close: func() error {
		close(finalStarted)
		return nil
	}})
	if !timedOut {
		t.Fatal("expected timeout")
	}
	select {
	case <-finalStarted:
	case <-time.After(time.Second):
		t.Fatal("final close was not started after timeout")
	}
	close(blocked)
}

func TestCloseWithDeadlineDoesNotWaitForBlockedFinal(t *testing.T) {
	blocked := make(chan struct{})
	started := make(chan struct{})
	start := time.Now()
	_, timedOut := CloseWithDeadline(20*time.Millisecond, nil, NamedCloseObj{Close: func() error {
		close(started)
		<-blocked
		return nil
	}})
	if !timedOut {
		t.Fatal("expected timeout while final close is blocked")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("blocked final held root shutdown for %s", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("final close was not started")
	}
	close(blocked)
}
