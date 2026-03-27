package traceroute

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj is the traceroute module for exploring Yggdrasil network topology.
// Works directly with the core, without an admin socket.
// Tree() does BFS over peers via debug_remoteGetPeers.
// Path(), Hops(), Trace() work with local core data.
type Obj struct {
	core        *yggcore.Core
	logger      yggcore.Logger
	remotePeers yggcore.AddHandlerFunc
	cache       *peerCacheObj
}

// //

// adminCapture implements AddHandler to intercept handlers from core.SetAdmin.
// No real admin socket needed — just stores functions in a map.
type adminCapture struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCapture) AddHandler(name, desc string, args []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// // // // // // // // // //

const defaultPoolSize = 16

// //

// New creates a traceroute module.
// Captures debug_remoteGetPeers via core.SetAdmin without starting an admin socket.
func New(core *yggcore.Core, logger yggcore.Logger) (*Obj, error) {
	if core == nil {
		return nil, fmt.Errorf("traceroute: core is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("traceroute: logger is required")
	}

	capture := &adminCapture{handlers: make(map[string]yggcore.AddHandlerFunc)}
	_ = core.SetAdmin(capture)

	return &Obj{
		core:        core,
		logger:      logger,
		remotePeers: capture.handlers["debug_remoteGetPeers"],
		cache:       newPeerCache(),
	}, nil
}

// // // // // // // // // //

// Tree — builds a network topology tree via BFS.
// Root is our node; depth 1 is direct active peers from GetPeers().
// maxDepth > 0 required. concurrency <= 0 defaults to 16.
// Nodes that do not respond to peer queries are marked Unreachable.
func (o *Obj) Tree(ctx context.Context, maxDepth uint16, concurrency int) (*TreeResultObj, error) {
	return o.treeBFS(ctx, maxDepth, concurrency, nil)
}

// TreeChan is the same as Tree but sends a TreeProgressObj to ch after each depth level.
// Done=true on the last message. ch is not closed by TreeChan.
func (o *Obj) TreeChan(ctx context.Context, maxDepth uint16, concurrency int, ch chan<- TreeProgressObj) (*TreeResultObj, error) {
	return o.treeBFS(ctx, maxDepth, concurrency, ch)
}

// //

// treeBFS is the shared BFS implementation used by Tree and TreeChan.
// progress is nil-safe: no messages are sent when nil.
func (o *Obj) treeBFS(ctx context.Context, maxDepth uint16, concurrency int, progress chan<- TreeProgressObj) (*TreeResultObj, error) {
	if maxDepth == 0 {
		return nil, fmt.Errorf("traceroute: maxDepth must be > 0")
	}
	if concurrency <= 0 {
		concurrency = defaultPoolSize
	}

	pool := newWorkerPool(concurrency, o.callRemotePeers)
	defer pool.stop()

	selfKey := o.core.PublicKey()
	root := &NodeObj{Key: selfKey, Depth: 0}
	total := 0

	visited := make(map[[ed25519.PublicKeySize]byte]struct{})
	visited[toKeyArray(selfKey)] = struct{}{}

	// Level 1: active direct peers only.
	var currentLevel []*NodeObj
	for _, p := range o.core.GetPeers() {
		if !p.Up {
			continue
		}
		k := toKeyArray(p.Key)
		if _, seen := visited[k]; seen {
			continue
		}
		visited[k] = struct{}{}
		child := &NodeObj{Key: p.Key, Depth: 1}
		root.Children = append(root.Children, child)
		currentLevel = append(currentLevel, child)
	}
	total += len(currentLevel)
	if progress != nil && len(currentLevel) > 0 {
		progress <- TreeProgressObj{Depth: 1, Found: len(currentLevel), Total: total}
	}

	// BFS levels 2..maxDepth.
	for depth := uint16(1); depth < maxDepth && len(currentLevel) > 0; depth++ {
		if ctx.Err() != nil {
			break
		}
		currentLevel = o.scanLevel(ctx, pool, currentLevel, visited, int(depth)+1)
		total += len(currentLevel)
		if progress != nil && len(currentLevel) > 0 {
			progress <- TreeProgressObj{Depth: int(depth) + 1, Found: len(currentLevel), Total: total}
		}
	}

	if progress != nil {
		progress <- TreeProgressObj{Done: true, Total: total}
	}

	return &TreeResultObj{Root: root, Total: total}, nil
}

// //

// scanLevel queries peers of all nodes at the current BFS level via the worker pool.
// visited is updated sequentially after all results are collected — no mutex needed.
func (o *Obj) scanLevel(ctx context.Context, pool *workerPoolObj, nodes []*NodeObj, visited map[[ed25519.PublicKeySize]byte]struct{}, nextDepth int) []*NodeObj {
	nodeByKey := make(map[[ed25519.PublicKeySize]byte]*NodeObj, len(nodes))
	for _, n := range nodes {
		nodeByKey[toKeyArray(n.Key)] = n
	}

	results := make(chan peerResultObj, len(nodes))
	for _, node := range nodes {
		pool.submit(ctx, node.Key, results)
	}

	collected := make([]peerResultObj, 0, len(nodes))
	for range len(nodes) {
		collected = append(collected, <-results)
	}

	var nextLevel []*NodeObj
	for _, r := range collected {
		parent := nodeByKey[toKeyArray(r.key)]
		if r.err != nil {
			parent.Unreachable = true
			continue
		}
		for _, peerKey := range r.peers {
			k := toKeyArray(peerKey)
			if _, seen := visited[k]; seen {
				continue
			}
			visited[k] = struct{}{}
			child := &NodeObj{Key: peerKey, Depth: nextDepth}
			parent.Children = append(parent.Children, child)
			nextLevel = append(nextLevel, child)
		}
	}
	return nextLevel
}

// //

// callRemotePeers queries a remote node's peers via debug_remoteGetPeers.
// Called from pool workers. Returns immediately on ctx cancellation.
// The underlying o.remotePeers call (~6s yggdrasil timeout) may outlive the return —
// this is a bounded goroutine leak; the buffered channel prevents it from blocking.
func (o *Obj) callRemotePeers(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, error) {
	if o.remotePeers == nil {
		return nil, fmt.Errorf("traceroute: debug_remoteGetPeers unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	k := toKeyArray(key)
	if cached, ok := o.cache.get(k); ok {
		if cached == nil {
			return nil, fmt.Errorf("traceroute: node unreachable (cached)")
		}
		return cached, nil
	}

	req, _ := json.Marshal(map[string]string{"key": hex.EncodeToString(key)})

	type callResult struct {
		peers []ed25519.PublicKey
		err   error
	}
	ch := make(chan callResult, 1)

	go func() {
		raw, err := o.remotePeers(req)
		if err != nil {
			ch <- callResult{err: err}
			return
		}
		peers, err := parseRemotePeersResponse(raw)
		ch <- callResult{peers: peers, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			o.cache.set(k, nil)
			return nil, r.err
		}
		o.cache.set(k, r.peers)
		return r.peers, nil
	}
}

// //

// parseRemotePeersResponse parses the debug_remoteGetPeers response.
// Format: {"<ipv6>": {"keys": ["hex1", "hex2", ...]}}
// Uses JSON roundtrip because the raw type from yggdrasil is not guaranteed to be map[string]interface{}.
func parseRemotePeersResponse(raw interface{}) ([]ed25519.PublicKey, error) {
	js, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var outer map[string]struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(js, &outer); err != nil {
		return nil, err
	}

	var peers []ed25519.PublicKey
	for _, inner := range outer {
		for _, hexKey := range inner.Keys {
			kbs, err := hex.DecodeString(hexKey)
			if err != nil || len(kbs) != ed25519.PublicKeySize {
				continue
			}
			peers = append(peers, ed25519.PublicKey(kbs))
		}
	}
	return peers, nil
}

// // // // // // // // // //

func validateKey(key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("traceroute: invalid key length %d, expected %d", len(key), ed25519.PublicKeySize)
	}
	return nil
}

// //

// Path returns the node chain from the spanning tree root to the target key.
// Result: [root, ..., target]. Uses local GetTree().
func (o *Obj) Path(key ed25519.PublicKey) ([]*NodeObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	root, err := buildTree(o.core.GetTree())
	if err != nil {
		return nil, err
	}
	path := root.PathTo(key)
	if path == nil {
		return nil, fmt.Errorf("traceroute: key not found in tree")
	}
	return path, nil
}

// Hops returns the port-level route to the key from GetPaths().
// Each port is resolved to a key via GetPeers(). Requires a prior Lookup().
func (o *Obj) Hops(key ed25519.PublicKey) ([]HopObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	paths := o.core.GetPaths()
	target := toKeyArray(key)
	for _, p := range paths {
		if toKeyArray(p.Key) == target {
			return resolveHops(p, o.core.GetPeers()), nil
		}
	}
	return nil, fmt.Errorf("traceroute: no active path to key")
}

// Lookup initiates a path search to the key. Results appear in Hops() after some time.
func (o *Obj) Lookup(key ed25519.PublicKey) {
	o.core.PacketConn.PacketConn.SendLookup(key)
}

// Trace searches for the key in both spanning tree and pathfinder.
// Strategy: both available → return immediately; tree only → Lookup + wait 2s for hops;
// nothing → full poll with lookup retries every second until ctx expires.
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	result := o.collect(key)

	if result != nil && result.TreePath != nil && result.Hops != nil {
		return result, nil
	}

	o.Lookup(key)

	if result != nil {
		if result.Hops == nil {
			enriched := o.pollHops(ctx, key, 2*time.Second)
			if enriched != nil {
				result.Hops = enriched
			}
		}
		return result, nil
	}

	o.logger.Infof("[traceroute] lookup started for %x", key[:8])
	return o.pollFull(ctx, key)
}

// //

// pollHops waits for hops to appear within maxWait. One retry lookup after ~1s.
func (o *Obj) pollHops(ctx context.Context, key ed25519.PublicKey, maxWait time.Duration) []HopObj {
	startTime := time.Now()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	retried := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if time.Since(startTime) > maxWait {
				hops, _ := o.Hops(key)
				return hops
			}
			if hops, err := o.Hops(key); err == nil {
				return hops
			}
			if !retried && time.Since(startTime) > time.Second {
				o.Lookup(key)
				retried = true
			}
		}
	}
}

const (
	pollInterval     = 200 * time.Millisecond
	hopsGracePeriod  = 10 // ticks to wait for hops after tree is found
	lookupRetryEvery = time.Second
)

// pollFull polls for both tree path and hops until ctx expires.
// Single flat loop: once tree is found, gives hopsGracePeriod extra ticks for hops.
func (o *Obj) pollFull(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	lastLookup := time.Now()
	graceTicks := -1

	for {
		select {
		case <-ctx.Done():
			if result := o.collect(key); result != nil {
				return result, nil
			}
			return nil, fmt.Errorf("traceroute: lookup timed out for key %x", key[:8])
		case <-ticker.C:
			result := o.collect(key)

			if result != nil && result.TreePath != nil && result.Hops != nil {
				return result, nil
			}

			if result != nil && result.TreePath != nil && graceTicks < 0 {
				graceTicks = hopsGracePeriod
				o.Lookup(key)
				lastLookup = time.Now()
			}

			if graceTicks > 0 {
				graceTicks--
			} else if graceTicks == 0 {
				if result == nil {
					result = o.collect(key)
				}
				if result != nil {
					return result, nil
				}
				graceTicks = -1
			}

			if time.Since(lastLookup) >= lookupRetryEvery {
				o.Lookup(key)
				lastLookup = time.Now()
			}
		}
	}
}

// //

// collect attempts to gather data from both tree and pathfinder sources.
func (o *Obj) collect(key ed25519.PublicKey) *TraceResultObj {
	var result TraceResultObj
	if path, err := o.Path(key); err == nil {
		result.TreePath = path
	}
	if hops, err := o.Hops(key); err == nil {
		result.Hops = hops
	}
	if result.TreePath != nil || result.Hops != nil {
		return &result
	}
	return nil
}

// // // // // // // // // //

// Peers returns direct peers from the core.
func (o *Obj) Peers() []yggcore.PeerInfo {
	return o.core.GetPeers()
}

// Sessions returns active sessions from the core.
func (o *Obj) Sessions() []yggcore.SessionInfo {
	return o.core.GetSessions()
}
