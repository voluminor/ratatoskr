package probe

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"testing"
	"time"
)

// // // // // // // // // //

func cacheTestKey(seed int) ed25519.PublicKey {
	key := make(ed25519.PublicKey, ed25519.PublicKeySize)
	binary.LittleEndian.PutUint64(key, uint64(seed)+1)
	return key
}

// // // // // // // // // //

func TestPeerCache_getSet(t *testing.T) {
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
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

func TestPeerCache_setStoresCopy(t *testing.T) {
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
	defer c.close()

	k := toKeyArray(genKey(t))
	peers := genKeyN(t, 2)
	firstByte := peers[0][0]
	c.set(k, peers, 0)
	peers[0][0] ^= 0xff

	got, _, ok := c.get(k)
	if !ok {
		t.Fatal("expected hit")
	}
	if got[0][0] != firstByte {
		t.Fatal("cached peer key was mutated through set input")
	}
}

func TestPeerCache_unreachable(t *testing.T) {
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
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
	c := newPeerCache(50*time.Millisecond, defaultCacheMaxEntries)
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
	c := newPeerCache(50*time.Millisecond, defaultCacheMaxEntries)
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
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
	c.set(toKeyArray(genKey(t)), genKeyN(t, 1), 0)
	c.close()
	c.close() // double close must not panic
}

func TestPeerCache_cleanupRemovesExpired(t *testing.T) {
	c := newPeerCache(50*time.Millisecond, defaultCacheMaxEntries)
	defer c.close()

	k := toKeyArray(genKey(t))
	c.set(k, genKeyN(t, 1), 0)

	// Wait for cleanup (runs every ttl/2 = 25ms)
	time.Sleep(120 * time.Millisecond)

	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	if n != 0 {
		t.Fatalf("expected cleanup to remove expired entry, got %d entries", n)
	}
}

func TestPeerCache_usesOwnTTL(t *testing.T) {
	c := newPeerCache(time.Hour, defaultCacheMaxEntries)
	defer c.close()

	k := toKeyArray(genKey(t))
	c.set(k, genKeyN(t, 1), 0)
	time.Sleep(5 * time.Millisecond)

	if _, _, ok := c.get(k); !ok {
		t.Fatal("expected cache to use its own TTL, not package global CacheTTL")
	}
}

func TestPeerCache_setAfterCloseIgnored(t *testing.T) {
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
	k := toKeyArray(genKey(t))

	c.close()
	c.set(k, genKeyN(t, 1), 0)

	if _, _, ok := c.get(k); ok {
		t.Fatal("expected set after close to be ignored")
	}
}

func TestPeerCache_concurrentSetClose(t *testing.T) {
	for range 100 {
		c := newPeerCache(time.Minute, defaultCacheMaxEntries)
		start := make(chan struct{})
		done := make(chan struct{}, 8)
		keys := make([]ed25519.PublicKey, 8*50)
		peers := make([][]ed25519.PublicKey, len(keys))
		for i := range keys {
			keys[i] = cacheTestKey(i)
			peers[i] = []ed25519.PublicKey{cacheTestKey(1000 + i)}
		}

		for worker := range 8 {
			worker := worker
			go func() {
				defer func() { done <- struct{}{} }()
				<-start
				for i := range 50 {
					idx := worker*50 + i
					c.set(toKeyArray(keys[idx]), peers[idx], 0)
				}
			}()
		}

		close(start)
		c.close()
		for range 8 {
			<-done
		}
	}
}

func TestPeerCache_maxEntries(t *testing.T) {
	c := newPeerCache(time.Minute, 2)
	defer c.close()

	for i := range 8 {
		c.set(toKeyArray(cacheTestKey(i)), []ed25519.PublicKey{cacheTestKey(100 + i)}, time.Millisecond)
	}
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	if n > 2 {
		t.Fatalf("expected cache cap 2, got %d", n)
	}
}

func TestPeerCache_maxBytes(t *testing.T) {
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
	defer c.close()
	c.maxBytes = peerCacheEntryBytes(genKeyN(t, 2))

	for i := range 8 {
		c.set(toKeyArray(cacheTestKey(i)), genKeyN(t, 2), time.Millisecond)
	}
	c.mu.RLock()
	bytes := c.bytes
	entries := len(c.entries)
	c.mu.RUnlock()
	if bytes > c.maxBytes {
		t.Fatalf("expected cache bytes <= %d, got %d", c.maxBytes, bytes)
	}
	if entries == 0 {
		t.Fatal("byte budget should keep the newest fitting entry")
	}
}

func TestPeerCache_updateExistingDoesNotEvict(t *testing.T) {
	c := newPeerCache(time.Minute, 2)
	defer c.close()

	a := toKeyArray(cacheTestKey(1))
	b := toKeyArray(cacheTestKey(2))
	c.set(a, []ed25519.PublicKey{cacheTestKey(101)}, time.Millisecond)
	c.set(b, []ed25519.PublicKey{cacheTestKey(102)}, time.Millisecond)
	c.set(a, []ed25519.PublicKey{cacheTestKey(103)}, 2*time.Millisecond)

	if _, rtt, ok := c.get(a); !ok || rtt != 2*time.Millisecond {
		t.Fatalf("expected updated entry a, ok=%v rtt=%s", ok, rtt)
	}
	if _, _, ok := c.get(b); !ok {
		t.Fatal("updating existing key should not evict key b")
	}
}

func TestPeerCache_compactsOrderAfterRepeatedUpdates(t *testing.T) {
	c := newPeerCache(time.Minute, 128)
	defer c.close()

	key := toKeyArray(cacheTestKey(1))
	for i := 0; i < 64; i++ {
		c.set(key, []ed25519.PublicKey{cacheTestKey(100 + i)}, time.Millisecond)
	}

	c.mu.RLock()
	entries := len(c.entries)
	order := len(c.order)
	c.mu.RUnlock()
	if entries != 1 {
		t.Fatalf("expected one live entry, got %d", entries)
	}
	if order > entries*2 {
		t.Fatalf("cache order retained stale entries: order=%d live=%d", order, entries)
	}
}

// // // // // // // // // //

func BenchmarkCacheGetSet(b *testing.B) {
	c := newPeerCache(time.Minute, defaultCacheMaxEntries)
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
