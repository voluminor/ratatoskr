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

// //

func TestNew_valid(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err != nil || mgr == nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_nilNode(t *testing.T) {
	_, err := New(nil, fastCfg([]string{"tls://h:1"}))
	if !errors.Is(err, ErrNodeRequired) {
		t.Fatalf("expected ErrNodeRequired, got %v", err)
	}
}

func TestNew_noLogger(t *testing.T) {
	// A nil logger is accepted and normalized to a discard logger.
	mgr, err := New(&mockNodeObj{}, ConfigObj{
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
	_, err := New(&mockNodeObj{}, ConfigObj{Logger: noopLogObj{}})
	if err == nil {
		t.Fatal("expected error: no valid peers")
	}
}

func TestNew_allInvalidPeers(t *testing.T) {
	_, err := New(&mockNodeObj{}, ConfigObj{
		Peers:  []string{"tcp://%zz", "://nohost"},
		Logger: noopLogObj{},
	})
	if err == nil {
		t.Fatal("expected error: all peers invalid")
	}
}

func TestNew_defaultMaxPerProto(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if mgr.cfg.MaxPerProto != 1 {
		t.Errorf("expected MaxPerProto=1, got %d", mgr.cfg.MaxPerProto)
	}
}

func TestNew_rejectsInvalidMaxPerProto(t *testing.T) {
	_, err := New(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://h:1"},
		MaxPerProto: -2,
		Logger:      noopLogObj{},
	})
	if !errors.Is(err, ErrInvalidMaxPerProto) {
		t.Fatalf("expected ErrInvalidMaxPerProto, got %v", err)
	}
}

func TestNew_defaultProbeTimeout(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, ConfigObj{
		Peers:        []string{"tls://h:1"},
		Logger:       noopLogObj{},
		ProbeTimeout: 0,
	})
	if mgr.cfg.ProbeTimeout != defaultProbeTimeout {
		t.Errorf("expected defaultProbeTimeout, got %v", mgr.cfg.ProbeTimeout)
	}
}

func TestEffectiveWindow_usesBoundedDefault(t *testing.T) {
	if got := effectiveWindow(0, defaultBatchSize+10); got != defaultBatchSize {
		t.Fatalf("default window = %d, want %d", got, defaultBatchSize)
	}
	if got := effectiveWindow(1, defaultBatchSize+10); got != defaultBatchSize {
		t.Fatalf("legacy window = %d, want %d", got, defaultBatchSize)
	}
	if got := effectiveWindow(8, defaultBatchSize+10); got != 8 {
		t.Fatalf("configured window = %d, want 8", got)
	}
	if got := effectiveWindow(maxBatchSize+100, maxBatchSize+200); got != maxBatchSize {
		t.Fatalf("capped window = %d, want %d", got, maxBatchSize)
	}
	if got := effectiveWindow(0, 3); got != 3 {
		t.Fatalf("small total window = %d, want 3", got)
	}
}

func TestNew_partiallyInvalidPeers(t *testing.T) {
	// Some valid, some not — should succeed with valid only
	mgr, err := New(&mockNodeObj{}, fastCfg([]string{"tcp://%zz", "tls://good:1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mgr.peers) != 1 || mgr.peers[0].Scheme != "tls" {
		t.Errorf("unexpected peers: %v", mgr.peers)
	}
}

// //

func TestStartStop(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = mgr.Stop()
}

func TestDoubleStart(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Stop() }()
	if err := mgr.Start(); err == nil {
		t.Fatal("expected error on double Start")
	}
}

func TestStop_idempotent(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = mgr.Stop()
	_ = mgr.Stop() // must not panic or block
}

func TestStop_beforeStart(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	_ = mgr.Stop() // must not panic
}

func TestStartDuringStopReturnsAlreadyRunning(t *testing.T) {
	node := &blockingAddNodeObj{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	mgr, err := New(node, fastCfg([]string{"tls://h:1"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err = mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-node.entered:
	case <-time.After(time.Second):
		close(node.release)
		t.Fatal("manager did not enter AddPeer")
	}

	stopDone := make(chan struct{})
	go func() {
		_ = mgr.Stop()
		close(stopDone)
	}()

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err = mgr.Start(); !errors.Is(err, ErrAlreadyRunning) {
			close(node.release)
			t.Fatalf("Start during Stop = %v, want ErrAlreadyRunning", err)
		}
		time.Sleep(time.Millisecond)
	}
	close(node.release)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after blocked AddPeer was released")
	}
}

func TestOptimizeDuringStopReturnsNotRunning(t *testing.T) {
	node := &blockingAddNodeObj{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	mgr, err := New(node, ConfigObj{
		Peers:       []string{"tls://h:1"},
		MaxPerProto: 1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err = mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-node.entered:
	case <-time.After(time.Second):
		close(node.release)
		t.Fatal("manager did not enter AddPeer")
	}

	stopDone := make(chan struct{})
	go func() {
		_ = mgr.Stop()
		close(stopDone)
	}()

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err = mgr.Optimize(); !errors.Is(err, ErrNotRunning) {
			close(node.release)
			t.Fatalf("Optimize during Stop = %v, want ErrNotRunning", err)
		}
		time.Sleep(time.Millisecond)
	}
	close(node.release)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after blocked AddPeer was released")
	}
}

func TestRefreshInterval_reoptimizesWhileRunning(t *testing.T) {
	// AddPeer always fails, so the candidate never becomes active and never earns
	// backoff; every refresh re-probes it, letting us count reoptimizations by adds.
	node := &recordingFailNodeObj{err: errors.New("temporary failure")}
	mgr, err := New(node, ConfigObj{
		Peers:           []string{"tls://h:1"},
		ProbeTimeout:    10 * time.Millisecond,
		MaxPerProto:     1,
		RefreshInterval: 10 * time.Millisecond,
		Logger:          noopLogObj{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()

	waitAddedPeers(t, &node.mockNodeObj, 1)
	waitAddedPeers(t, &node.mockNodeObj, 2)
}

func TestOptimize_notRunning(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err := mgr.Optimize(); err == nil {
		t.Fatal("expected error when not running")
	}
}

func TestActive_beforeStart(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if act := mgr.Active(); len(act) != 0 {
		t.Errorf("expected empty active list before start, got %v", act)
	}
}

func TestActive_returnsCopy(t *testing.T) {
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{makePeerInfo("tls://a:1", true, 5*time.Millisecond)},
	}
	mgr, _ := New(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
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
	mgr, _ := New(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()
	if err := mgr.Optimize(); err != nil {
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
	mgr, _ := New(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()
	if err := mgr.Optimize(); err != nil {
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

func TestActiveMode_batchSize(t *testing.T) {
	node := &mockNodeObj{}
	peers := make([]string, 6)
	for i := range peers {
		peers[i] = "tls://h" + string(rune('a'+i)) + ":1"
	}
	mgr, err := New(node, ConfigObj{
		Peers:        peers,
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		BatchSize:    2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()
	if err := mgr.Optimize(); err != nil {
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
	mgr, err := New(node, ConfigObj{
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
	mgr, err := New(node, ConfigObj{
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
		state.nextTry = time.Now().Add(time.Hour)
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
	mgr, err := New(node, ConfigObj{
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
	if !ok || state.failures == 0 || !state.nextTry.After(time.Now()) {
		t.Fatalf("failing AddPeer did not create backoff: state=%+v ok=%v", state, ok)
	}
	if due := mgr.probeCandidates(time.Now()); len(due) != 0 {
		t.Fatalf("backed-off failing peer still due for probing: %v", due)
	}
}

// //

func TestStop_removesActivePeers(t *testing.T) {
	// Both peers are up on distinct protocols, so both win selection and stay
	// active; Stop must then remove them from the node.
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
			makePeerInfo("tcp://b:2", true, 10*time.Millisecond),
		},
	}
	mgr, _ := New(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tcp://b:2"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Let the startup optimize settle both peers into the active set before Stop, so
	// Stop reaps a fully populated set rather than racing the initial probe cycle.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.Active()) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if act := mgr.Active(); len(act) != 2 {
		t.Fatalf("expected 2 active peers before Stop, got %v", act)
	}

	node.mu.Lock()
	beforeStop := len(node.removed)
	node.mu.Unlock()

	_ = mgr.Stop()

	node.mu.Lock()
	afterStop := len(node.removed)
	node.mu.Unlock()

	if afterStop-beforeStop < 2 {
		t.Errorf("Stop should remove both active peers, removed %d", afterStop-beforeStop)
	}
}

func TestOptimizeActive_cancelRemovesPendingBatch(t *testing.T) {
	// A cycle cancelled mid-probe performs no per-batch rollback; instead it records
	// every peer it handed to the node as active, so Stop reaps exactly those.
	node := &mockNodeObj{}
	mgr, err := New(node, ConfigObj{
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
		t.Fatalf("expected the two added peers left active for Stop to reap, got %v", active)
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
	mgr, err := New(node, ConfigObj{
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
	mgr, err := New(node, ConfigObj{
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

func TestOptimizeActive_timeoutRetainsUnprocessedActivePeer(t *testing.T) {
	const firstURI = "tls://first:1"
	const candidateURI = "tls://candidate:1"
	const laterURI = "tls://later:1"
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo(firstURI, true, 5*time.Millisecond),
			makePeerInfo(laterURI, true, 10*time.Millisecond),
		},
	}
	mgr, err := New(node, ConfigObj{
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

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err = mgr.optimizeLocked(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected optimize deadline, got %v", err)
	}
	// The deadline strikes during the second window; neither the winner already
	// selected in the first window (firstURI) nor the still-unprobed active peer
	// awaiting a later window (laterURI) may be torn down by the interrupted cycle.
	node.mu.Lock()
	removed := append([]string(nil), node.removed...)
	node.mu.Unlock()
	for _, uri := range removed {
		if uri == firstURI || uri == laterURI {
			t.Fatalf("already active peer was removed on timeout: %v", removed)
		}
	}
}

func TestOptimizeActive_keepsPeerWithQuery(t *testing.T) {
	const uri = "tls://a:1?password=x"
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
		},
	}
	mgr, err := New(node, ConfigObj{
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
// Stop still removes them — dropping them here would leak established peers.
func TestOptimizeActive_cancelRetainsAlreadyActivePeers(t *testing.T) {
	node := &mockNodeObj{}
	mgr, err := New(node, ConfigObj{
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
	if err := mgr.optimizeActive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != "tls://a.example:1" {
		t.Fatalf("cancelled cycle must retain the active peer, got %v", got)
	}
	if len(node.removed) != 0 {
		t.Fatalf("cancelled cycle must not remove peers itself, removed %v", node.removed)
	}
}

func TestStopRetainsFailedRemovalForRetry(t *testing.T) {
	const uri = "tls://a.example:1"
	node := &mockNodeObj{removePeerFail: map[string]bool{uri: true}}
	mgr, err := New(node, ConfigObj{Peers: []string{uri}, Logger: noopLogObj{}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{uri})
	if err := mgr.Stop(); err == nil {
		t.Fatal("Stop must report RemovePeer failure")
	}
	if got := mgr.Active(); len(got) != 1 || got[0] != uri {
		t.Fatalf("failed removal lost teardown ownership: %v", got)
	}
	node.mu.Lock()
	delete(node.removePeerFail, uri)
	node.mu.Unlock()
	if err := mgr.Stop(); err != nil {
		t.Fatalf("retry Stop: %v", err)
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
	mgr, err := New(node, ConfigObj{Peers: []string{winnerURI, loserURI}, MaxPerProto: 1, Logger: noopLogObj{}})
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

func TestPassiveAndNoReachableCallback(t *testing.T) {
	const uri = "future+transport://peer.example:1"
	node := &mockNodeObj{}
	called := 0
	mgr, err := New(node, ConfigObj{
		Peers:              []string{uri},
		Passive:            true,
		MinPeers:           1,
		OnNoReachablePeers: func() { called++ },
		Logger:             noopLogObj{},
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
	if called != 1 {
		t.Fatalf("OnNoReachablePeers calls = %d, want 1", called)
	}
}

// //

func BenchmarkNew(b *testing.B) {
	node := &mockNodeObj{}
	peers := make([]string, 20)
	for i := range peers {
		peers[i] = "tls://host" + string(rune('a'+i%26)) + ":1234"
	}
	for b.Loop() {
		if _, err := New(node, fastCfg(peers)); err != nil {
			b.Fatalf("New: %v", err)
		}
	}
}
