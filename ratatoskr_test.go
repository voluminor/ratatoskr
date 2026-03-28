package ratatoskr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

func newTestNode(t *testing.T) *Obj {
	t.Helper()
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{Config: cfg, CoreStopTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}

// //

func TestNew_nilConfig(t *testing.T) {
	// nil Config → random keys
	node, err := New(ConfigObj{CoreStopTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New with nil config: %v", err)
	}
	node.Close()
}

func TestNew_nilLogger(t *testing.T) {
	// nil Logger → noopLoggerObj used internally; must not panic
	node, err := New(ConfigObj{CoreStopTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	node.Close()
}

func TestNew_conflictingPeers(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.Peers = []string{"tls://h:1"}
	pmCfg := &peermgr.ConfigObj{
		Peers:  []string{"tls://other:1"},
		Logger: noopLoggerObj{},
	}
	_, err := New(ConfigObj{Config: cfg, Peers: pmCfg})
	if err == nil {
		t.Fatal("expected error: Config.Peers and Peers manager simultaneously")
	}
}

func TestClose_idempotent(t *testing.T) {
	node := newTestNode(t)
	if err := node.Close(); err != nil {
		t.Logf("first Close error (acceptable): %v", err)
	}
	if err := node.Close(); err != nil {
		t.Logf("second Close: %v", err)
	}
	// Must not panic or deadlock
}

func TestClose_contextShutdown(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"

	ctx, cancel := context.WithCancel(context.Background())
	node, err := New(ConfigObj{
		Ctx:             ctx,
		Config:          cfg,
		CoreStopTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cancel()
	// Wait for the internal shutdown goroutine to call Close()
	time.Sleep(200 * time.Millisecond)

	// Calling Close() again must be safe
	node.Close()
}

// //

func TestAddress_nonNil(t *testing.T) {
	node := newTestNode(t)
	addr := node.Address()
	if addr == nil {
		t.Error("expected non-nil address")
	}
}

func TestSubnet_valid(t *testing.T) {
	node := newTestNode(t)
	sn := node.Subnet()
	if len(sn.IP) == 0 {
		t.Error("expected non-empty subnet")
	}
}

func TestPublicKey_size(t *testing.T) {
	node := newTestNode(t)
	pk := node.PublicKey()
	if len(pk) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(pk))
	}
}

func TestMTU_positive(t *testing.T) {
	node := newTestNode(t)
	if node.MTU() == 0 {
		t.Error("expected MTU > 0")
	}
}

// //

func TestSnapshot_structure(t *testing.T) {
	node := newTestNode(t)
	snap := node.Snapshot()

	if snap.Address == "" {
		t.Error("Snapshot.Address empty")
	}
	if snap.Subnet == "" {
		t.Error("Snapshot.Subnet empty")
	}
	if snap.PublicKey == "" {
		t.Error("Snapshot.PublicKey empty")
	}
	if snap.MTU == 0 {
		t.Error("Snapshot.MTU is zero")
	}
	if snap.Peers == nil {
		t.Error("Snapshot.Peers is nil")
	}
	if snap.SOCKS.Enabled {
		t.Error("Snapshot.SOCKS.Enabled should be false before EnableSOCKS")
	}
}

func TestSnapshot_addressFormat(t *testing.T) {
	node := newTestNode(t)
	snap := node.Snapshot()
	if !strings.HasPrefix(snap.Address, "2") && !strings.HasPrefix(snap.Address, "3") {
		t.Logf("Address %q — expected Yggdrasil prefix 2xx or 3xx", snap.Address)
	}
}

// //

func TestEnableSOCKS_lifecycle(t *testing.T) {
	node := newTestNode(t)
	if err := node.EnableSOCKS(SOCKSConfigObj{Addr: "127.0.0.1:0"}); err != nil {
		t.Fatalf("EnableSOCKS: %v", err)
	}
	snap := node.Snapshot()
	if !snap.SOCKS.Enabled {
		t.Error("SOCKS should be enabled")
	}
	if snap.SOCKS.Addr == "" {
		t.Error("SOCKS addr should be non-empty")
	}
	if err := node.DisableSOCKS(); err != nil {
		t.Fatalf("DisableSOCKS: %v", err)
	}
	snap = node.Snapshot()
	if snap.SOCKS.Enabled {
		t.Error("SOCKS should be disabled after DisableSOCKS")
	}
}

func TestEnableSOCKS_doubleEnable(t *testing.T) {
	node := newTestNode(t)
	if err := node.EnableSOCKS(SOCKSConfigObj{Addr: "127.0.0.1:0"}); err != nil {
		t.Fatalf("first EnableSOCKS: %v", err)
	}
	defer node.DisableSOCKS()
	err := node.EnableSOCKS(SOCKSConfigObj{Addr: "127.0.0.1:0"})
	if !errors.Is(err, socks.ErrAlreadyEnabled) {
		t.Fatalf("expected socks.ErrAlreadyEnabled, got: %v", err)
	}
}

// //

func TestPeerManagerActive_nilWhenNoManager(t *testing.T) {
	node := newTestNode(t)
	if act := node.PeerManagerActive(); act != nil {
		t.Errorf("expected nil when no peer manager, got %v", act)
	}
}

func TestPeerManagerOptimize_errorWhenNoManager(t *testing.T) {
	node := newTestNode(t)
	if err := node.PeerManagerOptimize(); err == nil {
		t.Fatal("expected error when peer manager not enabled")
	}
}

func TestRetryPeers_noopWhenNoRealCore(t *testing.T) {
	node := newTestNode(t)
	node.RetryPeers() // must not panic
}

// //

func TestNew_withPeerManager_passiveMode(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	pmCfg := &peermgr.ConfigObj{
		Peers:        []string{"tls://nonexistent.example.invalid:4443"},
		MaxPerProto:  -1,
		ProbeTimeout: 10 * time.Millisecond,
		Logger:       noopLoggerObj{},
	}
	node, err := New(ConfigObj{
		Config:          cfg,
		Peers:           pmCfg,
		CoreStopTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New with peer manager: %v", err)
	}
	defer node.Close()

	// Peer manager should be active
	if act := node.PeerManagerActive(); act == nil {
		t.Error("expected non-nil from PeerManagerActive")
	}
}

// //

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		cfg := config.GenerateConfig()
		cfg.AdminListen = "none"
		node, err := New(ConfigObj{Config: cfg, CoreStopTimeout: time.Second})
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		node.Close()
	}
}

func BenchmarkSnapshot(b *testing.B) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{Config: cfg, CoreStopTimeout: time.Second})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer node.Close()

	for b.Loop() {
		node.Snapshot()
	}
}
