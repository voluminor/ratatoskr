package probe

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

// // // // // // // // // //

func TestPeerCache_getSet(t *testing.T) {
	c := newPeerCache()
	defer c.close()

	k := toKeyArray(genKey(t))
	peers := genKeyN(t, 3)

	_, _, ok := c.get(k)
	if ok {
		t.Fatal("expected miss on empty cache")
	}

	c.set(k, peers, 50*time.Millisecond)
	got, rtt, ok := c.get(k)
	if !ok {
		t.Fatal("expected hit")
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(got))
	}
	if rtt != 50*time.Millisecond {
		t.Fatalf("expected rtt=50ms, got %v", rtt)
	}
}

func TestPeerCache_unreachable(t *testing.T) {
	c := newPeerCache()
	defer c.close()

	k := toKeyArray(genKey(t))
	c.set(k, nil, 100*time.Millisecond)
	peers, rtt, ok := c.get(k)
	if !ok {
		t.Fatal("expected hit for unreachable")
	}
	if peers != nil {
		t.Fatal("expected nil peers for unreachable")
	}
	if rtt != 100*time.Millisecond {
		t.Fatalf("expected rtt=100ms, got %v", rtt)
	}
}

func TestPeerCache_expiry(t *testing.T) {
	origTTL := CacheTTL
	CacheTTL = 50 * time.Millisecond
	t.Cleanup(func() { CacheTTL = origTTL })

	c := newPeerCache()
	defer c.close()

	k := toKeyArray(genKey(t))
	c.set(k, genKeyN(t, 1), 0)

	time.Sleep(60 * time.Millisecond)
	_, _, ok := c.get(k)
	if ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestPeerCache_flush(t *testing.T) {
	origTTL := CacheTTL
	CacheTTL = 50 * time.Millisecond
	t.Cleanup(func() { CacheTTL = origTTL })

	c := newPeerCache()
	defer c.close()

	for range 10 {
		c.set(toKeyArray(genKey(t)), genKeyN(t, 1), 0)
	}
	c.flush()

	// All entries should be gone; use a fresh key to verify map is empty
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	if n != 0 {
		t.Fatalf("expected 0 entries after flush, got %d", n)
	}
}

func TestPeerCache_close(t *testing.T) {
	c := newPeerCache()
	c.set(toKeyArray(genKey(t)), genKeyN(t, 1), 0)
	c.close()
	c.close() // double close must not panic
}

func TestPeerCache_cleanupRemovesExpired(t *testing.T) {
	origTTL := CacheTTL
	CacheTTL = 50 * time.Millisecond
	t.Cleanup(func() { CacheTTL = origTTL })

	c := newPeerCache()
	defer c.close()

	k := toKeyArray(genKey(t))
	c.set(k, genKeyN(t, 1), 0)

	// Wait for cleanup (runs every CacheTTL/2 = 25ms)
	time.Sleep(120 * time.Millisecond)

	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	if n != 0 {
		t.Fatalf("expected cleanup to remove expired entry, got %d entries", n)
	}
}

// // // // // // // // // //

func BenchmarkCacheGetSet(b *testing.B) {
	c := newPeerCache()
	defer c.close()
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	k := toKeyArray(pk)
	peers := []ed25519.PublicKey{pk}

	b.Run("set", func(b *testing.B) {
		for b.Loop() {
			c.set(k, peers, time.Millisecond)
		}
	})
	b.Run("get_hit", func(b *testing.B) {
		c.set(k, peers, time.Millisecond)
		for b.Loop() {
			c.get(k)
		}
	})
	b.Run("get_miss", func(b *testing.B) {
		pk2, _, _ := ed25519.GenerateKey(rand.Reader)
		k2 := toKeyArray(pk2)
		for b.Loop() {
			c.get(k2)
		}
	})
}
