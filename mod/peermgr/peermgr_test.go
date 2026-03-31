package peermgr

import (
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

// //

func TestNew_valid(t *testing.T) {
	mgr, err := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	if err != nil || mgr == nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_noLogger(t *testing.T) {
	_, err := New(&mockNodeObj{}, ConfigObj{
		Peers:  []string{"tls://h:1"},
		Logger: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil logger")
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
	mgr.Start()
	mgr.Stop()
	mgr.Stop() // must not panic or block
}

func TestStop_beforeStart(t *testing.T) {
	mgr, _ := New(&mockNodeObj{}, fastCfg([]string{"tls://h:1"}))
	mgr.Stop() // must not panic
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
	mgr.Start()
	defer mgr.Stop()
	mgr.Optimize()
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
	mgr.Start()
	defer mgr.Stop()
	mgr.Optimize()

	node.mu.Lock()
	addedCount := len(node.added)
	node.mu.Unlock()
	if addedCount < len(uris) {
		t.Errorf("expected AddPeer called ≥%d times, got %d", len(uris), addedCount)
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
	mgr.Start()
	defer mgr.Stop()
	mgr.Optimize()

	select {
	case <-called:
	default:
		t.Error("OnNoReachablePeers was not called")
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
	mgr.Start()
	defer mgr.Stop()
	mgr.Optimize()
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
	mgr.Start()
	defer mgr.Stop()
	mgr.Optimize()

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
	mgr.Start()
	defer mgr.Stop()
	mgr.Optimize()
	// Just verify it completes without error; active may be empty
}

// //

func TestStop_removesActivePeers(t *testing.T) {
	node := &mockNodeObj{}
	mgr, _ := New(node, ConfigObj{
		Peers:       []string{"tls://a:1", "tcp://b:2"},
		MaxPerProto: -1,
		Logger:      noopLogObj{},
	})
	mgr.Start()
	mgr.Optimize()

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

// //

func TestNew_minPeersTooHigh(t *testing.T) {
	_, err := New(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://a:1", "tls://b:2", "tls://c:3"},
		MaxPerProto: 2,
		MinPeers:    2,
		Logger:      noopLogObj{},
	})
	if err != ErrMinPeersTooHigh {
		t.Fatalf("expected ErrMinPeersTooHigh, got %v", err)
	}
}

func TestNew_minPeersTooMany(t *testing.T) {
	_, err := New(&mockNodeObj{}, ConfigObj{
		Peers:       []string{"tls://a:1", "tls://b:2"},
		MaxPerProto: 10,
		MinPeers:    2,
		Logger:      noopLogObj{},
	})
	if err != ErrMinPeersTooMany {
		t.Fatalf("expected ErrMinPeersTooMany, got %v", err)
	}
}

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
	oldWatch := WatchInterval
	oldConfirm := MinPeersConfirmations
	WatchInterval = 10 * time.Millisecond
	MinPeersConfirmations = 3
	defer func() {
		WatchInterval = oldWatch
		MinPeersConfirmations = oldConfirm
	}()

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
	oldWatch := WatchInterval
	oldConfirm := MinPeersConfirmations
	WatchInterval = 10 * time.Millisecond
	MinPeersConfirmations = 3
	defer func() {
		WatchInterval = oldWatch
		MinPeersConfirmations = oldConfirm
	}()

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
		New(node, fastCfg(peers))
	}
}
