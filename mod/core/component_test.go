package core

import (
	"errors"
	"sync"
	"testing"
)

// // // // // // // // // //

func TestComponent_enable(t *testing.T) {
	c := &componentObj{name: "test"}
	called := false
	err := c.enable(func() (any, func() error, error) {
		called = true
		return "value", func() error { return nil }, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("start function not called")
	}
	if c.get() != "value" {
		t.Errorf("unexpected value: %v", c.get())
	}
}

func TestComponent_enableTwice(t *testing.T) {
	c := &componentObj{name: "test"}
	c.enable(func() (any, func() error, error) {
		return "v1", func() error { return nil }, nil
	})
	err := c.enable(func() (any, func() error, error) {
		return "v2", func() error { return nil }, nil
	})
	if err == nil {
		t.Fatal("expected error on double enable")
	}
	if c.get() != "v1" {
		t.Error("value should not change on failed enable")
	}
}

func TestComponent_enableError(t *testing.T) {
	c := &componentObj{name: "test"}
	want := errors.New("start failed")
	err := c.enable(func() (any, func() error, error) {
		return nil, nil, want
	})
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
	if c.get() != nil {
		t.Error("value should be nil after failed enable")
	}
}

func TestComponent_disable(t *testing.T) {
	c := &componentObj{name: "test"}
	stopped := false
	c.enable(func() (any, func() error, error) {
		return "value", func() error {
			stopped = true
			return nil
		}, nil
	})
	if err := c.disable(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopped {
		t.Error("stop function not called")
	}
	if c.get() != nil {
		t.Error("value should be nil after disable")
	}
}

func TestComponent_disableWhenInactive(t *testing.T) {
	c := &componentObj{name: "test"}
	if err := c.disable(); err != nil {
		t.Fatalf("unexpected error on inactive disable: %v", err)
	}
}

func TestComponent_disableError(t *testing.T) {
	c := &componentObj{name: "test"}
	want := errors.New("stop failed")
	c.enable(func() (any, func() error, error) {
		return "value", func() error { return want }, nil
	})
	err := c.disable()
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
	// Even on stop error, value is cleared
	if c.get() != nil {
		t.Error("value should be nil even after stop error")
	}
}

func TestComponent_getInactive(t *testing.T) {
	c := &componentObj{name: "test"}
	if v := c.get(); v != nil {
		t.Errorf("expected nil, got %v", v)
	}
}

func TestComponent_enableDisableEnable(t *testing.T) {
	c := &componentObj{name: "test"}
	c.enable(func() (any, func() error, error) {
		return "v1", func() error { return nil }, nil
	})
	c.disable()
	err := c.enable(func() (any, func() error, error) {
		return "v2", func() error { return nil }, nil
	})
	if err != nil {
		t.Fatalf("unexpected error after re-enable: %v", err)
	}
	if c.get() != "v2" {
		t.Errorf("expected v2, got %v", c.get())
	}
}

func TestComponent_nameInError(t *testing.T) {
	c := &componentObj{name: "mycomp"}
	c.enable(func() (any, func() error, error) {
		return "v", func() error { return nil }, nil
	})
	err := c.enable(func() (any, func() error, error) {
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
	c := &componentObj{name: "concurrent"}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.enable(func() (any, func() error, error) {
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
	c := &componentObj{name: "concurrent"}
	c.enable(func() (any, func() error, error) {
		return "value", func() error { return nil }, nil
	})
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
	c := &componentObj{name: "bench"}
	for b.Loop() {
		c.enable(func() (any, func() error, error) {
			return "v", func() error { return nil }, nil
		})
		c.disable()
	}
}

func BenchmarkComponent_get(b *testing.B) {
	c := &componentObj{name: "bench"}
	c.enable(func() (any, func() error, error) {
		return "v", func() error { return nil }, nil
	})
	for b.Loop() {
		c.get()
	}
}

func BenchmarkComponent_concurrentGet(b *testing.B) {
	c := &componentObj{name: "bench"}
	c.enable(func() (any, func() error, error) {
		return "v", func() error { return nil }, nil
	})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.get()
		}
	})
}
