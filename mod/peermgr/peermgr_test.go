package peermgr

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

func fastCfg(peers []string) ConfigObj {
	return ConfigObj{
		Peers:        peers,
		ProbeTimeout: 10 * time.Millisecond,
		Logger:       noopLogObj{},
	}
}

func newTestObj(node NodeInterface, cfg ConfigObj) (*Obj, error) {
	cfg.Node = node
	return newObj(cfg)
}

func waitAddedPeers(t *testing.T, node *mockNodeObj, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		node.mu.Lock()
		got := len(node.added)
		node.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d added peers", want)
}

func hasCandidate(candidates []peerEntryObj, uri string) bool {
	for _, candidate := range candidates {
		if candidate.URI == uri {
			return true
		}
	}
	return false
}

type blockingAddNodeObj struct {
	mockNodeObj
	entered chan struct{}
	release chan struct{}
}

func (n *blockingAddNodeObj) AddPeer(uri string) error {
	select {
	case n.entered <- struct{}{}:
	default:
	}
	<-n.release
	return n.mockNodeObj.AddPeer(uri)
}

type recordingFailNodeObj struct {
	mockNodeObj
	err error
}

func (n *recordingFailNodeObj) AddPeer(uri string) error {
	_ = n.mockNodeObj.AddPeer(uri)
	return n.err
}

type staleSnapshotNodeObj struct {
	mockNodeObj
	uri      string
	getCalls int
}

func (n *staleSnapshotNodeObj) GetPeers() []yggcore.PeerInfo {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.getCalls++
	if n.getCalls == 1 {
		return nil
	}
	out := make([]yggcore.PeerInfo, len(n.peers))
	copy(out, n.peers)
	return out
}

func (n *staleSnapshotNodeObj) AddPeer(uri string) error {
	n.mu.Lock()
	n.addAttempts++
	n.added = append(n.added, uri)
	n.peers = []yggcore.PeerInfo{makePeerInfo(n.uri, true, time.Millisecond)}
	n.mu.Unlock()
	return errors.New("peer appeared after snapshot")
}

// //

func TestNew_valid(t *testing.T) {
	mgr, err := newTestObj(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err != nil || mgr == nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_nilNode(t *testing.T) {
	_, err := newTestObj(nil, fastCfg([]string{"tls://h:1"}))
	if !errors.Is(err, ErrNodeRequired) {
		t.Fatalf("expected ErrNodeRequired, got %v", err)
	}
}

func TestNew_noLogger(t *testing.T) {
	// A nil logger is accepted and normalized to a discard logger.
	mgr, err := newTestObj(&mockNodeObj{}, ConfigObj{
		Peers:  []string{"tls://h:1"},
		Logger: nil,
	})
	if err != nil {
		t.Fatalf("nil logger should be accepted: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected a manager with a nil logger")
	}
}

func TestNew_noPeers(t *testing.T) {
	_, err := newTestObj(&mockNodeObj{}, ConfigObj{Logger: noopLogObj{}})
	if err == nil {
		t.Fatal("expected error: no valid peers")
	}
}

func TestNew_allInvalidPeers(t *testing.T) {
	_, err := newTestObj(&mockNodeObj{}, ConfigObj{
		Peers:  []string{"tcp://%zz", "://nohost"},
		Logger: noopLogObj{},
	})
	if err == nil {
		t.Fatal("expected error: all peers invalid")
	}
}

func TestNew_defaultMaxPerProto(t *testing.T) {
	mgr, _ := newTestObj(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if mgr.cfg.MaxPerProto != 1 {
		t.Errorf("expected MaxPerProto=1, got %d", mgr.cfg.MaxPerProto)
	}
}

func TestNew_rejectsInvalidMaxPerProto(t *testing.T) {
	_, err := newTestObj(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://h:1"},
		MaxPerProto: -2,
		Logger:      noopLogObj{},
	})
	if !errors.Is(err, ErrInvalidMaxPerProto) {
		t.Fatalf("expected ErrInvalidMaxPerProto, got %v", err)
	}
}

func TestNewRejectsNegativeDefaultedValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  ConfigObj
		want error
	}{
		{name: "probe timeout", cfg: ConfigObj{ProbeTimeout: -1}, want: ErrInvalidProbeTimeout},
		{name: "batch size", cfg: ConfigObj{BatchSize: -1}, want: ErrInvalidBatchSize},
		{name: "confirmations", cfg: ConfigObj{MinPeersConfirmations: -1}, want: ErrInvalidMinPeersConfirmations},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.cfg.Peers = []string{"tls://h:1"}
			test.cfg.Logger = noopLogObj{}
			_, err := newTestObj(&mockNodeObj{}, test.cfg)
			if err == nil {
				t.Fatalf("New accepted invalid config: %+v", test.cfg)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("New error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNew_defaultProbeTimeout(t *testing.T) {
	mgr, _ := newTestObj(&mockNodeObj{}, ConfigObj{
		Peers:        []string{"tls://h:1"},
		Logger:       noopLogObj{},
		ProbeTimeout: 0,
	})
	if mgr.cfg.ProbeTimeout != defaultProbeTimeout {
		t.Errorf("expected defaultProbeTimeout, got %v", mgr.cfg.ProbeTimeout)
	}
}

func TestNew_defaultHealthInterval(t *testing.T) {
	mgr, _ := newTestObj(&mockNodeObj{}, ConfigObj{Peers: []string{"tls://h:1"}, Logger: noopLogObj{}})
	if got := mgr.cfg.HealthInterval; got != defaultHealthInterval {
		t.Fatalf("health interval = %s, want %s", got, defaultHealthInterval)
	}
	mgr, _ = newTestObj(&mockNodeObj{}, ConfigObj{Peers: []string{"tls://h:1"}, Logger: noopLogObj{}, HealthInterval: -1})
	if got := mgr.cfg.HealthInterval; got != -1 {
		t.Fatalf("disabled health interval = %s, want -1", got)
	}
}

func TestNewRejectsMinPeersAboveSelectableCapacity(t *testing.T) {
	_, err := newTestObj(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://a:1", "tls://b:1", "quic://c:1"},
		MaxPerProto: 1,
		MinPeers:    2,
		Logger:      noopLogObj{},
	})
	if !errors.Is(err, ErrMinPeersTooHigh) {
		t.Fatalf("New error = %v, want ErrMinPeersTooHigh", err)
	}
}

func TestNew_defaultReprobeInterval(t *testing.T) {
	mgr, _ := newTestObj(&mockNodeObj{}, ConfigObj{Peers: []string{"tls://h:1"}, Logger: noopLogObj{}})
	if got := mgr.cfg.ReprobeInterval; got != defaultReprobeInterval {
		t.Fatalf("reprobe interval = %s, want %s", got, defaultReprobeInterval)
	}
	mgr, _ = newTestObj(&mockNodeObj{}, ConfigObj{Peers: []string{"tls://h:1"}, Logger: noopLogObj{}, ReprobeInterval: -1})
	if got := mgr.cfg.ReprobeInterval; got != -1 {
		t.Fatalf("disabled reprobe holdoff = %s, want -1", got)
	}
}

func TestEffectiveBatchSize_usesBoundedDefault(t *testing.T) {
	if got := effectiveBatchSize(0, defaultBatchSize+10); got != defaultBatchSize {
		t.Fatalf("default batch = %d, want %d", got, defaultBatchSize)
	}
	if got := effectiveBatchSize(1, defaultBatchSize+10); got != defaultBatchSize {
		t.Fatalf("legacy batch = %d, want %d", got, defaultBatchSize)
	}
	if got := effectiveBatchSize(8, defaultBatchSize+10); got != 8 {
		t.Fatalf("configured batch = %d, want 8", got)
	}
	if got := effectiveBatchSize(maxBatchSize+100, maxBatchSize+200); got != maxBatchSize {
		t.Fatalf("capped batch = %d, want %d", got, maxBatchSize)
	}
	if got := effectiveBatchSize(0, 3); got != 3 {
		t.Fatalf("small total batch = %d, want 3", got)
	}
}

func TestNew_partiallyInvalidPeers(t *testing.T) {
	// Some valid, some not — should succeed with valid only
	mgr, err := newTestObj(&mockNodeObj{}, fastCfg([]string{"tcp://%zz", "tls://good:1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mgr.peers) != 1 || mgr.peers[0].Scheme != "tls" {
		t.Errorf("unexpected peers: %v", mgr.peers)
	}
}

// //

func TestNewStartsAndCloseIsTerminal(t *testing.T) {
	node := &blockingAddNodeObj{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	cfg := fastCfg([]string{"tls://a:1", "tls://b:1", "tls://c:1"})
	cfg.Node = node
	mgr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	select {
	case <-node.entered:
	case <-time.After(time.Second):
		close(node.release)
		t.Fatal("manager did not enter AddPeer")
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- mgr.Close()
	}()
	select {
	case <-mgr.tasks.Context().Done():
	case <-time.After(time.Second):
		close(node.release)
		t.Fatal("Close did not cancel manager tasks")
	}
	if err = mgr.Optimize(); !errors.Is(err, ErrClosed) {
		close(node.release)
		t.Fatalf("Optimize during Close = %v, want ErrClosed", err)
	}
	close(node.release)
	select {
	case err = <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after blocked AddPeer was released")
	}
	node.mu.Lock()
	addAttempts := node.addAttempts
	node.mu.Unlock()
	if addAttempts != 1 {
		t.Fatalf("AddPeer calls after cancellation = %d, want 1", addAttempts)
	}
}

func TestCloseIdempotentAfterSuccessfulRemoval(t *testing.T) {
	const uri = "tls://h:1"
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, fastCfg([]string{uri}))
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})
	if err = mgr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err = mgr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	node.mu.Lock()
	removes := len(node.removed)
	node.mu.Unlock()
	if removes != 1 {
		t.Fatalf("RemovePeer calls = %d, want 1", removes)
	}
}

func TestConcurrentCloseRemovesPeerOnce(t *testing.T) {
	const uri = "tls://h:1"
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, fastCfg([]string{uri}))
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})

	const callers = 16
	done := make(chan error, callers)
	for range callers {
		go func() { done <- mgr.Close() }()
	}
	for range callers {
		if err = <-done; err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	node.mu.Lock()
	removes := len(node.removed)
	node.mu.Unlock()
	if removes != 1 {
		t.Fatalf("concurrent RemovePeer calls = %d, want 1", removes)
	}
}

func TestRefreshInterval_reoptimizesWhileRunning(t *testing.T) {
	// AddPeer always fails, so the candidate never becomes active and never earns
	// backoff; every refresh re-probes it, letting us count reoptimizations by adds.
	node := &recordingFailNodeObj{err: errors.New("temporary failure")}
	mgr, err := New(ConfigObj{
		Node:            node,
		Peers:           []string{"tls://h:1"},
		ProbeTimeout:    10 * time.Millisecond,
		MaxPerProto:     1,
		RefreshInterval: 10 * time.Millisecond,
		Logger:          noopLogObj{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	waitAddedPeers(t, &node.mockNodeObj, 1)
	waitAddedPeers(t, &node.mockNodeObj, 2)
}

func TestOptimizeAfterClose(t *testing.T) {
	mgr, _ := newTestObj(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Optimize(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Optimize after Close = %v, want ErrClosed", err)
	}
}

func TestActiveInitiallyEmpty(t *testing.T) {
	mgr, _ := newTestObj(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if act := mgr.Active(); len(act) != 0 {
		t.Errorf("expected empty active list, got %v", act)
	}
}

func TestActive_returnsCopy(t *testing.T) {
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{makePeerInfo("tls://a:1", true, 5*time.Millisecond)},
	}
	mgr, _ := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	mgr.setActive([]string{"tls://a:1"})
	a := mgr.Active()
	b := mgr.Active()
	if len(a) == 0 {
		t.Fatal("expected a non-empty active list to compare copies")
	}
	if &a[0] == &b[0] {
		t.Error("Active() should return independent copies")
	}
}

// //

func TestActiveMode_noReachable_activeEmpty(t *testing.T) {
	node := &mockNodeObj{}
	mgr, _ := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if act := mgr.Active(); len(act) != 0 {
		t.Errorf("expected empty active (no up peers), got %v", act)
	}
}

func TestActiveMode_removesLosers(t *testing.T) {
	// Two same-protocol peers are up; with MaxPerProto=1 the slower one loses the
	// tournament and must be removed from the node.
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
			makePeerInfo("tls://b:2", true, 10*time.Millisecond),
		},
	}
	mgr, _ := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	node.mu.Lock()
	removedCount := len(node.removed)
	node.mu.Unlock()
	if removedCount < 1 {
		t.Errorf("expected the losing peer to be removed, got %d RemovePeer calls", removedCount)
	}
	active := mgr.Active()
	if len(active) != 1 || active[0] != "tls://a:1" {
		t.Fatalf("expected only the fastest peer active, got %v", active)
	}
}

func TestActiveMode_presentDownOwnedPeerIsNotAddedAgain(t *testing.T) {
	const uri = "tls://a:1"
	node := &mockNodeObj{
		peers:          []yggcore.PeerInfo{makePeerInfo(uri, false, 0)},
		removePeerFail: map[string]bool{uri: true},
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{uri},
		ProbeTimeout: time.Millisecond,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.active = []string{uri}
	if err = mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatalf("optimize: %v", err)
	}
	node.mu.Lock()
	adds := node.addAttempts
	removes := len(node.removed)
	node.mu.Unlock()
	if adds != 0 {
		t.Fatalf("AddPeer calls for present owned down peer = %d, want 0", adds)
	}
	if removes != 1 {
		t.Fatalf("RemovePeer calls = %d, want 1", removes)
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != uri {
		t.Fatalf("failed removal lost ownership: %v", got)
	}
}

func TestActiveMode_reachableLoserWaitsForReprobeTTL(t *testing.T) {
	const fast = "tls://a:1"
	const slow = "tls://b:2"
	node := &mockNodeObj{peers: []yggcore.PeerInfo{
		makePeerInfo(fast, true, time.Millisecond),
		makePeerInfo(slow, true, 2*time.Millisecond),
	}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:           []string{fast, slow},
		ProbeTimeout:    time.Millisecond,
		ReprobeInterval: time.Hour,
		Logger:          noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	node.mu.Lock()
	firstAdds := node.addAttempts
	node.mu.Unlock()
	if err = mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	node.mu.Lock()
	secondAdds := node.addAttempts
	node.mu.Unlock()
	if firstAdds != 2 || secondAdds != firstAdds {
		t.Fatalf("AddPeer calls first=%d second=%d, reachable loser was reprobed", firstAdds, secondAdds)
	}
	state, ok := mgr.probeState[normalizePeerURI(slow)]
	if !ok || time.Until(state.holdoffUntil) < 30*time.Minute {
		t.Fatalf("loser reprobe state = %+v, ok=%v", state, ok)
	}
}

func TestActiveMode_batchSize(t *testing.T) {
	node := &mockNodeObj{}
	peers := make([]string, 6)
	for i := range peers {
		peers[i] = "tls://h" + string(rune('a'+i)) + ":1"
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        peers,
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	// Just verify it completes without error; active may be empty
}

func TestActiveMode_waitsFullProbeTimeoutForBestPeer(t *testing.T) {
	const (
		early = "tls://early:1"
		late  = "tls://late:1"
	)
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo(early, true, 50*time.Millisecond),
		},
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{early, late},
		ProbeTimeout: 200 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		node.mu.Lock()
		node.peers = []yggcore.PeerInfo{
			makePeerInfo(early, true, 50*time.Millisecond),
			makePeerInfo(late, true, 5*time.Millisecond),
		}
		node.mu.Unlock()
	}()

	started := time.Now()
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("optimizeLocked: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 180*time.Millisecond {
		t.Fatalf("optimize returned before full ProbeTimeout: %s", elapsed)
	}
	active := mgr.Active()
	if len(active) != 1 || active[0] != late {
		t.Fatalf("expected late lower-latency peer %q, got %v", late, active)
	}
}

func TestActiveMode_respectsBackoffWhenNoPeersActive(t *testing.T) {
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2", "tls://c:3"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("first optimizeLocked: %v", err)
	}
	node.mu.Lock()
	firstAdded := len(node.added)
	node.mu.Unlock()
	if firstAdded != 3 {
		t.Fatalf("expected first full probe to add 3 peers, got %d", firstAdded)
	}
	// Push every candidate far into its backoff window so none is due again.
	for key, state := range mgr.probeState {
		state.holdoffUntil = time.Now().Add(time.Hour)
		mgr.probeState[key] = state
	}

	for i := 0; i < 6; i++ {
		if err := mgr.optimizeLocked(context.Background()); err != nil {
			t.Fatalf("backed-off optimizeLocked %d: %v", i, err)
		}
	}
	node.mu.Lock()
	secondAdded := len(node.added)
	node.mu.Unlock()
	if secondAdded != firstAdded {
		t.Fatalf("backed-off peers were probed again, adds before=%d after=%d", firstAdded, secondAdded)
	}
}

func TestActiveMode_backsOffPeerWhenAddPeerFails(t *testing.T) {
	node := &mockNodeObj{addPeerFail: map[string]bool{"tls://bad:1": true}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://bad:1"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("optimizeLocked: %v", err)
	}

	node.mu.Lock()
	attempts := node.addAttempts
	node.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("expected 1 AddPeer attempt, got %d", attempts)
	}

	// A synchronous AddPeer failure must still record backoff state, so the bad
	// URI is not a due candidate again until its backoff window elapses.
	key := normalizePeerURI("tls://bad:1")
	state, ok := mgr.probeState[key]
	if !ok || state.failures == 0 || !state.retryAfter.After(time.Now()) {
		t.Fatalf("failing AddPeer did not create backoff: state=%+v ok=%v", state, ok)
	}
	if due := mgr.probeCycleCandidates(time.Now(), false, node.GetPeers()); len(due) != 0 {
		t.Fatalf("backed-off failing peer still due for probing: %v", due)
	}
}

// //

func TestCloseRemovesActivePeers(t *testing.T) {
	// Both peers are up on distinct protocols, so both win selection and stay
	// active; Close must then remove them from the node.
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
			makePeerInfo("tcp://b:2", true, 10*time.Millisecond),
		},
	}
	mgr, err := New(ConfigObj{
		Node:         node,
		Peers:        []string{"tls://a:1", "tcp://b:2"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Let the startup optimize settle both peers into the active set before Close.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.Active()) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if act := mgr.Active(); len(act) != 2 {
		t.Fatalf("expected 2 active peers before Close, got %v", act)
	}

	node.mu.Lock()
	beforeClose := len(node.removed)
	node.mu.Unlock()

	_ = mgr.Close()

	node.mu.Lock()
	afterClose := len(node.removed)
	node.mu.Unlock()

	if afterClose-beforeClose < 2 {
		t.Errorf("Close should remove both active peers, removed %d", afterClose-beforeClose)
	}
}

func TestOptimizeActive_cancelRemovesPendingBatch(t *testing.T) {
	// A cycle cancelled mid-probe performs no per-batch rollback; instead it records
	// every peer it handed to the node as active, so Close reaps exactly those.
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2"},
		ProbeTimeout: time.Hour,
		MaxPerProto:  1,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- mgr.optimizeLocked(ctx)
	}()
	waitAddedPeers(t, node, 2)
	cancel()

	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("optimize did not return after cancellation")
	}

	active := mgr.Active()
	if len(active) != 2 {
		t.Fatalf("expected the two added peers left active for Close to reap, got %v", active)
	}
	node.mu.Lock()
	removed := len(node.removed)
	node.mu.Unlock()
	if removed != 0 {
		t.Fatalf("cancelled cycle must not remove peers itself, got %d removals", removed)
	}
}

func TestOptimizeActive_abortsOnContextDeadline(t *testing.T) {
	node := &mockNodeObj{}
	peers := make([]string, 64)
	for i := range peers {
		peers[i] = fmt.Sprintf("tls://h%d:1", i)
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        peers,
		ProbeTimeout: time.Second,
		MaxPerProto:  1,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = mgr.optimizeLocked(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected optimize deadline, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("optimize exceeded deadline, elapsed=%s", elapsed)
	}
}

func TestOptimizeActive_timeoutDoesNotRemoveAlreadyActivePeer(t *testing.T) {
	const activeURI = "tls://active:1"
	const candidateURI = "tls://candidate:1"
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo(activeURI, true, 5*time.Millisecond),
		},
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{activeURI, candidateURI},
		ProbeTimeout: time.Second,
		MaxPerProto:  1,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{activeURI})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = mgr.optimizeLocked(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected optimize deadline, got %v", err)
	}
	// A timed-out window performs no selection, so an already-active peer is never
	// handed to RemovePeer even though its probe was interrupted.
	node.mu.Lock()
	removed := append([]string(nil), node.removed...)
	node.mu.Unlock()
	for _, uri := range removed {
		if uri == activeURI {
			t.Fatalf("already active peer was removed on timeout: %v", removed)
		}
	}
}

func TestOptimizeActive_staleSnapshotRetainsOwnedPeerForClose(t *testing.T) {
	const uri = "tls://active.example:1"
	node := &staleSnapshotNodeObj{uri: uri}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{uri},
		ProbeTimeout: time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})

	if err = mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != uri {
		t.Fatalf("stale snapshot lost active ownership: %v", got)
	}
	if err = mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	node.mu.Lock()
	removed := append([]string(nil), node.removed...)
	node.mu.Unlock()
	if len(removed) != 1 || removed[0] != uri {
		t.Fatalf("Close removals = %v, want [%s]", removed, uri)
	}
}

func TestOptimizePassive_staleSnapshotRetainsOwnedPeerForClose(t *testing.T) {
	const uri = "tls://active.example:1"
	node := &staleSnapshotNodeObj{uri: uri}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:   []string{uri},
		Passive: true,
		Logger:  noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})

	if err = mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != uri {
		t.Fatalf("stale snapshot lost passive ownership: %v", got)
	}
	if err = mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	node.mu.Lock()
	removed := append([]string(nil), node.removed...)
	node.mu.Unlock()
	if len(removed) != 1 || removed[0] != uri {
		t.Fatalf("Close removals = %v, want [%s]", removed, uri)
	}
}

func TestOptimizeActive_singleBatchRetainsAllActivePeers(t *testing.T) {
	const firstURI = "tls://first:1"
	const candidateURI = "tls://candidate:1"
	const laterURI = "tls://later:1"
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo(firstURI, true, 5*time.Millisecond),
			makePeerInfo(laterURI, true, 10*time.Millisecond),
		},
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{firstURI, candidateURI, laterURI},
		ProbeTimeout: 20 * time.Millisecond,
		MaxPerProto:  2,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{firstURI, laterURI})

	if err = mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("optimize: %v", err)
	}
	// Active peers are compared in the single cycle without consuming the
	// BatchSize budget or requiring a second window.
	node.mu.Lock()
	removed := append([]string(nil), node.removed...)
	node.mu.Unlock()
	for _, uri := range removed {
		if uri == firstURI || uri == laterURI {
			t.Fatalf("already active peer was removed on timeout: %v", removed)
		}
	}
}

func TestOptimizeActiveAttemptsOnlyOneBatchPerCycle(t *testing.T) {
	peers := []string{"tls://a:1", "tls://b:1", "tls://c:1", "tls://d:1", "tls://e:1"}
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        peers,
		ProbeTimeout: time.Millisecond,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	node.mu.Lock()
	first := append([]string(nil), node.added...)
	node.mu.Unlock()
	if len(first) != 2 {
		t.Fatalf("first cycle added %d peers, want 2: %v", len(first), first)
	}
	if err = mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	node.mu.Lock()
	total := len(node.added)
	node.mu.Unlock()
	if total != 4 {
		t.Fatalf("two cycles added %d peers, want 4", total)
	}
}

func TestPartialRecoveryTargetsOnlyVacantProtocols(t *testing.T) {
	const (
		tlsWinner = "tls://winner:1"
		tlsLoser  = "tls://loser:1"
		quicDown  = "quic://down:1"
		quicSpare = "quic://spare:1"
	)
	node := &mockNodeObj{peers: []yggcore.PeerInfo{
		makePeerInfo(tlsWinner, true, time.Millisecond),
		makePeerInfo(quicDown, false, 0),
	}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:       []string{tlsWinner, tlsLoser, quicDown, quicSpare},
		BatchSize:   4,
		MaxPerProto: 1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{tlsWinner, quicDown})
	holdoff := time.Now().Add(time.Hour)
	mgr.probeState[normalizePeerURI(tlsLoser)] = probeStateObj{holdoffUntil: holdoff}
	mgr.probeState[normalizePeerURI(quicSpare)] = probeStateObj{holdoffUntil: holdoff}

	candidates := mgr.probeCycleCandidates(time.Now(), true, node.GetPeers())
	if hasCandidate(candidates, tlsLoser) {
		t.Fatalf("partial recovery selected held-off candidate from full tls protocol: %v", candidates)
	}
	if !hasCandidate(candidates, quicSpare) {
		t.Fatalf("partial recovery skipped held-off candidate from vacant quic protocol: %v", candidates)
	}
}

func TestPartialRecoveryAddsAtMostProtocolDeficit(t *testing.T) {
	const active = "tls://active:1"
	spares := []string{"tls://a:1", "tls://b:1", "tls://c:1"}
	peers := append([]string{active}, spares...)
	node := &mockNodeObj{peers: []yggcore.PeerInfo{makePeerInfo(active, true, time.Millisecond)}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:       peers,
		BatchSize:   4,
		MaxPerProto: 2,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{active})
	for _, uri := range spares {
		mgr.probeState[normalizePeerURI(uri)] = probeStateObj{holdoffUntil: time.Now().Add(time.Hour)}
	}

	candidates := mgr.probeCycleCandidates(time.Now(), true, node.GetPeers())
	if got := len(candidates) - 1; got != 1 {
		t.Fatalf("partial recovery challengers = %d, want one vacant slot: %v", got, candidates)
	}
}

func TestPartialRecoveryReservesMissingActiveButReplacesPresentDown(t *testing.T) {
	const (
		tcpUp      = "tcp://up:1"
		tlsMissing = "tls://missing:1"
		tlsSpare   = "tls://spare:1"
		quicDown   = "quic://down:1"
		quicSpare  = "quic://spare:1"
	)
	node := &mockNodeObj{peers: []yggcore.PeerInfo{
		makePeerInfo(tcpUp, true, time.Millisecond),
		makePeerInfo(quicDown, false, 0),
	}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:       []string{tcpUp, tlsMissing, tlsSpare, quicDown, quicSpare},
		BatchSize:   5,
		MaxPerProto: 1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{tcpUp, tlsMissing, quicDown})
	holdoff := time.Now().Add(time.Hour)
	mgr.probeState[normalizePeerURI(tlsSpare)] = probeStateObj{holdoffUntil: holdoff}
	mgr.probeState[normalizePeerURI(quicSpare)] = probeStateObj{holdoffUntil: holdoff}

	candidates := mgr.probeCycleCandidates(time.Now(), true, node.GetPeers())
	if hasCandidate(candidates, tlsSpare) {
		t.Fatalf("spare competed with the missing active reconnect: %v", candidates)
	}
	if !hasCandidate(candidates, quicSpare) {
		t.Fatalf("present-down active prevented replacement candidate: %v", candidates)
	}
}

func TestPartialRecoveryCursorRotatesLimitedCandidates(t *testing.T) {
	const active = "tls://active:1"
	spares := []string{"tls://a:1", "tls://b:1", "tls://c:1"}
	node := &mockNodeObj{peers: []yggcore.PeerInfo{makePeerInfo(active, true, time.Millisecond)}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:       append([]string{active}, spares...),
		BatchSize:   4,
		MaxPerProto: 2,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{active})
	now := time.Now()
	for _, uri := range spares {
		mgr.probeState[normalizePeerURI(uri)] = probeStateObj{holdoffUntil: now.Add(time.Hour)}
	}

	first := mgr.probeCycleCandidates(now, true, node.GetPeers())
	second := mgr.probeCycleCandidates(now, true, node.GetPeers())
	if !hasCandidate(first, spares[0]) || !hasCandidate(second, spares[1]) {
		t.Fatalf("cursor did not rotate limited recovery candidates: first=%v second=%v", first, second)
	}
}

func TestOutageBypassesHoldoffButNotFailureBackoff(t *testing.T) {
	const (
		held = "tls://held:1"
		dead = "quic://dead:1"
	)
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:     []string{held, dead},
		BatchSize: 2,
		Logger:    noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mgr.probeState[normalizePeerURI(held)] = probeStateObj{holdoffUntil: now.Add(time.Hour)}
	mgr.probeState[normalizePeerURI(dead)] = probeStateObj{retryAfter: now.Add(time.Hour)}

	candidates := mgr.probeCycleCandidates(now, true, node.GetPeers())
	if !hasCandidate(candidates, held) || hasCandidate(candidates, dead) {
		t.Fatalf("outage delay handling is wrong: %v", candidates)
	}
}

func TestActiveUpCountDeduplicatesPeerInfo(t *testing.T) {
	const uri = "tls://active:1"
	node := &mockNodeObj{peers: []yggcore.PeerInfo{
		makePeerInfo(uri, true, time.Millisecond),
		makePeerInfo(uri, true, 2*time.Millisecond),
	}}
	mgr, err := newTestObj(node, fastCfg([]string{uri}))
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})
	if got := mgr.activeUpCount(); got != 1 {
		t.Fatalf("active Up count = %d, want 1", got)
	}
}

func TestOptimizeActiveOutageRespectsFailureBackoff(t *testing.T) {
	peers := []string{"tls://a:1", "tls://b:1", "tls://c:1"}
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        peers,
		ProbeTimeout: time.Millisecond,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range mgr.peers {
		mgr.probeState[peerEntryKey(peer)] = probeStateObj{failures: 10, retryAfter: time.Now().Add(time.Hour)}
	}
	if err = mgr.optimizeActiveMode(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	node.mu.Lock()
	added := len(node.added)
	node.mu.Unlock()
	if added != 0 {
		t.Fatalf("outage cycle bypassed failure backoff for %d peers", added)
	}
}

func TestOptimizeActiveDoesNotWaitWithoutNewConnection(t *testing.T) {
	const uri = "tls://a:1"
	node := &mockNodeObj{peers: []yggcore.PeerInfo{makePeerInfo(uri, true, time.Millisecond)}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{uri},
		ProbeTimeout: time.Second,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})

	started := time.Now()
	if err := mgr.optimizeActiveMode(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("cycle without a new connection waited %s", elapsed)
	}
}

func TestRunPartialDegradationBypassesReprobeHoldoff(t *testing.T) {
	const (
		first  = "tls://a:1"
		failed = "tls://b:1"
		spare  = "tls://c:1"
	)
	node := &mockNodeObj{peers: []yggcore.PeerInfo{
		makePeerInfo(first, true, time.Millisecond),
		makePeerInfo(failed, false, 0),
		makePeerInfo(spare, true, 2*time.Millisecond),
	}}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:                 []string{first, failed, spare},
		ProbeTimeout:          time.Millisecond,
		MaxPerProto:           2,
		MinPeers:              1,
		MinPeersConfirmations: 1,
		HealthInterval:        5 * time.Millisecond,
		ReprobeInterval:       time.Hour,
		Logger:                noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{first, failed})
	mgr.probeState[normalizePeerURI(spare)] = probeStateObj{holdoffUntil: time.Now().Add(time.Hour)}
	_ = mgr.tasks.Go(mgr.run)
	t.Cleanup(func() { _ = mgr.Close() })

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		node.mu.Lock()
		addedSpare := false
		for _, uri := range node.added {
			if uri == spare {
				addedSpare = true
				break
			}
		}
		node.mu.Unlock()
		if addedSpare {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("health recovery did not bypass the spare peer holdoff")
}

func TestRunHealthTickerRetriesAfterFailedStartup(t *testing.T) {
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:          []string{"tls://a:1"},
		ProbeTimeout:   time.Millisecond,
		HealthInterval: 5 * time.Millisecond,
		Logger:         noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = mgr.tasks.Go(mgr.run)
	t.Cleanup(func() {
		if closeErr := mgr.Close(); closeErr != nil {
			t.Errorf("Close: %v", closeErr)
		}
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		node.mu.Lock()
		attempts := node.addAttempts
		node.mu.Unlock()
		if attempts >= 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	node.mu.Lock()
	attempts := node.addAttempts
	node.mu.Unlock()
	t.Fatalf("health ticker made %d attempts, want at least 2", attempts)
}

func TestOptimizeActive_keepsPeerWithQuery(t *testing.T) {
	const uri = "tls://a:1?password=x"
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
		},
	}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{uri},
		ProbeTimeout: time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("optimizeLocked: %v", err)
	}
	active := mgr.Active()
	if len(active) != 1 || active[0] != uri {
		t.Fatalf("expected active full URI %q, got %v", uri, active)
	}
	node.mu.Lock()
	removed := append([]string(nil), node.removed...)
	node.mu.Unlock()
	if len(removed) != 0 {
		t.Fatalf("peer with query should not be removed, got %v", removed)
	}
}

// //

// A cycle cancelled mid-probe must retain the peers that were already active so
// Close still removes them — dropping them here would leak established peers.
func TestOptimizeActive_cancelRetainsAlreadyActivePeers(t *testing.T) {
	node := &mockNodeObj{}
	mgr, err := newTestObj(node, ConfigObj{
		Peers:        []string{"tls://a.example:1"},
		MaxPerProto:  1,
		ProbeTimeout: time.Hour,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Simulate an established active peer the node already holds.
	mgr.setActive([]string{"tls://a.example:1"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mgr.optimizeActiveMode(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != "tls://a.example:1" {
		t.Fatalf("cancelled cycle must retain the active peer, got %v", got)
	}
	if len(node.removed) != 0 {
		t.Fatalf("cancelled cycle must not remove peers itself, removed %v", node.removed)
	}
}

func TestCloseRetainsFailedRemovalForRetry(t *testing.T) {
	const uri = "tls://a.example:1"
	node := &mockNodeObj{removePeerFail: map[string]bool{uri: true}}
	mgr, err := newTestObj(node, ConfigObj{Peers: []string{uri}, Logger: noopLogObj{}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})
	if err := mgr.Close(); err == nil {
		t.Fatal("Close must report RemovePeer failure")
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != uri {
		t.Fatalf("failed removal lost teardown ownership: %v", got)
	}
	node.mu.Lock()
	delete(node.removePeerFail, uri)
	node.mu.Unlock()
	if err := mgr.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if got := mgr.Active(); len(got) != 0 {
		t.Fatalf("active after successful retry: %v", got)
	}
}

func TestSelectAndPruneRetainsFailedRemoval(t *testing.T) {
	const winnerURI = "tls://winner.example:1"
	const loserURI = "tls://loser.example:1"
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo(winnerURI, true, time.Millisecond),
			makePeerInfo(loserURI, true, 2*time.Millisecond),
		},
		removePeerFail: map[string]bool{loserURI: true},
	}
	mgr, err := newTestObj(node, ConfigObj{Peers: []string{winnerURI, loserURI}, MaxPerProto: 1, Logger: noopLogObj{}})
	if err != nil {
		t.Fatal(err)
	}
	managed := map[string]string{normalizePeerURI(winnerURI): winnerURI, normalizePeerURI(loserURI): loserURI}
	kept := mgr.selectAndPrune(mgr.peers, time.Millisecond, managed)
	if len(kept) != 2 {
		t.Fatalf("kept = %d, want selected plus failed removal", len(kept))
	}
	if _, ok := managed[normalizePeerURI(loserURI)]; !ok {
		t.Fatal("failed removal was deleted from managed set")
	}
}

func TestPassiveAndNoReachableNotification(t *testing.T) {
	const uri = "future+transport://peer.example:1"
	node := &mockNodeObj{}
	notifications := make(chan struct{}, 1)
	mgr, err := newTestObj(node, ConfigObj{
		Peers:            []string{uri},
		Passive:          true,
		MinPeers:         1,
		NoReachablePeers: notifications,
		Logger:           noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mgr.cfg.MinPeers != 0 {
		t.Fatal("MinPeers must be disabled in passive mode")
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != uri {
		t.Fatalf("passive active = %v", got)
	}
	mgr.cfg.Passive = false
	mgr.reportResult(nil)
	mgr.reportResult(nil)
	select {
	case <-notifications:
	default:
		t.Fatal("no-reachable notification was not delivered")
	}
	select {
	case <-notifications:
		t.Fatal("slow receiver accumulated duplicate no-reachable notifications")
	default:
	}
}

func TestOptimizeDoesNotBlockOnUnreadNoReachableNotification(t *testing.T) {
	const uri = "tls://unreachable.example:1"
	mgr, err := newTestObj(&mockNodeObj{}, ConfigObj{
		Peers:            []string{uri},
		ProbeTimeout:     time.Millisecond,
		NoReachablePeers: make(chan struct{}),
		Logger:           noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- mgr.Optimize() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("Optimize: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Optimize blocked on an unread no-reachable notification channel")
	}
}

// //

func BenchmarkNew(b *testing.B) {
	node := &mockNodeObj{}
	peers := make([]string, 20)
	for i := range peers {
		peers[i] = "tls://host" + string(rune('a'+i%26)) + ":1234"
	}
	cfg := fastCfg(peers)
	cfg.Node = node
	for b.Loop() {
		mgr, err := New(cfg)
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		if err = mgr.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}
}
