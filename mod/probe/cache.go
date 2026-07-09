package probe

import (
	"crypto/ed25519"
	"sync"
	"time"
)

// // // // // // // // // //

const (
	defaultCacheTTL        = 60 * time.Second
	defaultCacheMaxEntries = 4096
	defaultCacheMaxBytes   = 64 * 1024 * 1024
)

// //

type peerCacheEntryObj struct {
	peers []ed25519.PublicKey // nil means unreachable
	rtt   time.Duration
	at    time.Time
	bytes int
	seq   uint64
}

type peerCacheObj struct {
	mu        sync.RWMutex
	entries   map[[ed25519.PublicKeySize]byte]peerCacheEntryObj
	order     []peerCacheOrderObj
	ttl       time.Duration
	max       int
	maxBytes  int
	bytes     int
	seq       uint64
	closed    bool
	wg        sync.WaitGroup
	done      chan struct{}
	closeOnce sync.Once
}

type peerCacheOrderObj struct {
	key [ed25519.PublicKeySize]byte
	seq uint64
}

// // // // // // // // // //

func newPeerCache(ttl time.Duration, maxEntries int) *peerCacheObj {
	if maxEntries <= 0 {
		maxEntries = defaultCacheMaxEntries
	}
	c := &peerCacheObj{
		entries:  make(map[[ed25519.PublicKeySize]byte]peerCacheEntryObj),
		ttl:      ttl,
		max:      maxEntries,
		maxBytes: defaultCacheMaxBytes,
		done:     make(chan struct{}),
	}
	c.wg.Add(1)
	go c.cleanup()
	return c
}

// //

func (c *peerCacheObj) close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.entries = nil
		c.order = nil
		c.bytes = 0
		c.mu.Unlock()
		close(c.done)
	})
	c.wg.Wait()
}

// //

// get returns cached peers and RTT. nil peers with ok=true means unreachable.
func (c *peerCacheObj) get(key [ed25519.PublicKeySize]byte) ([]ed25519.PublicKey, time.Duration, bool) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, 0, false
	}
	e, ok := c.entries[key]
	ttl := c.ttl
	c.mu.RUnlock()
	if !ok || time.Since(e.at) >= ttl {
		return nil, 0, false
	}
	return e.peers, e.rtt, true
}

// flush drops all cached entries.
func (c *peerCacheObj) flush() {
	c.mu.Lock()
	if !c.closed {
		c.entries = make(map[[ed25519.PublicKeySize]byte]peerCacheEntryObj)
		c.order = nil
		c.bytes = 0
	}
	c.mu.Unlock()
}

// ttlValue returns the immutable cache TTL.
func (c *peerCacheObj) ttlValue() time.Duration {
	return c.ttl
}

func peerCacheEntryBytes(peers []ed25519.PublicKey) int {
	if len(peers) == 0 {
		return 0
	}
	return len(peers) * (ed25519.PublicKeySize + 24)
}

func clonePeerKeys(peers []ed25519.PublicKey) []ed25519.PublicKey {
	if peers == nil {
		return nil
	}
	out := make([]ed25519.PublicKey, len(peers))
	for i, peer := range peers {
		out[i] = append(ed25519.PublicKey(nil), peer...)
	}
	return out
}

func (c *peerCacheObj) evictOneLocked() bool {
	for len(c.order) > 0 {
		item := c.order[0]
		c.order[0] = peerCacheOrderObj{}
		c.order = c.order[1:]
		if entry, ok := c.entries[item.key]; ok && entry.seq == item.seq {
			delete(c.entries, item.key)
			c.bytes -= entry.bytes
			return true
		}
	}
	for key, entry := range c.entries {
		delete(c.entries, key)
		c.bytes -= entry.bytes
		return true
	}
	return false
}

func (c *peerCacheObj) compactOrderLocked() {
	live := len(c.entries)
	if live == 0 {
		c.order = nil
		return
	}
	if len(c.order) <= live*2 {
		return
	}
	old := c.order
	compact := old[:0]
	for _, item := range old {
		if entry, ok := c.entries[item.key]; ok && entry.seq == item.seq {
			compact = append(compact, item)
		}
	}
	for i := len(compact); i < len(old); i++ {
		old[i] = peerCacheOrderObj{}
	}
	c.order = compact
}

// set stores a peer list. nil peers means unreachable.
func (c *peerCacheObj) set(key [ed25519.PublicKeySize]byte, peers []ed25519.PublicKey, rtt time.Duration) {
	c.mu.Lock()
	if !c.closed {
		if old, exists := c.entries[key]; exists {
			c.bytes -= old.bytes
			delete(c.entries, key)
		}
		entryBytes := peerCacheEntryBytes(peers)
		for len(c.entries) >= c.max {
			if !c.evictOneLocked() {
				break
			}
		}
		for c.maxBytes > 0 && c.bytes+entryBytes > c.maxBytes && len(c.entries) > 0 {
			if !c.evictOneLocked() {
				break
			}
		}
		c.seq++
		entry := peerCacheEntryObj{peers: clonePeerKeys(peers), rtt: rtt, at: time.Now(), bytes: entryBytes, seq: c.seq}
		c.entries[key] = entry
		c.order = append(c.order, peerCacheOrderObj{key: key, seq: entry.seq})
		c.bytes += entryBytes
		c.compactOrderLocked()
	}
	c.mu.Unlock()
}

// //

// cleanupInterval derives the fixed sweep interval from the immutable TTL.
func (c *peerCacheObj) cleanupInterval() time.Duration {
	interval := c.ttl / 2
	if interval <= 0 {
		interval = c.ttl
	}
	if interval <= 0 {
		interval = defaultCacheTTL / 2
	}
	return interval
}

func stopCleanupTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

// cleanup removes expired entries on a fixed interval derived from the TTL.
func (c *peerCacheObj) cleanup() {
	defer c.wg.Done()

	interval := c.cleanupInterval()
	timer := time.NewTimer(interval)
	defer stopCleanupTimer(timer)

	for {
		select {
		case <-c.done:
			return
		case <-timer.C:
		}

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		now := time.Now()
		for k, e := range c.entries {
			if now.Sub(e.at) >= c.ttl {
				delete(c.entries, k)
				c.bytes -= e.bytes
			}
		}
		if len(c.entries) == 0 {
			c.entries = make(map[[ed25519.PublicKeySize]byte]peerCacheEntryObj)
			c.order = nil
			c.bytes = 0
		} else {
			c.compactOrderLocked()
		}
		c.mu.Unlock()
		timer.Reset(interval)
	}
}
