package traceroute

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
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

// // // // // // // // // //

const defaultPoolSize = 16

// MaxPeersPerNode limits how many peers are accepted from a single remote node.
// If a node reports more peers than this limit, it is marked as Unreachable.
var MaxPeersPerNode = 65535

// //

// New creates a traceroute module.
// Captures debug_remoteGetPeers via core.SetAdmin without starting an admin socket.
func New(core *yggcore.Core, logger yggcore.Logger) (*Obj, error) {
	if core == nil {
		return nil, ErrCoreRequired
	}
	if logger == nil {
		return nil, ErrLoggerRequired
	}
	if CacheTTL < time.Second {
		return nil, ErrInvalidCacheTTL
	}

	capture := &adminCaptureObj{handlers: make(map[string]yggcore.AddHandlerFunc)}
	_ = core.SetAdmin(capture)

	remotePeers := capture.handlers["debug_remoteGetPeers"]
	if remotePeers == nil {
		return nil, ErrRemotePeersNotCaptured
	}

	return &Obj{
		core:        core,
		logger:      logger,
		remotePeers: remotePeers,
		cache:       newPeerCache(),
	}, nil
}

// //

// Close stops the cache cleanup goroutine and releases resources.
func (o *Obj) Close() {
	o.cache.close()
}

// FlushCache drops all cached peer query results.
func (o *Obj) FlushCache() {
	o.cache.flush()
}

// // // // // // // // // //

// Tree builds a network topology tree via BFS.
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
		return nil, ErrMaxDepthRequired
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

	// Level 1: active direct peers only. RTT from core's measured latency.
	var currentLevel []*NodeObj
	for _, p := range o.core.GetPeers() {
		if !p.Up || len(p.Key) != ed25519.PublicKeySize {
			continue
		}
		k := toKeyArray(p.Key)
		if _, seen := visited[k]; seen {
			continue
		}
		visited[k] = struct{}{}
		child := &NodeObj{Key: p.Key, Parent: selfKey, Depth: 1, RTT: p.Latency}
		root.Children = append(root.Children, child)
		currentLevel = append(currentLevel, child)
	}
	total += len(currentLevel)
	if progress != nil && len(currentLevel) > 0 {
		select {
		case progress <- TreeProgressObj{Depth: 1, Found: len(currentLevel), Total: total}:
		case <-ctx.Done():
			return &TreeResultObj{Root: root, Total: total}, nil
		}
	}

	// BFS levels 2..maxDepth.
	for depth := uint16(1); depth < maxDepth && len(currentLevel) > 0; depth++ {
		if ctx.Err() != nil {
			break
		}
		currentLevel = o.scanLevel(ctx, pool, currentLevel, visited, int(depth)+1)
		total += len(currentLevel)
		if progress != nil && len(currentLevel) > 0 {
			select {
			case progress <- TreeProgressObj{Depth: int(depth) + 1, Found: len(currentLevel), Total: total}:
			case <-ctx.Done():
				return &TreeResultObj{Root: root, Total: total}, nil
			}
		}
	}

	if progress != nil {
		select {
		case progress <- TreeProgressObj{Done: true, Total: total}:
		case <-ctx.Done():
		}
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
		select {
		case r := <-results:
			collected = append(collected, r)
		case <-ctx.Done():
			return nil
		}
	}

	limit := MaxPeersPerNode
	var nextLevel []*NodeObj
	for _, r := range collected {
		parent := nodeByKey[toKeyArray(r.key)]
		parent.RTT = r.rtt
		if r.err != nil {
			parent.Unreachable = true
			continue
		}
		if limit > 0 && len(r.peers) > limit {
			parent.Unreachable = true
			o.logger.Warnf("[traceroute] node %x reported %d peers (limit %d), marked unreachable", r.key[:8], len(r.peers), limit)
			continue
		}
		for _, peerKey := range r.peers {
			if len(peerKey) != ed25519.PublicKeySize {
				continue
			}
			k := toKeyArray(peerKey)
			if _, seen := visited[k]; seen {
				continue
			}
			visited[k] = struct{}{}
			child := &NodeObj{Key: peerKey, Parent: parent.Key, Depth: nextDepth}
			parent.Children = append(parent.Children, child)
			nextLevel = append(nextLevel, child)
		}
	}
	return nextLevel
}

// // // // // // // // // //

func validateKey(key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: got %d, expected %d", ErrInvalidKeyLength, len(key), ed25519.PublicKeySize)
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
	root, err := buildTree(o.core.GetTree(), o.logger)
	if err != nil {
		return nil, err
	}
	path := root.PathTo(key)
	if path == nil {
		return nil, ErrKeyNotInTree
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
	return nil, ErrNoActivePath
}

// Lookup initiates a path search to the key. Results appear in Hops() after some time.
func (o *Obj) Lookup(key ed25519.PublicKey) {
	o.core.PacketConn.PacketConn.SendLookup(key)
}

// // // // // // // // // //

// Self returns this node's public key and routing info.
func (o *Obj) Self() yggcore.SelfInfo {
	return o.core.GetSelf()
}

// Address returns this node's Yggdrasil IPv6 address.
func (o *Obj) Address() net.IP {
	return o.core.Address()
}

// Subnet returns this node's Yggdrasil IPv6 subnet.
func (o *Obj) Subnet() net.IPNet {
	return o.core.Subnet()
}

// Peers returns direct peers from the core.
func (o *Obj) Peers() []yggcore.PeerInfo {
	return o.core.GetPeers()
}

// Sessions returns active sessions from the core.
func (o *Obj) Sessions() []yggcore.SessionInfo {
	return o.core.GetSessions()
}

// SpanningTree returns the raw spanning tree entries from the core.
func (o *Obj) SpanningTree() []yggcore.TreeEntryInfo {
	return o.core.GetTree()
}

// Paths returns active pathfinder routes from the core.
func (o *Obj) Paths() []yggcore.PathEntryInfo {
	return o.core.GetPaths()
}
