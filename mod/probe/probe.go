package probe

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj explores Yggdrasil network topology without an admin socket.
// Tree() does BFS over peers via debug_remoteGetPeers.
// Path(), Hops(), Trace() work with local core data.
type Obj struct {
	source           SourceInterface
	logger           yggcore.Logger
	remotePeers      yggcore.AddHandlerFunc
	remoteSem        chan struct{}
	remoteMu         sync.RWMutex
	remoteWG         sync.WaitGroup
	closeOnce        sync.Once
	closed           bool
	maxTotalNodes    int
	pollInterval     time.Duration
	lookupRetryEvery time.Duration
	maxDuration      time.Duration
}

// SourceInterface is the core access needed by topology probing.
type SourceInterface interface {
	SetAdmin(yggcore.AddHandler) error
	SendLookup(key ed25519.PublicKey)
	Address() net.IP
	Subnet() net.IPNet
	PublicKey() ed25519.PublicKey
	GetSelf() yggcore.SelfInfo
	GetPeers() []yggcore.PeerInfo
	GetSessions() []yggcore.SessionInfo
	GetTree() []yggcore.TreeEntryInfo
	GetPaths() []yggcore.PathEntryInfo
}

// // // // // // // // // //

const (
	defaultPoolSize = 16

	DefaultMaxPeersPerNode = 1024
	DefaultMaxTotalNodes   = 4096
	DefaultMaxConcurrency  = 256

	defaultMaxDuration = 5 * time.Minute

	// closeWait bounds Close's wait for in-flight remote calls; the captured
	// handler has no context, so a stuck call cannot be cancelled.
	closeWait = 2 * time.Second
)

// //

func validateKey(key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: got %d, expected %d", ErrInvalidKeyLength, len(key), ed25519.PublicKeySize)
	}
	return nil
}

func compareKeys(a, b ed25519.PublicKey) int {
	return bytes.Compare(a, b)
}

func sortNodes(nodes []*NodeObj) {
	slices.SortFunc(nodes, func(a, b *NodeObj) int {
		return compareKeys(a.Key, b.Key)
	})
}

// clonePeerKeys copies the key slice so downstream sorting never reorders the
// remote call result in place.
func clonePeerKeys(keys []ed25519.PublicKey) []ed25519.PublicKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]ed25519.PublicKey, len(keys))
	copy(out, keys)
	return out
}

// peerResultObj is the outcome of a single remote peer query.
type peerResultObj struct {
	key   ed25519.PublicKey
	peers []ed25519.PublicKey
	rtt   time.Duration
	err   error
}

// remoteCallFunc queries a node's peers; callRemotePeers is the production impl.
type remoteCallFunc func(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error)

func (o *Obj) effectiveConcurrency(concurrency int) int {
	if concurrency <= 0 {
		return defaultPoolSize
	}
	// Bound the BFS worker fan-out; the global remoteSem is the hard cap on
	// concurrent remote calls, this just keeps the goroutine count sane.
	if concurrency > DefaultMaxConcurrency {
		return DefaultMaxConcurrency
	}
	return concurrency
}

func (o *Obj) boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok && o.maxDuration > 0 {
		return context.WithTimeout(ctx, o.maxDuration)
	}
	return ctx, func() {}
}

// // // // // // // // // //

// ConfigObj tunes a probe. Zero values fall back to internal defaults.
type ConfigObj struct {
	// Logger receives probe events; nil → logs are discarded.
	Logger yggcore.Logger
	// MaxTotalNodes caps how many nodes the tree crawl visits; 0 → default.
	MaxTotalNodes int
	// PollInterval is the Trace poll ticker interval; 0 → default.
	PollInterval time.Duration
	// LookupRetryEvery is how often Trace re-issues a path lookup; 0 → default.
	LookupRetryEvery time.Duration
	// MaxDuration bounds a probe when the caller sets no ctx deadline;
	// 0 → default, <0 → unbounded.
	MaxDuration time.Duration
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func orDefaultDuration(v, def time.Duration) time.Duration {
	if v == 0 {
		return def
	}
	return v
}

// //

// New creates a probe module over the given core source. cfg tunes crawl timing,
// the total-node cap, and the logger. Captures debug_remoteGetPeers via
// core.SetAdmin. The per-node peer cap and hops wait are fixed package constants
// (topology data comes from untrusted remote nodes), not caller knobs.
func New(source SourceInterface, cfg ConfigObj) (*Obj, error) {
	if source == nil {
		return nil, ErrCoreRequired
	}
	logger := common.NormalizeLogger(cfg.Logger)

	capture := common.NewAdminCapture()
	if err := source.SetAdmin(capture); err != nil {
		return nil, fmt.Errorf("probe: capture admin handlers: %w", err)
	}

	remotePeers := capture.Handlers["debug_remoteGetPeers"]
	if remotePeers == nil {
		return nil, ErrRemotePeersNotCaptured
	}

	return &Obj{
		source:           source,
		logger:           logger,
		remotePeers:      remotePeers,
		remoteSem:        make(chan struct{}, DefaultMaxConcurrency),
		maxTotalNodes:    orDefaultInt(cfg.MaxTotalNodes, DefaultMaxTotalNodes),
		pollInterval:     orDefaultDuration(cfg.PollInterval, defaultPollInterval),
		lookupRetryEvery: orDefaultDuration(cfg.LookupRetryEvery, defaultLookupRetryEvery),
		maxDuration:      orDefaultDuration(cfg.MaxDuration, defaultMaxDuration),
	}, nil
}

// //

// Close stops the cache cleanup and waits, bounded by closeWait, for in-flight
// remote calls. The captured debug_remoteGetPeers handler has no context, so a
// stuck call cannot be cancelled; Close gives up rather than blocking forever.
func (o *Obj) Close() {
	o.closeOnce.Do(func() {
		o.remoteMu.Lock()
		o.closed = true
		o.remoteMu.Unlock()

		done := make(chan struct{})
		go func() {
			o.remoteWG.Wait()
			close(done)
		}()
		timer := time.NewTimer(closeWait)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			o.logger.Warnf("[probe] close timed out waiting for in-flight remote calls")
		}
	})
}

// // // // // // // // // //

// Tree builds a network topology tree via BFS from our node as root.
// maxDepth > 0 required. concurrency <= 0 defaults to 16.
func (o *Obj) Tree(ctx context.Context, maxDepth uint16, concurrency int) (*TreeResultObj, error) {
	return o.treeBFS(ctx, maxDepth, concurrency, nil)
}

// TreeChan is Tree with progress: sends TreeProgressObj after each depth level.
// Done=true on the last message. Closes ch before returning.
func (o *Obj) TreeChan(ctx context.Context, maxDepth uint16, concurrency int, ch chan<- TreeProgressObj) (*TreeResultObj, error) {
	if ch != nil {
		defer close(ch)
	}
	return o.treeBFS(ctx, maxDepth, concurrency, ch)
}

// //

// treeBFS is the shared BFS implementation. progress is nil-safe.
func (o *Obj) treeBFS(ctx context.Context, maxDepth uint16, concurrency int, progress chan<- TreeProgressObj) (*TreeResultObj, error) {
	if maxDepth == 0 {
		return nil, ErrMaxDepthRequired
	}
	var cancel context.CancelFunc
	ctx, cancel = o.boundedContext(ctx)
	defer cancel()

	selfKey := o.source.PublicKey()
	root := &NodeObj{Key: selfKey, Depth: 0}
	total := 0
	truncated := false

	visited := make(map[[ed25519.PublicKeySize]byte]struct{})
	visited[toKeyArray(selfKey)] = struct{}{}

	directPeers := make([]yggcore.PeerInfo, 0)
	directSeen := make(map[[ed25519.PublicKeySize]byte]struct{})
	for _, p := range o.source.GetPeers() {
		if !p.Up || len(p.Key) != ed25519.PublicKeySize {
			continue
		}
		k := toKeyArray(p.Key)
		if _, seen := visited[k]; seen {
			continue
		}
		if _, dup := directSeen[k]; dup {
			continue
		}
		directSeen[k] = struct{}{}
		directPeers = append(directPeers, p)
	}
	// Sort ascending so the node cap keeps a deterministic, lowest-key subset.
	slices.SortFunc(directPeers, func(a, b yggcore.PeerInfo) int {
		return compareKeys(a.Key, b.Key)
	})

	var currentLevel []*NodeObj
	for _, p := range directPeers {
		if total >= o.maxTotalNodes {
			truncated = true
			break
		}
		visited[toKeyArray(p.Key)] = struct{}{}
		child := &NodeObj{Key: p.Key, Parent: selfKey, Depth: 1, RTT: p.Latency}
		root.Children = append(root.Children, child)
		currentLevel = append(currentLevel, child)
		total++
	}
	sortNodes(root.Children)
	sortNodes(currentLevel)
	if progress != nil && len(currentLevel) > 0 {
		select {
		case progress <- TreeProgressObj{Depth: 1, Found: len(currentLevel), Total: total, Truncated: truncated, Limit: o.maxTotalNodes}:
		case <-ctx.Done():
			return &TreeResultObj{Root: root, Total: total, Truncated: truncated, Limit: o.maxTotalNodes}, ctx.Err()
		}
	}

	for depth := uint16(1); depth < maxDepth && len(currentLevel) > 0 && !truncated; depth++ {
		if err := ctx.Err(); err != nil {
			return &TreeResultObj{Root: root, Total: total, Truncated: truncated, Limit: o.maxTotalNodes}, err
		}
		remaining := o.maxTotalNodes - total
		if remaining <= 0 {
			truncated = true
			break
		}
		var levelTruncated bool
		var nextLevel []*NodeObj
		var err error
		nextLevel, levelTruncated, err = o.scanLevel(ctx, o.callRemotePeers, currentLevel, visited, int(depth)+1, remaining, concurrency)
		currentLevel = nextLevel
		truncated = levelTruncated
		total += len(currentLevel)
		if err != nil {
			return &TreeResultObj{Root: root, Total: total, Truncated: truncated, Limit: o.maxTotalNodes}, err
		}
		if progress != nil && len(currentLevel) > 0 {
			select {
			case progress <- TreeProgressObj{Depth: int(depth) + 1, Found: len(currentLevel), Total: total, Truncated: truncated, Limit: o.maxTotalNodes}:
			case <-ctx.Done():
				return &TreeResultObj{Root: root, Total: total, Truncated: truncated, Limit: o.maxTotalNodes}, ctx.Err()
			}
		}
	}

	if truncated {
		o.logger.Warnf("[probe] tree traversal reached node limit %d, result truncated", o.maxTotalNodes)
	}
	if progress != nil {
		select {
		case progress <- TreeProgressObj{Done: true, Total: total, Truncated: truncated, Limit: o.maxTotalNodes}:
		case <-ctx.Done():
		}
	}

	return &TreeResultObj{Root: root, Total: total, Truncated: truncated, Limit: o.maxTotalNodes}, nil
}

// //

// scanLevel queries the peers of a BFS level with a fixed worker pool. Results
// are applied serially in arrival order, so shared BFS state needs no extra
// locking. Truncation short-circuits application; a cancelled context still
// applies every result already produced before surfacing the error.
func (o *Obj) scanLevel(ctx context.Context, call remoteCallFunc, nodes []*NodeObj, visited map[[ed25519.PublicKeySize]byte]struct{}, nextDepth int, remaining int, concurrency int) ([]*NodeObj, bool, error) {
	if remaining <= 0 {
		return nil, true, nil
	}
	nodeByKey := make(map[[ed25519.PublicKeySize]byte]*NodeObj, len(nodes))
	for _, n := range nodes {
		nodeByKey[toKeyArray(n.Key)] = n
	}

	// levelCtx is cancelled once the level truncates so in-flight remote calls
	// return promptly instead of running to completion for discarded results.
	levelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan ed25519.PublicKey, len(nodes))
	for _, n := range nodes {
		jobs <- n.Key
	}
	close(jobs)

	workerCount := o.effectiveConcurrency(concurrency)
	if workerCount > len(nodes) {
		workerCount = len(nodes)
	}
	// Buffered to len(nodes) so no worker blocks on send even if we stop reading
	// early on truncation; every queued node emits exactly one result.
	results := make(chan peerResultObj, len(nodes))
	for range workerCount {
		go func() {
			for key := range jobs {
				peers, rtt, err := call(levelCtx, key)
				results <- peerResultObj{key: key, peers: peers, rtt: rtt, err: err}
			}
		}()
	}

	var nextLevel []*NodeObj
	truncated := false
	for range nodes {
		r := <-results
		children, childTruncated := o.applyPeerResult(r, nodeByKey, visited, nextDepth, remaining-len(nextLevel))
		nextLevel = append(nextLevel, children...)
		if childTruncated {
			truncated = true
			cancel()
			break
		}
	}
	sortNodes(nextLevel)
	if truncated {
		return nextLevel, true, nil
	}
	if err := ctx.Err(); err != nil {
		return nextLevel, truncated, err
	}
	return nextLevel, truncated, nil
}

func (o *Obj) applyPeerResult(r peerResultObj, nodeByKey map[[ed25519.PublicKeySize]byte]*NodeObj, visited map[[ed25519.PublicKeySize]byte]struct{}, nextDepth int, remaining int) ([]*NodeObj, bool) {
	if remaining <= 0 {
		return nil, true
	}
	parent := nodeByKey[toKeyArray(r.key)]
	if parent == nil {
		return nil, false
	}
	parent.RTT = r.rtt
	if r.err != nil {
		parent.Unreachable = true
		return nil, false
	}
	peers := clonePeerKeys(r.peers)
	slices.SortFunc(peers, compareKeys)
	children := make([]*NodeObj, 0, len(peers))
	for _, peerKey := range peers {
		if len(peerKey) != ed25519.PublicKeySize {
			continue
		}
		k := toKeyArray(peerKey)
		if _, seen := visited[k]; seen {
			continue
		}
		if len(children) >= remaining {
			return children, true
		}
		visited[k] = struct{}{}
		child := &NodeObj{Key: peerKey, Parent: parent.Key, Depth: nextDepth}
		parent.Children = append(parent.Children, child)
		children = append(children, child)
	}
	sortNodes(parent.Children)
	return children, false
}

// // // // // // // // // //

// Path returns [root, ..., target] from the local spanning tree.
// It walks parent links from the target up to the root instead of materialising
// the whole tree, so repeated Trace polling stays cheap on large networks.
func (o *Obj) Path(key ed25519.PublicKey) ([]*NodeObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	return spanningTreePath(o.source.GetTree(), key)
}

// Hops returns the port-level route to the key. Requires a prior Lookup().
func (o *Obj) Hops(key ed25519.PublicKey) ([]HopObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	paths := o.source.GetPaths()
	target := toKeyArray(key)
	for _, p := range paths {
		if toKeyArray(p.Key) == target {
			return resolveHops(p, o.source.GetPeers()), nil
		}
	}
	return nil, ErrNoActivePath
}

// Lookup initiates a path search. Results appear in Hops() after some time.
func (o *Obj) Lookup(key ed25519.PublicKey) {
	o.source.SendLookup(key)
}

// // // // // // // // // //

func (o *Obj) Self() yggcore.SelfInfo                { return o.source.GetSelf() }
func (o *Obj) Address() net.IP                       { return o.source.Address() }
func (o *Obj) Subnet() net.IPNet                     { return o.source.Subnet() }
func (o *Obj) Peers() []yggcore.PeerInfo             { return o.source.GetPeers() }
func (o *Obj) Sessions() []yggcore.SessionInfo       { return o.source.GetSessions() }
func (o *Obj) SpanningTree() []yggcore.TreeEntryInfo { return o.source.GetTree() }
func (o *Obj) Paths() []yggcore.PathEntryInfo        { return o.source.GetPaths() }
