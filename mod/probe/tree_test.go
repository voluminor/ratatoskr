package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type treeSourceObj struct {
	self  ed25519.PublicKey
	peers []yggcore.PeerInfo
}

func (s *treeSourceObj) SetAdmin(yggcore.AddHandler) error {
	return nil
}

func (s *treeSourceObj) SendLookup(ed25519.PublicKey) {}

func (s *treeSourceObj) Address() net.IP {
	return nil
}

func (s *treeSourceObj) Subnet() net.IPNet {
	return net.IPNet{}
}

func (s *treeSourceObj) PublicKey() ed25519.PublicKey {
	return s.self
}

func (s *treeSourceObj) GetSelf() yggcore.SelfInfo {
	return yggcore.SelfInfo{}
}

func (s *treeSourceObj) GetPeers() []yggcore.PeerInfo {
	return append([]yggcore.PeerInfo(nil), s.peers...)
}

func (s *treeSourceObj) GetSessions() []yggcore.SessionInfo {
	return nil
}

func (s *treeSourceObj) GetTree() []yggcore.TreeEntryInfo {
	return nil
}

func (s *treeSourceObj) GetPaths() []yggcore.PathEntryInfo {
	return nil
}

// // // // // // // // // //
// buildTree

func TestBuildTree_normal(t *testing.T) {
	keys := genKeyN(t, 4)
	entries := []yggcore.TreeEntryInfo{
		{Key: keys[0], Parent: keys[0], Sequence: 1}, // root
		{Key: keys[1], Parent: keys[0], Sequence: 2},
		{Key: keys[2], Parent: keys[0], Sequence: 3},
		{Key: keys[3], Parent: keys[1], Sequence: 4},
	}
	root, err := buildTree(entries, noopLoggerObj{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !root.Key.Equal(keys[0]) {
		t.Fatal("root key mismatch")
	}
	if len(root.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(root.Children))
	}
	if root.Depth != 0 {
		t.Fatalf("root depth must be 0, got %d", root.Depth)
	}
	flat := root.Flatten()
	if len(flat) != 4 {
		t.Fatalf("expected 4 nodes total, got %d", len(flat))
	}
}

func TestBuildTree_empty(t *testing.T) {
	_, err := buildTree(nil, noopLoggerObj{})
	if !errors.Is(err, ErrTreeEmpty) {
		t.Fatalf("expected ErrTreeEmpty, got: %v", err)
	}
}

func TestBuildTree_noRoot(t *testing.T) {
	keys := genKeyN(t, 2)
	entries := []yggcore.TreeEntryInfo{
		{Key: keys[0], Parent: keys[1]},
		{Key: keys[1], Parent: keys[0]},
	}
	_, err := buildTree(entries, noopLoggerObj{})
	if !errors.Is(err, ErrNoRoot) {
		t.Fatalf("expected ErrNoRoot, got: %v", err)
	}
}

func TestBuildTree_singleRoot(t *testing.T) {
	k := genKey(t)
	entries := []yggcore.TreeEntryInfo{{Key: k, Parent: k}}
	root, err := buildTree(entries, noopLoggerObj{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !root.Key.Equal(k) {
		t.Fatal("key mismatch")
	}
	if len(root.Children) != 0 {
		t.Fatal("single root should have no children")
	}
}

func TestTree_levelOneTruncationUsesSortedPeers(t *testing.T) {
	self := cacheTestKey(100)
	low := cacheTestKey(1)
	high := cacheTestKey(2)
	obj := &Obj{
		source: &treeSourceObj{
			self: self,
			peers: []yggcore.PeerInfo{
				{Key: high, Up: true},
				{Key: low, Up: true},
			},
		},
		logger:         noopLoggerObj{},
		maxTotalNodes:  1,
		maxConcurrency: 1,
		maxDuration:    -1,
	}
	result, err := obj.Tree(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if !result.Truncated {
		t.Fatal("expected truncated result")
	}
	if len(result.Root.Children) != 1 || !result.Root.Children[0].Key.Equal(low) {
		t.Fatalf("expected lowest key to survive truncation, got %v", result.Root.Children)
	}
}

func TestTree_backgroundContextUsesMaxDuration(t *testing.T) {
	self := cacheTestKey(100)
	peer := cacheTestKey(1)
	obj := &Obj{
		source: &treeSourceObj{
			self:  self,
			peers: []yggcore.PeerInfo{{Key: peer, Up: true}},
		},
		logger:          noopLoggerObj{},
		maxPeersPerNode: DefaultMaxPeersPerNode,
		maxTotalNodes:   4,
		maxConcurrency:  1,
		maxDuration:     10 * time.Millisecond,
		remotePeers: func(json.RawMessage) (interface{}, error) {
			time.Sleep(50 * time.Millisecond)
			return yggcore.DebugGetPeersResponse{}, nil
		},
		cache: newPeerCache(time.Minute, defaultCacheMaxEntries),
	}
	defer obj.cache.close()

	_, err := obj.Tree(context.Background(), 2, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline from MaxDuration, got %v", err)
	}
}

func TestTrace_backgroundContextUsesMaxDuration(t *testing.T) {
	obj := &Obj{
		source:           &treeSourceObj{self: cacheTestKey(100)},
		logger:           noopLoggerObj{},
		pollInterval:     time.Millisecond,
		lookupRetryEvery: time.Millisecond,
		maxDuration:      10 * time.Millisecond,
	}

	_, err := obj.Trace(context.Background(), cacheTestKey(1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline from MaxDuration, got %v", err)
	}
}

func TestBuildTree_orphans(t *testing.T) {
	keys := genKeyN(t, 3)
	entries := []yggcore.TreeEntryInfo{
		{Key: keys[0], Parent: keys[0]},   // root
		{Key: keys[1], Parent: keys[0]},   // valid child
		{Key: keys[2], Parent: genKey(t)}, // orphan: parent not in tree
	}
	root, err := buildTree(entries, noopLoggerObj{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(root.Flatten()) != 2 {
		t.Fatalf("expected 2 reachable nodes (root + child), got %d", len(root.Flatten()))
	}
}

// // // // // // // // // //
// setDepth

func TestSetDepth_normal(t *testing.T) {
	root, _ := buildTestTree(t)
	setDepth(root, 0, 100)
	for _, n := range root.Flatten() {
		if n.Depth < 0 {
			t.Fatal("negative depth")
		}
	}
	if root.Children[0].Children[0].Depth != 2 {
		t.Fatalf("expected depth 2 for grandchild, got %d", root.Children[0].Children[0].Depth)
	}
}

func TestSetDepth_cutoff(t *testing.T) {
	root, _ := buildTestTree(t)
	setDepth(root, 0, 1)
	// maxDepth=1 means children at depth 1 get their children cut
	for _, c := range root.Children {
		if len(c.Children) != 0 {
			t.Fatal("children beyond maxDepth should be cut")
		}
	}
}

// // // // // // // // // //

func TestEffectiveConcurrency(t *testing.T) {
	obj := &Obj{maxConcurrency: 64}
	if got := obj.effectiveConcurrency(0); got != defaultPoolSize {
		t.Fatalf("expected default pool size %d, got %d", defaultPoolSize, got)
	}
	if got := obj.effectiveConcurrency(128); got != 64 {
		t.Fatalf("expected capped concurrency 64, got %d", got)
	}

	obj.maxConcurrency = 4
	if got := obj.effectiveConcurrency(0); got != 4 {
		t.Fatalf("expected capped default concurrency 4, got %d", got)
	}
}

// // // // // // // // // //
// scanLevel

func TestScanLevel_totalLimit(t *testing.T) {
	keys := genKeyN(t, 4)
	parent := &NodeObj{Key: keys[0], Depth: 1}
	call := func(_ context.Context, _ ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		return keys[1:], time.Millisecond, nil
	}

	obj := &Obj{
		logger:          noopLoggerObj{},
		maxPeersPerNode: DefaultMaxPeersPerNode,
		maxTotalNodes:   2,
	}
	visited := map[[ed25519.PublicKeySize]byte]struct{}{
		toKeyArray(parent.Key): {},
	}

	next, truncated, err := obj.scanLevel(context.Background(), call, []*NodeObj{parent}, visited, 2, 2, 0)
	if err != nil {
		t.Fatalf("scanLevel: %v", err)
	}
	if !truncated {
		t.Fatal("expected traversal to be truncated")
	}
	if len(next) != 2 {
		t.Fatalf("expected 2 nodes after total limit, got %d", len(next))
	}
	if len(parent.Children) != 2 {
		t.Fatalf("expected 2 child nodes, got %d", len(parent.Children))
	}
	if len(visited) != 3 {
		t.Fatalf("expected parent plus 2 visited children, got %d", len(visited))
	}
}

func TestScanLevel_cancelReturnsPartialLevel(t *testing.T) {
	keys := genKeyN(t, 4)
	parentA := &NodeObj{Key: keys[0], Depth: 1}
	parentB := &NodeObj{Key: keys[1], Depth: 1}
	ctx, cancel := context.WithCancel(context.Background())
	blocked := make(chan struct{})
	call := func(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		if key.Equal(parentA.Key) {
			return []ed25519.PublicKey{keys[2]}, time.Millisecond, nil
		}
		close(blocked)
		cancel()
		return nil, 0, ctx.Err()
	}

	obj := &Obj{
		logger:          noopLoggerObj{},
		maxPeersPerNode: DefaultMaxPeersPerNode,
		maxTotalNodes:   4,
	}
	visited := map[[ed25519.PublicKeySize]byte]struct{}{
		toKeyArray(parentA.Key): {},
		toKeyArray(parentB.Key): {},
	}

	next, truncated, err := obj.scanLevel(ctx, call, []*NodeObj{parentA, parentB}, visited, 2, 4, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if truncated {
		t.Fatal("cancel should not mark traversal as truncated")
	}
	if len(next) != 1 || !next[0].Key.Equal(keys[2]) {
		t.Fatalf("expected one partial child, got %v", next)
	}
	if len(parentA.Children) != 1 {
		t.Fatalf("expected partial child attached to first parent, got %d", len(parentA.Children))
	}
	select {
	case <-blocked:
	default:
		t.Fatal("second worker did not reach blocking point")
	}
}

func TestScanLevel_appliesOutOfOrderResultsBeforeCancel(t *testing.T) {
	keys := genKeyN(t, 4)
	parentA := &NodeObj{Key: keys[0], Depth: 1}
	parentB := &NodeObj{Key: keys[1], Depth: 1}
	ctx, cancel := context.WithCancel(context.Background())
	parentABlocked := make(chan struct{})
	releaseParentA := make(chan struct{})
	parentBDone := make(chan struct{})
	call := func(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		if key.Equal(parentA.Key) {
			close(parentABlocked)
			<-releaseParentA
			return nil, 0, ctx.Err()
		}
		close(parentBDone)
		cancel()
		return []ed25519.PublicKey{keys[2]}, time.Millisecond, nil
	}
	defer func() {
		select {
		case <-releaseParentA:
		default:
			close(releaseParentA)
		}
	}()

	obj := &Obj{
		logger:          noopLoggerObj{},
		maxPeersPerNode: DefaultMaxPeersPerNode,
		maxTotalNodes:   4,
	}
	visited := map[[ed25519.PublicKeySize]byte]struct{}{
		toKeyArray(parentA.Key): {},
		toKeyArray(parentB.Key): {},
	}

	type scanResultObj struct {
		next      []*NodeObj
		truncated bool
		err       error
	}
	resultCh := make(chan scanResultObj, 1)
	go func() {
		next, truncated, err := obj.scanLevel(ctx, call, []*NodeObj{parentA, parentB}, visited, 2, 4, 0)
		resultCh <- scanResultObj{next: next, truncated: truncated, err: err}
	}()

	select {
	case <-parentABlocked:
	case <-time.After(time.Second):
		t.Fatal("first worker did not block")
	}
	select {
	case <-parentBDone:
	case <-time.After(time.Second):
		t.Fatal("second worker did not finish")
	}
	close(releaseParentA)

	var result scanResultObj
	select {
	case result = <-resultCh:
	case <-time.After(time.Second):
		t.Fatal("scanLevel did not return after cancellation")
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", result.err)
	}
	if result.truncated {
		t.Fatal("cancel should not mark traversal as truncated")
	}
	if len(result.next) != 1 || !result.next[0].Key.Equal(keys[2]) {
		t.Fatalf("expected out-of-order child to be retained, got %v", result.next)
	}
	if len(parentB.Children) != 1 {
		t.Fatalf("expected child attached to second parent, got %d", len(parentB.Children))
	}
}

func TestScanLevel_usesWorkerPool(t *testing.T) {
	keys := genKeyN(t, 8)
	nodes := make([]*NodeObj, len(keys))
	visited := make(map[[ed25519.PublicKeySize]byte]struct{}, len(keys))
	for i, key := range keys {
		nodes[i] = &NodeObj{Key: key, Depth: 1}
		visited[toKeyArray(key)] = struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan ed25519.PublicKey, len(nodes))
	release := make(chan struct{})
	call := func(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		entered <- key
		select {
		case <-release:
			return nil, 0, nil
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	obj := &Obj{
		logger:          noopLoggerObj{},
		maxPeersPerNode: DefaultMaxPeersPerNode,
		maxTotalNodes:   len(keys),
		maxConcurrency:  2,
	}

	type scanResultObj struct {
		truncated bool
		err       error
	}
	resultCh := make(chan scanResultObj, 1)
	go func() {
		_, truncated, err := obj.scanLevel(ctx, call, nodes, visited, 2, len(keys), 0)
		resultCh <- scanResultObj{truncated: truncated, err: err}
	}()

	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	select {
	case <-entered:
		t.Fatal("scanLevel started more workers than the configured concurrency")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("scanLevel: %v", result.err)
		}
		if result.truncated {
			t.Fatal("worker-pool scan should not truncate")
		}
	case <-time.After(time.Second):
		t.Fatal("scanLevel did not return")
	}
}

func TestTreeChan_closesProgressOnError(t *testing.T) {
	obj := &Obj{}
	ch := make(chan TreeProgressObj)
	_, err := obj.TreeChan(context.Background(), 0, 1, ch)
	if !errors.Is(err, ErrMaxDepthRequired) {
		t.Fatalf("expected ErrMaxDepthRequired, got %v", err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("TreeChan should close progress channel before returning")
	}
}

// // // // // // // // // //

func BenchmarkBuildTree(b *testing.B) {
	n := 500
	keys := make([]ed25519.PublicKey, n)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	entries := make([]yggcore.TreeEntryInfo, n)
	entries[0] = yggcore.TreeEntryInfo{Key: keys[0], Parent: keys[0]}
	for i := 1; i < n; i++ {
		entries[i] = yggcore.TreeEntryInfo{Key: keys[i], Parent: keys[(i-1)/2]}
	}
	log := noopLoggerObj{}
	for b.Loop() {
		if _, err := buildTree(entries, log); err != nil {
			b.Fatalf("buildTree: %v", err)
		}
	}
}
