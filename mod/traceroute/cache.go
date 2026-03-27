package traceroute

import (
	"crypto/ed25519"
	"sync"
	"sync/atomic"
	"time"
)

// // // // // // // // // //

// CacheTTL controls how long peer query results are cached.
var CacheTTL = 60 * time.Second

// //

type peerCacheEntryObj struct {
	peers []ed25519.PublicKey // nil means unreachable
	at    time.Time
}

type peerCacheObj struct {
	mu        sync.RWMutex
	entries   map[[ed25519.PublicKeySize]byte]peerCacheEntryObj
	running   atomic.Int32
	idle      atomic.Int32
	done      chan struct{}
	closeOnce sync.Once
}

// // // // // // // // // //

func newPeerCache() *peerCacheObj {
	return &peerCacheObj{
		entries: make(map[[ed25519.PublicKeySize]byte]peerCacheEntryObj),
		done:    make(chan struct{}),
	}
}

// //

func (c *peerCacheObj) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// //

// get returns cached peers and true if the entry exists and has not expired.
// nil peers with ok=true means the node was unreachable.
func (c *peerCacheObj) get(key [ed25519.PublicKeySize]byte) ([]ed25519.PublicKey, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Since(e.at) >= CacheTTL {
		return nil, false
	}
	return e.peers, true
}

// flush drops all cached entries.
func (c *peerCacheObj) flush() {
	c.mu.Lock()
	c.entries = make(map[[ed25519.PublicKeySize]byte]peerCacheEntryObj)
	c.mu.Unlock()
}

// set stores a peer list (or nil for unreachable) and ensures the cleanup goroutine is running.
func (c *peerCacheObj) set(key [ed25519.PublicKeySize]byte, peers []ed25519.PublicKey) {
	c.mu.Lock()
	c.entries[key] = peerCacheEntryObj{peers: peers, at: time.Now()}
	c.mu.Unlock()

	c.idle.Store(0)
	if c.running.CompareAndSwap(0, 1) {
		go c.cleanup()
	}
}

// //

// cleanup removes expired entries every CacheTTL/2.
// Exits after 10 consecutive iterations with an empty cache or when done is closed.
func (c *peerCacheObj) cleanup() {
	ticker := time.NewTicker(CacheTTL / 2)
	defer ticker.Stop()
	defer c.running.Store(0)

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
		}

		now := time.Now()
		c.mu.Lock()
		for k, e := range c.entries {
			if now.Sub(e.at) >= CacheTTL {
				delete(c.entries, k)
			}
		}
		empty := len(c.entries) == 0
		if empty {
			c.entries = make(map[[ed25519.PublicKeySize]byte]peerCacheEntryObj)
		}
		c.mu.Unlock()

		if empty {
			if c.idle.Add(1) >= 10 {
				return
			}
		} else {
			c.idle.Store(0)
		}
	}
}
