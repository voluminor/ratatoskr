package core

import (
	"errors"
	"sync"
	"testing"
)

// // // // // // // // // //

func TestComponent_enable(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	called := false
	err := c.enable(func() (string, func() error, error) {
		called = true
		return "value", func() error { return nil }, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("start function not called")
	}
	if got, active := c.get(); got != "value" || !active {
		t.Errorf("unexpected state: value=%v active=%v", got, active)
	}
}

func TestComponent_enableTwice(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	if err := c.enable(func() (string, func() error, error) {
		return "v1", func() error { return nil }, nil
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	err := c.enable(func() (string, func() error, error) {
		return "v2", func() error { return nil }, nil
	})
	if err == nil {
		t.Fatal("expected error on double enable")
	}
	if got, active := c.get(); got != "v1" || !active {
		t.Errorf("value should not change on failed enable: value=%v active=%v", got, active)
	}
}

func TestComponent_enableError(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	want := errors.New("start failed")
	err := c.enable(func() (string, func() error, error) {
		return "", nil, want
	})
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
	if _, active := c.get(); active {
		t.Error("component should be inactive after failed enable")
	}
}

func TestComponent_disable(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	stopped := false
	if err := c.enable(func() (string, func() error, error) {
		return "value", func() error {
			stopped = true
			return nil
		}, nil
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := c.disable(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopped {
		t.Error("stop function not called")
	}
	if _, active := c.get(); active {
		t.Error("component should be inactive after disable")
	}
}

func TestComponent_disableWhenInactive(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	if err := c.disable(); err != nil {
		t.Fatalf("unexpected error on inactive disable: %v", err)
	}
}

func TestComponent_disableError(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	want := errors.New("stop failed")
	if err := c.enable(func() (string, func() error, error) {
		return "value", func() error { return want }, nil
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	err := c.disable()
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
	// Even on stop error, value is cleared
	if _, active := c.get(); active {
		t.Error("component should be inactive even after stop error")
	}
}

func TestComponent_getInactive(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	if v, active := c.get(); active {
		t.Errorf("expected inactive, got value=%v", v)
	}
}

func TestComponent_enableDisableEnable(t *testing.T) {
	c := &componentObj[string]{name: "test"}
	if err := c.enable(func() (string, func() error, error) {
		return "v1", func() error { return nil }, nil
	}); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	if err := c.disable(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	err := c.enable(func() (string, func() error, error) {
		return "v2", func() error { return nil }, nil
	})
	if err != nil {
		t.Fatalf("unexpected error after re-enable: %v", err)
	}
	if got, active := c.get(); got != "v2" || !active {
		t.Errorf("expected v2 active, got value=%v active=%v", got, active)
	}
}

func TestComponent_nameInError(t *testing.T) {
	c := &componentObj[string]{name: "mycomp"}
	if err := c.enable(func() (string, func() error, error) {
		return "v", func() error { return nil }, nil
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	err := c.enable(func() (string, func() error, error) {
		return "v2", func() error { return nil }, nil
	})
	if err == nil || !errors.Is(err, err) {
		t.Fatal("expected error")
	}
	if err.Error() == "" {
		t.Error("error should contain name")
	}
}

// //

func TestComponent_concurrentEnableDisable(t *testing.T) {
	c := &componentObj[string]{name: "concurrent"}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.enable(func() (string, func() error, error) {
				return "v", func() error { return nil }, nil
			})
		}()
		go func() {
			defer wg.Done()
			_ = c.disable()
		}()
	}
	wg.Wait()
}

func TestComponent_concurrentGet(t *testing.T) {
	c := &componentObj[string]{name: "concurrent"}
	if err := c.enable(func() (string, func() error, error) {
		return "value", func() error { return nil }, nil
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.get()
		}()
	}
	wg.Wait()
}

// //

func BenchmarkComponent_enableDisable(b *testing.B) {
	c := &componentObj[string]{name: "bench"}
	for b.Loop() {
		if err := c.enable(func() (string, func() error, error) {
			return "v", func() error { return nil }, nil
		}); err != nil {
			b.Fatalf("enable: %v", err)
		}
		if err := c.disable(); err != nil {
			b.Fatalf("disable: %v", err)
		}
	}
}

func BenchmarkComponent_get(b *testing.B) {
	c := &componentObj[string]{name: "bench"}
	if err := c.enable(func() (string, func() error, error) {
		return "v", func() error { return nil }, nil
	}); err != nil {
		b.Fatalf("enable: %v", err)
	}
	for b.Loop() {
		c.get()
	}
}

func BenchmarkComponent_concurrentGet(b *testing.B) {
	c := &componentObj[string]{name: "bench"}
	if err := c.enable(func() (string, func() error, error) {
		return "v", func() error { return nil }, nil
	}); err != nil {
		b.Fatalf("enable: %v", err)
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.get()
		}
	})
}
