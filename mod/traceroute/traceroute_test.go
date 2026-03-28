package traceroute

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type noopLoggerObj struct{}

var _ yggcore.Logger = noopLoggerObj{}

func (noopLoggerObj) Printf(string, ...interface{}) {}
func (noopLoggerObj) Println(...interface{})        {}
func (noopLoggerObj) Infof(string, ...interface{})  {}
func (noopLoggerObj) Infoln(...interface{})         {}
func (noopLoggerObj) Warnf(string, ...interface{})  {}
func (noopLoggerObj) Warnln(...interface{})         {}
func (noopLoggerObj) Errorf(string, ...interface{}) {}
func (noopLoggerObj) Errorln(...interface{})        {}
func (noopLoggerObj) Debugf(string, ...interface{}) {}
func (noopLoggerObj) Debugln(...interface{})        {}
func (noopLoggerObj) Traceln(...interface{})        {}

// //

func genKey(t testing.TB) ed25519.PublicKey {
	t.Helper()
	pk, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func genKeyN(t testing.TB, n int) []ed25519.PublicKey {
	t.Helper()
	keys := make([]ed25519.PublicKey, n)
	for i := range keys {
		keys[i] = genKey(t)
	}
	return keys
}

// buildTestTree creates:
//
//	root(0) -> c1(1) -> gc1(3), gc2(4)
//	        -> c2(2)
func buildTestTree(t testing.TB) (*NodeObj, []ed25519.PublicKey) {
	t.Helper()
	keys := genKeyN(t, 5)
	root := &NodeObj{Key: keys[0], Depth: 0}
	c1 := &NodeObj{Key: keys[1], Parent: keys[0], Depth: 1}
	c2 := &NodeObj{Key: keys[2], Parent: keys[0], Depth: 1}
	gc1 := &NodeObj{Key: keys[3], Parent: keys[1], Depth: 2}
	gc2 := &NodeObj{Key: keys[4], Parent: keys[1], Depth: 2}
	root.Children = []*NodeObj{c1, c2}
	c1.Children = []*NodeObj{gc1, gc2}
	return root, keys
}

// // // // // // // // // //
// validateKey

func TestValidateKey_valid(t *testing.T) {
	if err := validateKey(genKey(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKey_short(t *testing.T) {
	err := validateKey(ed25519.PublicKey(make([]byte, 16)))
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected ErrInvalidKeyLength, got: %v", err)
	}
}

func TestValidateKey_nil(t *testing.T) {
	err := validateKey(nil)
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected ErrInvalidKeyLength, got: %v", err)
	}
}

// // // // // // // // // //
// NodeObj.Find

func TestFind_root(t *testing.T) {
	root, keys := buildTestTree(t)
	if found := root.Find(keys[0]); found != root {
		t.Fatal("expected root")
	}
}

func TestFind_deep(t *testing.T) {
	root, keys := buildTestTree(t)
	found := root.Find(keys[3])
	if found == nil || !found.Key.Equal(keys[3]) {
		t.Fatal("expected grandchild1")
	}
}

func TestFind_notFound(t *testing.T) {
	root, _ := buildTestTree(t)
	if root.Find(genKey(t)) != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestFind_nil(t *testing.T) {
	var n *NodeObj
	if n.Find(genKey(t)) != nil {
		t.Fatal("expected nil on nil receiver")
	}
}

// // // // // // // // // //
// NodeObj.Flatten

func TestFlatten(t *testing.T) {
	root, _ := buildTestTree(t)
	flat := root.Flatten()
	if len(flat) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(flat))
	}
	if flat[0] != root {
		t.Fatal("first element must be root")
	}
}

func TestFlatten_single(t *testing.T) {
	n := &NodeObj{Key: genKey(t)}
	flat := n.Flatten()
	if len(flat) != 1 {
		t.Fatalf("expected 1, got %d", len(flat))
	}
}

func TestFlatten_nil(t *testing.T) {
	var n *NodeObj
	if flat := n.Flatten(); flat != nil {
		t.Fatalf("expected nil, got %v", flat)
	}
}

// // // // // // // // // //
// NodeObj.PathTo

func TestPathTo_leaf(t *testing.T) {
	root, keys := buildTestTree(t)
	path := root.PathTo(keys[4])
	if path == nil {
		t.Fatal("expected path to grandchild2")
	}
	// root -> c1 -> gc2
	if len(path) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(path))
	}
	if !path[0].Key.Equal(keys[0]) {
		t.Error("first hop must be root")
	}
	if !path[2].Key.Equal(keys[4]) {
		t.Error("last hop must be target")
	}
}

func TestPathTo_root(t *testing.T) {
	root, keys := buildTestTree(t)
	path := root.PathTo(keys[0])
	if len(path) != 1 {
		t.Fatalf("expected 1 hop for root self-path, got %d", len(path))
	}
}

func TestPathTo_notFound(t *testing.T) {
	root, _ := buildTestTree(t)
	if root.PathTo(genKey(t)) != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestPathTo_nil(t *testing.T) {
	var n *NodeObj
	if n.PathTo(genKey(t)) != nil {
		t.Fatal("expected nil on nil receiver")
	}
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
// resolveHops

func TestResolveHops_allResolved(t *testing.T) {
	keys := genKeyN(t, 3)
	path := yggcore.PathEntryInfo{
		Key:  genKey(t),
		Path: []uint64{10, 20, 30},
	}
	peers := []yggcore.PeerInfo{
		{Key: keys[0], Port: 10, Up: true},
		{Key: keys[1], Port: 20, Up: true},
		{Key: keys[2], Port: 30, Up: true},
	}
	hops := resolveHops(path, peers)
	if len(hops) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(hops))
	}
	for i, h := range hops {
		if h.Key == nil {
			t.Errorf("hop %d: expected resolved key", i)
		}
		if h.Index != i {
			t.Errorf("hop %d: expected Index=%d, got %d", i, i, h.Index)
		}
	}
}

func TestResolveHops_unresolved(t *testing.T) {
	path := yggcore.PathEntryInfo{
		Key:  genKey(t),
		Path: []uint64{10, 99},
	}
	peers := []yggcore.PeerInfo{
		{Key: genKey(t), Port: 10, Up: true},
	}
	hops := resolveHops(path, peers)
	if len(hops) != 2 {
		t.Fatalf("expected 2 hops, got %d", len(hops))
	}
	if hops[0].Key == nil {
		t.Error("hop 0 should be resolved")
	}
	if hops[1].Key != nil {
		t.Error("hop 1 should be unresolved")
	}
}

func TestResolveHops_downPeerIgnored(t *testing.T) {
	path := yggcore.PathEntryInfo{Key: genKey(t), Path: []uint64{10}}
	peers := []yggcore.PeerInfo{{Key: genKey(t), Port: 10, Up: false}}
	hops := resolveHops(path, peers)
	if hops[0].Key != nil {
		t.Error("down peer should not resolve")
	}
}

func TestResolveHops_empty(t *testing.T) {
	path := yggcore.PathEntryInfo{Key: genKey(t)}
	hops := resolveHops(path, nil)
	if len(hops) != 0 {
		t.Fatalf("expected 0 hops, got %d", len(hops))
	}
}

// // // // // // // // // //
// toKeyArray

func TestToKeyArray(t *testing.T) {
	key := genKey(t)
	arr := toKeyArray(key)
	for i := range arr {
		if arr[i] != key[i] {
			t.Fatalf("mismatch at byte %d", i)
		}
	}
}

func TestToKeyArray_mapEquality(t *testing.T) {
	key := genKey(t)
	a := toKeyArray(key)
	b := toKeyArray(key)
	if a != b {
		t.Fatal("same key should produce equal arrays")
	}
}

// // // // // // // // // //
// parseRemotePeersResponse

func TestParseRemotePeersResponse_valid(t *testing.T) {
	keys := genKeyN(t, 2)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{
		Keys: []string{
			hex.EncodeToString(keys[0]),
			hex.EncodeToString(keys[1]),
		},
	})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongType(t *testing.T) {
	_, err := parseRemotePeersResponse("not a map")
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestParseRemotePeersResponse_invalidHex(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"zzzz_not_hex"}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for invalid hex, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongKeyLength(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{hex.EncodeToString(make([]byte, 10))}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, _ := parseRemotePeersResponse(resp)
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for wrong key length, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_empty(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{}
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_nonRawMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{"n": "string value"}
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

// // // // // // // // // //
// adminCaptureObj

func TestAdminCapture(t *testing.T) {
	cap := &adminCaptureObj{handlers: make(map[string]yggcore.AddHandlerFunc)}
	fn := func(json.RawMessage) (interface{}, error) { return nil, nil }
	if err := cap.AddHandler("test_fn", "description", nil, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.handlers["test_fn"] == nil {
		t.Fatal("handler not captured")
	}
	if cap.handlers["missing"] != nil {
		t.Fatal("unexpected handler for missing key")
	}
}

// // // // // // // // // //
// peerCacheObj

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
// workerPoolObj

func TestWorkerPool_basic(t *testing.T) {
	var calls atomic.Int64
	call := func(_ context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		calls.Add(1)
		return []ed25519.PublicKey{key}, 10 * time.Millisecond, nil
	}

	pool := newWorkerPool(4, call)
	defer pool.stop()

	ctx := context.Background()
	results := make(chan peerResultObj, 10)
	keys := genKeyN(t, 10)
	for _, k := range keys {
		pool.submit(ctx, k, results)
	}

	for range 10 {
		r := <-results
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if len(r.peers) != 1 {
			t.Fatalf("expected 1 peer, got %d", len(r.peers))
		}
	}
	if calls.Load() != 10 {
		t.Fatalf("expected 10 calls, got %d", calls.Load())
	}
}

func TestWorkerPool_ctxCancel(t *testing.T) {
	call := func(ctx context.Context, _ ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}

	pool := newWorkerPool(2, call)

	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan peerResultObj, 1)

	cancel()
	pool.submit(ctx, genKey(t), results)
	r := <-results
	if r.err == nil {
		t.Fatal("expected error on cancelled context")
	}

	pool.stop()
}

func TestWorkerPool_stopDrainsAll(t *testing.T) {
	var count atomic.Int64
	call := func(_ context.Context, _ ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		count.Add(1)
		return nil, 0, nil
	}

	pool := newWorkerPool(2, call)
	ctx := context.Background()
	results := make(chan peerResultObj, 5)
	for range 5 {
		pool.submit(ctx, genKey(t), results)
	}
	for range 5 {
		<-results
	}
	pool.stop()
	if count.Load() != 5 {
		t.Fatalf("expected 5 calls, got %d", count.Load())
	}
}

// // // // // // // // // //
// Errors

func TestErrors_distinct(t *testing.T) {
	errs := []error{
		ErrCoreRequired, ErrLoggerRequired, ErrRemotePeersNotCaptured,
		ErrInvalidCacheTTL, ErrMaxDepthRequired, ErrInvalidKeyLength,
		ErrKeyNotInTree, ErrNoActivePath, ErrNodeUnreachable,
		ErrRemotePeersDisabled, ErrTreeEmpty, ErrNoRoot, ErrLookupTimedOut,
	}
	seen := make(map[string]bool, len(errs))
	for _, e := range errs {
		msg := e.Error()
		if seen[msg] {
			t.Fatalf("duplicate error message: %q", msg)
		}
		seen[msg] = true
	}
}

// // // // // // // // // //
// Benchmarks

func BenchmarkFind_deep(b *testing.B) {
	// Build a linear chain: root -> n1 -> n2 -> ... -> n99
	keys := make([]ed25519.PublicKey, 100)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	root := &NodeObj{Key: keys[0]}
	cur := root
	for i := 1; i < len(keys); i++ {
		child := &NodeObj{Key: keys[i]}
		cur.Children = []*NodeObj{child}
		cur = child
	}
	target := keys[len(keys)-1]
	for b.Loop() {
		root.Find(target)
	}
}

func BenchmarkFlatten(b *testing.B) {
	keys := make([]ed25519.PublicKey, 1000)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	// Binary-ish tree
	nodes := make([]*NodeObj, len(keys))
	for i, k := range keys {
		nodes[i] = &NodeObj{Key: k}
	}
	for i := range nodes {
		left := 2*i + 1
		right := 2*i + 2
		if left < len(nodes) {
			nodes[i].Children = append(nodes[i].Children, nodes[left])
		}
		if right < len(nodes) {
			nodes[i].Children = append(nodes[i].Children, nodes[right])
		}
	}
	root := nodes[0]
	for b.Loop() {
		root.Flatten()
	}
}

func BenchmarkPathTo(b *testing.B) {
	keys := make([]ed25519.PublicKey, 100)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	root := &NodeObj{Key: keys[0]}
	cur := root
	for i := 1; i < len(keys); i++ {
		child := &NodeObj{Key: keys[i]}
		cur.Children = []*NodeObj{child}
		cur = child
	}
	target := keys[len(keys)-1]
	for b.Loop() {
		root.PathTo(target)
	}
}

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
		buildTree(entries, log)
	}
}

func BenchmarkToKeyArray(b *testing.B) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	for b.Loop() {
		toKeyArray(pk)
	}
}

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

func BenchmarkParseRemotePeersResponse(b *testing.B) {
	keys := make([]string, 20)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = hex.EncodeToString(pk)
	}
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: keys})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	for b.Loop() {
		parseRemotePeersResponse(resp)
	}
}

func BenchmarkWorkerPool(b *testing.B) {
	call := func(_ context.Context, k ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		return []ed25519.PublicKey{k}, 0, nil
	}
	pool := newWorkerPool(8, call)
	defer pool.stop()

	ctx := context.Background()
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	results := make(chan peerResultObj, b.N+1)

	for b.Loop() {
		pool.submit(ctx, pk, results)
		<-results
	}
}

func BenchmarkResolveHops(b *testing.B) {
	peers := make([]yggcore.PeerInfo, 50)
	for i := range peers {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		peers[i] = yggcore.PeerInfo{Key: pk, Port: uint64(i + 1), Up: true}
	}
	ports := make([]uint64, 30)
	for i := range ports {
		ports[i] = uint64(i + 1)
	}
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	path := yggcore.PathEntryInfo{Key: pk, Path: ports}

	for b.Loop() {
		resolveHops(path, peers)
	}
}
