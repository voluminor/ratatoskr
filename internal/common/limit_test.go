package common

import "testing"

// // // // // // // // // //

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

func TestDynamicLimitUnlimited(t *testing.T) {
	limit := NewDynamicLimit(0)
	for range 4 {
		if !limit.Acquire() {
			t.Fatal("zero limit should allow every acquisition")
		}
	}
	limit.Set(-1)
	for range 4 {
		if !limit.Acquire() {
			t.Fatal("negative limit should allow every acquisition")
		}
	}
	for range 9 {
		limit.Release()
	}
	if limit.Active() != 0 {
		t.Fatalf("active slots = %d, want 0", limit.Active())
	}
}

func TestDynamicLimitUnlimitedSteadyStateDoesNotAllocate(t *testing.T) {
	limit := NewDynamicLimit(0)
	allocations := testing.AllocsPerRun(1000, func() {
		if !limit.Acquire() {
			t.Fatal("unlimited acquisition failed")
		}
		limit.Release()
	})
	if allocations != 0 {
		t.Fatalf("steady-state allocations = %f, want 0", allocations)
	}
}
