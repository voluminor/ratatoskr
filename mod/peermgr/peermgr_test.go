package peermgr

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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

func waitCounter(t *testing.T, counter *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for counter %d, got %d", want, counter.Load())
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

type failingAddNodeObj struct {
	mockNodeObj
	fail map[string]error
}

func (n *failingAddNodeObj) AddPeer(uri string) error {
	if err := n.fail[uri]; err != nil {
		return err
	}
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

type cancelAfterAddNodeObj struct {
	mockNodeObj
	cancel context.CancelFunc
	count  atomic.Int64
}

func (n *cancelAfterAddNodeObj) AddPeer(uri string) error {
	err := n.mockNodeObj.AddPeer(uri)
	if n.count.Add(1) == 1 {
		n.cancel()
	}
	return err
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
		Peers:  []string{"ftp://bad:21", "not-a-uri"},
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

func TestNew_watchUsesFixedDefaults(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://h:1", "tls://h:2", "tls://h:3"},
		Logger:      noopLogObj{},
		MinPeers:    1,
		MaxPerProto: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mgr.watchInterval != defaultWatchInterval {
		t.Fatalf("watch interval = %s, want %s", mgr.watchInterval, defaultWatchInterval)
	}
	if mgr.watchNeed != defaultMinPeersConfirmations {
		t.Fatalf("watch confirmations = %d, want %d", mgr.watchNeed, defaultMinPeersConfirmations)
	}
}

func TestEffectiveBatchSize_usesBoundedDefault(t *testing.T) {
	if got := effectiveBatchSize(0, defaultBatchSize+10); got != defaultBatchSize {
		t.Fatalf("default batch size = %d, want %d", got, defaultBatchSize)
	}
	if got := effectiveBatchSize(1, defaultBatchSize+10); got != defaultBatchSize {
		t.Fatalf("legacy batch size = %d, want %d", got, defaultBatchSize)
	}
	if got := effectiveBatchSize(8, defaultBatchSize+10); got != 8 {
		t.Fatalf("configured batch size = %d, want 8", got)
	}
	if got := effectiveBatchSize(maxBatchSize+100, maxBatchSize+200); got != maxBatchSize {
		t.Fatalf("capped batch size = %d, want %d", got, maxBatchSize)
	}
	if got := effectiveBatchSize(0, 3); got != 3 {
		t.Fatalf("small total batch size = %d, want 3", got)
	}
}

func TestEffectiveRefreshInterval_clampsSmallPositiveValues(t *testing.T) {
	if got := effectiveRefreshInterval(time.Nanosecond); got != minRefreshInterval {
		t.Fatalf("small refresh interval = %s, want %s", got, minRefreshInterval)
	}
	if got := effectiveRefreshInterval(0); got != 0 {
		t.Fatalf("disabled refresh interval = %s, want 0", got)
	}
}

func TestNew_partiallyInvalidPeers(t *testing.T) {
	// Some valid, some not — should succeed with valid only
	mgr, err := New(&mockNodeObj{}, fastCfg([]string{"ftp://bad:1", "tls://good:1"}))
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
	mgr.Stop()
}

func TestDoubleStart(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()
	if err := mgr.Start(); err == nil {
		t.Fatal("expected error on double Start")
	}
}

func TestStop_idempotent(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mgr.Stop()
	mgr.Stop() // must not panic or block
}

func TestStop_beforeStart(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	mgr.Stop() // must not panic
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
		mgr.Stop()
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
		MaxPerProto: -1,
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
		mgr.Stop()
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
	node := &recordingFailNodeObj{err: errors.New("temporary failure")}
	mgr, err := New(node, ConfigObj{
		Peers:           []string{"tls://h:1"},
		MaxPerProto:     -1,
		RefreshInterval: 10 * time.Millisecond,
		Logger:          noopLogObj{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()

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
	node := &mockNodeObj{}
	mgr, _ := New(node, ConfigObj{
		Peers:       []string{"tls://a:1"},
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	a := mgr.Active()
	b := mgr.Active()
	if len(a) > 0 && &a[0] == &b[0] {
		t.Error("Active() should return independent copies")
	}
}

// //

func TestPassiveMode_addsAllPeers(t *testing.T) {
	node := &mockNodeObj{}
	uris := []string{"tls://a:1", "tcp://b:2", "quic://c:3"}
	mgr, err := New(node, ConfigObj{
		Peers:       uris,
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	// Optimize blocks until done → active is set on return
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	active := mgr.Active()
	if len(active) != len(uris) {
		t.Errorf("expected %d active, got %d: %v", len(uris), len(active), active)
	}
}

func TestPassiveMode_callsAddPeer(t *testing.T) {
	node := &mockNodeObj{}
	uris := []string{"tls://a:1", "tcp://b:2"}
	mgr, _ := New(node, ConfigObj{
		Peers:       uris,
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	node.mu.Lock()
	addedCount := len(node.added)
	node.mu.Unlock()
	if addedCount < len(uris) {
		t.Errorf("expected AddPeer called ≥%d times, got %d", len(uris), addedCount)
	}
}

func TestPassiveMode_refreshDoesNotReconnectActivePeers(t *testing.T) {
	node := &mockNodeObj{}
	uris := []string{"tls://a:1", "tcp://b:2"}
	mgr, err := New(node, ConfigObj{
		Peers:       uris,
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("first optimizeLocked: %v", err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("second optimizeLocked: %v", err)
	}

	node.mu.Lock()
	addedCount := len(node.added)
	removedCount := len(node.removed)
	node.mu.Unlock()
	if addedCount != len(uris) {
		t.Fatalf("expected active peers to be added once, got %d adds", addedCount)
	}
	if removedCount != 0 {
		t.Fatalf("passive refresh must not remove active peers, got %d removes", removedCount)
	}
}

func TestPassiveMode_activeTracksSuccessfulAdds(t *testing.T) {
	wantErr := errors.New("dial rejected")
	node := &failingAddNodeObj{fail: map[string]error{
		"tcp://b:2": wantErr,
	}}
	uris := []string{"tls://a:1", "tcp://b:2", "quic://c:3"}
	mgr, err := New(node, ConfigObj{
		Peers:       uris,
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("optimizeLocked: %v", err)
	}

	active := mgr.Active()
	if len(active) != 2 {
		t.Fatalf("expected 2 active peers, got %d: %v", len(active), active)
	}
	for _, uri := range active {
		if uri == "tcp://b:2" {
			t.Fatalf("failed peer must not be active: %v", active)
		}
	}
}

func TestPassiveMode_cancelKeepsAccuratePartialActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	node := &cancelAfterAddNodeObj{cancel: cancel}
	uris := []string{"tls://a:1", "tcp://b:2", "quic://c:3"}
	mgr, err := New(node, ConfigObj{
		Peers:       uris,
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setActive([]string{"tls://a:1"})

	err = mgr.optimizePassive(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	active := mgr.Active()
	if len(active) != 2 {
		t.Fatalf("expected partial active list of 2 peers, got %v", active)
	}
	if active[0] != "tls://a:1" || active[1] != "tcp://b:2" {
		t.Fatalf("unexpected partial active list: %v", active)
	}
}

// //

func TestActiveMode_noReachable_callsCallback(t *testing.T) {
	node := &mockNodeObj{} // GetPeers returns [] → no up peers
	called := make(chan struct{}, 1)
	mgr, err := New(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
		OnNoReachablePeers: func() {
			select {
			case called <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Error("OnNoReachablePeers was not called")
	}
}

func TestActiveMode_noReachableCallbackCanOptimize(t *testing.T) {
	node := &mockNodeObj{}
	called := make(chan error, 1)
	var (
		mgr        *Obj
		dispatched atomic.Bool
	)
	var err error
	mgr, err = New(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
		OnNoReachablePeers: func() {
			if dispatched.CompareAndSwap(false, true) {
				called <- mgr.Optimize()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()

	select {
	case err := <-called:
		if err != nil {
			t.Fatalf("nested Optimize from callback failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback Optimize deadlocked")
	}
}

func TestActiveMode_noReachableCallbackDebounced(t *testing.T) {
	var calls atomic.Int64
	release := make(chan struct{})
	node := &mockNodeObj{}
	mgr, err := New(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
		OnNoReachablePeers: func() {
			calls.Add(1)
			<-release
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer close(release)

	for range 3 {
		if err := mgr.optimizeLocked(context.Background()); err != nil {
			t.Fatalf("optimizeLocked: %v", err)
		}
	}
	waitCounter(t, &calls, 1)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one active callback, got %d", got)
	}
}

// Stop is fire-and-forget: it must return even while a user callback is still
// blocked, so shutdown latency never depends on arbitrary user code.
func TestStop_doesNotWaitForNoReachableCallback(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	node := &mockNodeObj{}
	mgr, err := New(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
		OnNoReachablePeers: func() {
			close(started)
			<-release
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer close(release)
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}

	stopped := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on the user callback")
	}
}

// A panicking OnNoReachablePeers must not crash the process and must not block Stop.
func TestNoReachableCallbackPanic_recovered(t *testing.T) {
	started := make(chan struct{})
	node := &mockNodeObj{}
	mgr, err := New(node, ConfigObj{
		Peers:        []string{"tls://a:1"},
		ProbeTimeout: time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
		OnNoReachablePeers: func() {
			close(started)
			panic("boom")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		mgr.Stop()
		t.Fatal("callback did not run")
	}

	stopped := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked after a panicking callback")
	}
}

// A change to the active peer set must invoke OnActiveChange with the new set.
func TestOnActiveChange_firesOnSetChange(t *testing.T) {
	const good = "tls://good:1"
	got := make(chan []string, 1)
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{makePeerInfo(good, true, 5*time.Millisecond)},
	}
	mgr, err := New(node, ConfigObj{
		Peers:        []string{good},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
		OnActiveChange: func(active []string) {
			select {
			case got <- active:
			default:
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("optimizeLocked: %v", err)
	}

	select {
	case active := <-got:
		if len(active) != 1 || active[0] != good {
			t.Fatalf("unexpected active set: %v", active)
		}
	case <-time.After(time.Second):
		t.Fatal("OnActiveChange was not called")
	}
}

// A no-op re-optimize with an unchanged active set must not fire OnActiveChange.
func TestOnActiveChange_skipsNoOp(t *testing.T) {
	const good = "tls://good:1"
	var calls atomic.Int64
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{makePeerInfo(good, true, 5*time.Millisecond)},
	}
	mgr, err := New(node, ConfigObj{
		Peers:          []string{good},
		ProbeTimeout:   10 * time.Millisecond,
		MaxPerProto:    1,
		Logger:         noopLogObj{},
		OnActiveChange: func([]string) { calls.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("first optimizeLocked: %v", err)
	}
	waitCounter(t, &calls, 1)
	// Second pass keeps the same active peer; the set does not change.
	if err := mgr.optimizeLocked(context.Background()); err != nil {
		t.Fatalf("second optimizeLocked: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly one OnActiveChange, got %d", got)
	}
}

func TestOnActiveChange_coalescesSlowCallbacks(t *testing.T) {
	var first atomic.Bool
	started := make(chan struct{})
	release := make(chan struct{})
	got := make(chan []string, 3)
	mgr, err := New(&mockNodeObj{}, ConfigObj{
		Peers:  []string{"tls://a:1"},
		Logger: noopLogObj{},
		OnActiveChange: func(active []string) {
			got <- append([]string(nil), active...)
			if first.CompareAndSwap(false, true) {
				close(started)
				<-release
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	mgr.dispatchActiveChange([]string{"one"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first OnActiveChange did not start")
	}

	mgr.dispatchActiveChange([]string{"two"})
	mgr.dispatchActiveChange([]string{"three"})
	close(release)

	select {
	case active := <-got:
		if len(active) != 1 || active[0] != "one" {
			t.Fatalf("unexpected first active set: %v", active)
		}
	case <-time.After(time.Second):
		t.Fatal("first OnActiveChange result missing")
	}
	select {
	case active := <-got:
		if len(active) != 1 || active[0] != "three" {
			t.Fatalf("expected latest coalesced active set, got %v", active)
		}
	case <-time.After(time.Second):
		t.Fatal("coalesced OnActiveChange result missing")
	}
	select {
	case active := <-got:
		t.Fatalf("unexpected extra OnActiveChange call: %v", active)
	case <-time.After(50 * time.Millisecond):
	}
}

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
	defer mgr.Stop()
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if act := mgr.Active(); len(act) != 0 {
		t.Errorf("expected empty active (no up peers), got %v", act)
	}
}

func TestActiveMode_removesLosers(t *testing.T) {
	node := &mockNodeObj{}
	mgr, _ := New(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  1,
		Logger:       noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	node.mu.Lock()
	removedCount := len(node.removed)
	node.mu.Unlock()
	if removedCount < 2 {
		t.Errorf("expected ≥2 RemovePeer calls, got %d", removedCount)
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
	defer mgr.Stop()
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
	var callbacks atomic.Int64
	node := &mockNodeObj{}
	mgr, err := New(node, ConfigObj{
		Peers:              []string{"tls://a:1", "tls://b:2", "tls://c:3"},
		ProbeTimeout:       10 * time.Millisecond,
		MaxPerProto:        1,
		Logger:             noopLogObj{},
		OnNoReachablePeers: func() { callbacks.Add(1) },
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
	waitCounter(t, &callbacks, 1)
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
	if got := callbacks.Load(); got != 1 {
		t.Fatalf("expected no extra callback while all candidates are backed off, got %d", got)
	}
}

// //

func TestStop_removesActivePeers(t *testing.T) {
	node := &mockNodeObj{}
	mgr, _ := New(node, ConfigObj{
		Peers:       []string{"tls://a:1", "tcp://b:2"},
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := mgr.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	node.mu.Lock()
	beforeStop := len(node.removed)
	node.mu.Unlock()

	mgr.Stop()

	node.mu.Lock()
	afterStop := len(node.removed)
	node.mu.Unlock()

	if afterStop <= beforeStop {
		t.Error("Stop should call RemovePeer for active peers")
	}
}

func TestOptimizeActive_cancelRemovesPendingBatch(t *testing.T) {
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

	node.mu.Lock()
	removed := len(node.removed)
	node.mu.Unlock()
	if removed < 2 {
		t.Fatalf("expected pending peers to be removed, got %d removals", removed)
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
	active := mgr.Active()
	if len(active) != 1 || active[0] != activeURI {
		t.Fatalf("active peer changed after timeout: %v", active)
	}
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
	active := mgr.Active()
	if len(active) != 2 {
		t.Fatalf("expected both old active peers to remain tracked, got %v", active)
	}
	seen := make(map[string]bool, len(active))
	for _, uri := range active {
		seen[uri] = true
	}
	if !seen[firstURI] || !seen[laterURI] {
		t.Fatalf("old active peer became untracked after timeout: %v", active)
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

func TestNew_minPeersIgnoredPassive(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://a:1", "tls://b:2", "tls://c:3"},
		MaxPerProto: -1,
		MinPeers:    2,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.cfg.MinPeers != 0 {
		t.Errorf("expected MinPeers reset to 0 in passive mode, got %d", mgr.cfg.MinPeers)
	}
}

func TestNew_minPeersValid(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://a:1", "tls://b:2", "tls://c:3"},
		MaxPerProto: 3,
		MinPeers:    2,
		Logger:      noopLogObj{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.cfg.MinPeers != 2 {
		t.Errorf("expected MinPeers=2, got %d", mgr.cfg.MinPeers)
	}
}

func TestNew_minPeersDisabled(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, fastCfg([]string{"tls://a:1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.cfg.MinPeers != 0 {
		t.Errorf("expected MinPeers=0 by default, got %d", mgr.cfg.MinPeers)
	}
}

// //

func TestWatchPeers_triggersOptimize(t *testing.T) {
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
			makePeerInfo("tls://b:2", true, 10*time.Millisecond),
			makePeerInfo("tls://c:3", true, 15*time.Millisecond),
			makePeerInfo("tls://d:4", true, 20*time.Millisecond),
		},
	}
	mgr, err := New(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2", "tls://c:3", "tls://d:4"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  4,
		MinPeers:     2,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.watchInterval = 10 * time.Millisecond
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()

	// Wait for initial optimize to finish
	time.Sleep(50 * time.Millisecond)

	// Record added count before drop
	node.mu.Lock()
	addedBefore := len(node.added)
	node.mu.Unlock()

	// Drop peers to trigger watch: only 2 up = threshold
	node.mu.Lock()
	node.peers = []yggcore.PeerInfo{
		makePeerInfo("tls://a:1", true, 5*time.Millisecond),
		makePeerInfo("tls://b:2", true, 10*time.Millisecond),
		makePeerInfo("tls://c:3", false, 0),
		makePeerInfo("tls://d:4", false, 0),
	}
	node.mu.Unlock()

	// Wait for 3 confirmations + optimize to run
	time.Sleep(200 * time.Millisecond)

	node.mu.Lock()
	addedAfter := len(node.added)
	node.mu.Unlock()

	if addedAfter <= addedBefore {
		t.Errorf("expected optimize to be triggered by watch (AddPeer calls: before=%d, after=%d)", addedBefore, addedAfter)
	}
}

func TestWatchPeers_resetsOnRecovery(t *testing.T) {
	node := &mockNodeObj{
		peers: []yggcore.PeerInfo{
			makePeerInfo("tls://a:1", true, 5*time.Millisecond),
			makePeerInfo("tls://b:2", true, 10*time.Millisecond),
			makePeerInfo("tls://c:3", true, 15*time.Millisecond),
		},
	}
	mgr, err := New(node, ConfigObj{
		Peers:        []string{"tls://a:1", "tls://b:2", "tls://c:3"},
		ProbeTimeout: 10 * time.Millisecond,
		MaxPerProto:  3,
		MinPeers:     1,
		Logger:       noopLogObj{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.watchInterval = 10 * time.Millisecond
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()
	time.Sleep(50 * time.Millisecond)

	// Drop to threshold for 1 tick
	node.mu.Lock()
	node.peers = []yggcore.PeerInfo{
		makePeerInfo("tls://a:1", true, 5*time.Millisecond),
		makePeerInfo("tls://b:2", false, 0),
		makePeerInfo("tls://c:3", false, 0),
	}
	node.mu.Unlock()
	time.Sleep(15 * time.Millisecond)

	// Recover before 3 confirmations
	node.mu.Lock()
	node.peers = []yggcore.PeerInfo{
		makePeerInfo("tls://a:1", true, 5*time.Millisecond),
		makePeerInfo("tls://b:2", true, 10*time.Millisecond),
		makePeerInfo("tls://c:3", true, 15*time.Millisecond),
	}
	node.mu.Unlock()

	node.mu.Lock()
	addedBefore := len(node.added)
	node.mu.Unlock()

	// Wait enough for would-be 3 confirmations
	time.Sleep(100 * time.Millisecond)

	node.mu.Lock()
	addedAfter := len(node.added)
	node.mu.Unlock()

	if addedAfter != addedBefore {
		t.Errorf("expected no optimize after recovery (AddPeer: before=%d, after=%d)", addedBefore, addedAfter)
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
