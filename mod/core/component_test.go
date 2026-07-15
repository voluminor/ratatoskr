package core

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// // // // // // // // // //

func TestComponentLifecycle(t *testing.T) {
	c := &componentObj[string]{name: "admin"}
	if value, active := c.get(); value != "" || active {
		t.Fatalf("initial state = (%q, %t), want zero and inactive", value, active)
	}
	if err := c.disable(); err != nil {
		t.Fatalf("disable inactive component: %v", err)
	}

	startErr := errors.New("start failed")
	if err := c.enable(func() (string, func() error, error) {
		return "unexpected", nil, startErr
	}); !errors.Is(err, startErr) {
		t.Fatalf("enable error = %v, want %v", err, startErr)
	}
	if value, active := c.get(); value != "" || active {
		t.Fatalf("state after failed enable = (%q, %t), want zero and inactive", value, active)
	}

	stopErr := errors.New("stop failed")
	startCalls := 0
	if err := c.enable(func() (string, func() error, error) {
		startCalls++
		return "first", func() error { return stopErr }, nil
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if value, active := c.get(); value != "first" || !active {
		t.Fatalf("enabled state = (%q, %t), want first and active", value, active)
	}
	if err := c.enable(func() (string, func() error, error) {
		startCalls++
		return "unexpected", func() error { return nil }, nil
	}); !errors.Is(err, ErrAlreadyEnabled) || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("duplicate enable error = %v, want named ErrAlreadyEnabled", err)
	}
	if startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", startCalls)
	}

	if err := c.disable(); !errors.Is(err, stopErr) {
		t.Fatalf("disable error = %v, want %v", err, stopErr)
	}
	if value, active := c.get(); value != "" || active {
		t.Fatalf("state after failed stop = (%q, %t), want zero and inactive", value, active)
	}

	if err := c.enable(func() (string, func() error, error) {
		return "second", func() error { return nil }, nil
	}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if err := c.disable(); err != nil {
		t.Fatalf("final disable: %v", err)
	}
}

func TestComponentConcurrentAccess(t *testing.T) {
	c := &componentObj[string]{name: "component"}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = c.enable(func() (string, func() error, error) {
				return "value", func() error { return nil }, nil
			})
		}()
		go func() {
			defer wg.Done()
			_ = c.disable()
		}()
		go func() {
			defer wg.Done()
			_, _ = c.get()
		}()
	}
	wg.Wait()
	if err := c.disable(); err != nil {
		t.Fatalf("final disable: %v", err)
	}
	if _, active := c.get(); active {
		t.Fatal("component remained active after final disable")
	}
}
