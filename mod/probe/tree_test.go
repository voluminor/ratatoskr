package probe

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type treeSourceObj struct {
	self  ed25519.PublicKey
	peers []yggcore.PeerInfo
	tree  []yggcore.TreeEntryInfo
	paths []yggcore.PathEntryInfo
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
	return append([]yggcore.TreeEntryInfo(nil), s.tree...)
}

func (s *treeSourceObj) GetPaths() []yggcore.PathEntryInfo {
	return append([]yggcore.PathEntryInfo(nil), s.paths...)
}

// // // // // // // // // //

func newTreeTestObj() *Obj {
	return &Obj{
		logger:        noopLoggerObj{},
		tasks:         common.NewTaskGroup(context.Background()),
		remoteSem:     make(chan struct{}, DefaultMaxConcurrency),
		remoteFlights: make(map[[ed25519.PublicKeySize]byte]*remoteFlightObj),
	}
}

func TestTree_levelOneTruncationUsesSortedPeers(t *testing.T) {
	self := cacheTestKey(100)
	low := cacheTestKey(1)
	high := cacheTestKey(2)
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{
		self: self,
		peers: []yggcore.PeerInfo{
			{Key: high, Up: true},
			{Key: low, Up: true},
		},
	}
	obj.maxTotalNodes = 1
	obj.maxDuration = -1
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
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{
		self:  self,
		peers: []yggcore.PeerInfo{{Key: peer, Up: true}},
	}
	obj.maxTotalNodes = 4
	obj.maxDuration = 10 * time.Millisecond
	obj.remotePeers = func(json.RawMessage) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return yggcore.DebugGetPeersResponse{}, nil
	}

	_, err := obj.Tree(context.Background(), 2, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline from MaxDuration, got %v", err)
	}
}

func TestTreeReturnsPartialResultOnProbeOverload(t *testing.T) {
	self := cacheTestKey(1000)
	peer := cacheTestKey(1001)
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{self: self, peers: []yggcore.PeerInfo{{Key: peer, Up: true}}}
	obj.maxTotalNodes = 4
	obj.maxDuration = -1
	obj.remotePeers = func(json.RawMessage) (interface{}, error) {
		return yggcore.DebugGetPeersResponse{}, nil
	}
	obj.remoteSem = make(chan struct{}, 1)
	obj.remoteFlights[toKeyArray(cacheTestKey(999))] = &remoteFlightObj{done: make(chan struct{})}

	result, err := obj.Tree(context.Background(), 2, 1)
	if !errors.Is(err, ErrProbeBusy) {
		t.Fatalf("Tree error = %v, want ErrProbeBusy", err)
	}
	if result == nil || result.Total != 1 || len(result.Root.Children) != 1 {
		t.Fatalf("partial result = %+v, want direct peer", result)
	}
	if result.Truncated {
		t.Fatal("overload must not set Truncated")
	}
	if result.Root.Children[0].Unreachable {
		t.Fatal("overload must not mark the unqueried peer unreachable")
	}
}

func TestTreeChanSignalsDoneWithPartialOverloadResult(t *testing.T) {
	self := cacheTestKey(2000)
	peer := cacheTestKey(2001)
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{self: self, peers: []yggcore.PeerInfo{{Key: peer, Up: true}}}
	obj.maxTotalNodes = 4
	obj.maxDuration = -1
	obj.remotePeers = func(json.RawMessage) (interface{}, error) {
		return yggcore.DebugGetPeersResponse{}, nil
	}
	obj.remoteSem = make(chan struct{}, 1)
	obj.remoteFlights[toKeyArray(cacheTestKey(1999))] = &remoteFlightObj{done: make(chan struct{})}
	progress := make(chan TreeProgressObj, 4)

	result, err := obj.TreeChan(context.Background(), 2, 1, progress)
	if !errors.Is(err, ErrProbeBusy) || result == nil || result.Total != 1 {
		t.Fatalf("TreeChan result = %+v, error = %v", result, err)
	}
	foundDone := false
	for update := range progress {
		foundDone = foundDone || update.Done
	}
	if !foundDone {
		t.Fatal("TreeChan did not signal Done for the partial result")
	}
}

func TestEnrichPath_boundsLocalFanout(t *testing.T) {
	const extraNodes = 16

	path := make([]*NodeObj, DefaultMaxConcurrency+extraNodes+1)
	for i := range path {
		path[i] = &NodeObj{Key: cacheTestKey(i + 1)}
	}
	var active atomic.Int64
	var maxActive atomic.Int64
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{}
	obj.remoteSem = make(chan struct{}, len(path))
	obj.remotePeers = func(json.RawMessage) (interface{}, error) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		time.Sleep(5 * time.Millisecond)
		return yggcore.DebugGetPeersResponse{}, nil
	}

	if err := obj.enrichPath(context.Background(), path); err != nil {
		t.Fatalf("enrichPath: %v", err)
	}
	if got := maxActive.Load(); got > DefaultMaxConcurrency {
		t.Fatalf("enrichPath started %d remote calls, want at most %d", got, DefaultMaxConcurrency)
	}
	for i, n := range path[1:] {
		if n.RTT <= 0 {
			t.Fatalf("path[%d] RTT was not enriched", i+1)
		}
	}
}

func TestTrace_backgroundContextUsesMaxDuration(t *testing.T) {
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{self: cacheTestKey(100)}
	obj.pollInterval = time.Millisecond
	obj.lookupRetryEvery = time.Millisecond
	obj.maxDuration = 10 * time.Millisecond

	_, err := obj.Trace(context.Background(), cacheTestKey(1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline from MaxDuration, got %v", err)
	}
}

func TestTraceReturnsPartialResultOnRTTOverload(t *testing.T) {
	self := cacheTestKey(3000)
	target := cacheTestKey(3001)
	obj := newTreeTestObj()
	obj.source = &treeSourceObj{
		self: self,
		tree: []yggcore.TreeEntryInfo{
			{Key: self, Parent: self},
			{Key: target, Parent: self},
		},
		paths: []yggcore.PathEntryInfo{{Key: target, Path: []uint64{}}},
	}
	obj.maxDuration = -1
	obj.remotePeers = func(json.RawMessage) (interface{}, error) {
		return yggcore.DebugGetPeersResponse{}, nil
	}
	obj.remoteSem = make(chan struct{}, 1)
	obj.remoteFlights[toKeyArray(cacheTestKey(2999))] = &remoteFlightObj{done: make(chan struct{})}

	result, err := obj.Trace(context.Background(), target)
	if !errors.Is(err, ErrProbeBusy) {
		t.Fatalf("Trace error = %v, want ErrProbeBusy", err)
	}
	if result == nil || len(result.TreePath) != 2 || result.TreePath[1].RTT != 0 {
		t.Fatalf("Trace partial result = %+v", result)
	}
}

// // // // // // // // // //

func TestEffectiveConcurrency(t *testing.T) {
	obj := &Obj{}
	if got := obj.effectiveConcurrency(0); got != defaultPoolSize {
		t.Fatalf("expected default pool size %d, got %d", defaultPoolSize, got)
	}
	if got := obj.effectiveConcurrency(8); got != 8 {
		t.Fatalf("expected passthrough 8, got %d", got)
	}
	if got := obj.effectiveConcurrency(DefaultMaxConcurrency + 100); got != DefaultMaxConcurrency {
		t.Fatalf("expected clamp to %d, got %d", DefaultMaxConcurrency, got)
	}
}

// // // // // // // // // //

func TestScanLevel_totalLimit(t *testing.T) {
	keys := genKeyN(t, 4)
	parent := &NodeObj{Key: keys[0], Depth: 1}
	call := func(_ context.Context, _ ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		return keys[1:], time.Millisecond, nil
	}

	obj := &Obj{
		logger:        noopLoggerObj{},
		maxTotalNodes: 2,
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
		logger:        noopLoggerObj{},
		maxTotalNodes: 4,
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

func TestScanLevelOverloadKeepsOtherResults(t *testing.T) {
	keys := genKeyN(t, 3)
	parentA := &NodeObj{Key: keys[0], Depth: 1}
	parentB := &NodeObj{Key: keys[1], Depth: 1}
	call := func(_ context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		if key.Equal(parentA.Key) {
			return []ed25519.PublicKey{keys[2]}, time.Millisecond, nil
		}
		return nil, 0, ErrProbeBusy
	}
	visited := map[[ed25519.PublicKeySize]byte]struct{}{
		toKeyArray(parentA.Key): {},
		toKeyArray(parentB.Key): {},
	}
	obj := &Obj{logger: noopLoggerObj{}, maxTotalNodes: 4}

	next, truncated, err := obj.scanLevel(context.Background(), call, []*NodeObj{parentA, parentB}, visited, 2, 4, 2)
	if !errors.Is(err, ErrProbeBusy) {
		t.Fatalf("scanLevel error = %v, want ErrProbeBusy", err)
	}
	if truncated {
		t.Fatal("overload must not truncate the level")
	}
	if len(next) != 1 || !next[0].Key.Equal(keys[2]) {
		t.Fatalf("partial next level = %v", next)
	}
	if parentB.Unreachable {
		t.Fatal("busy parent was marked unreachable")
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
		logger:        noopLoggerObj{},
		maxTotalNodes: 4,
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
		logger:        noopLoggerObj{},
		maxTotalNodes: len(keys),
	}

	type scanResultObj struct {
		truncated bool
		err       error
	}
	resultCh := make(chan scanResultObj, 1)
	go func() {
		_, truncated, err := obj.scanLevel(ctx, call, nodes, visited, 2, len(keys), 2)
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
	obj := newTreeTestObj()
	ch := make(chan TreeProgressObj)
	_, err := obj.TreeChan(context.Background(), 0, 1, ch)
	if !errors.Is(err, ErrMaxDepthRequired) {
		t.Fatalf("expected ErrMaxDepthRequired, got %v", err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("TreeChan should close progress channel before returning")
	}
}
