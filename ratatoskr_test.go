package ratatoskr

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/inet"
	"github.com/voluminor/ratatoskr/mod/socks"
	"github.com/voluminor/ratatoskr/target"
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
	t.Cleanup(func() { _ = node.Close() })
	return node
}

// //

type errCoreObj struct {
	err error
}

func (e errCoreObj) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func (e errCoreObj) Listen(string, string) (net.Listener, error) {
	return nil, nil
}

func (e errCoreObj) ListenPacket(string, string) (net.PacketConn, error) {
	return nil, nil
}

func (e errCoreObj) Address() net.IP {
	return nil
}

func (e errCoreObj) Subnet() net.IPNet {
	return net.IPNet{}
}

func (e errCoreObj) PublicKey() ed25519.PublicKey {
	return nil
}

func (e errCoreObj) MTU() uint64 {
	return 0
}

func (e errCoreObj) AddPeer(string) error {
	return nil
}

func (e errCoreObj) RemovePeer(string) error {
	return nil
}

func (e errCoreObj) GetPeers() []yggcore.PeerInfo {
	return nil
}

func (e errCoreObj) RetryPeers() error {
	return e.err
}

func (e errCoreObj) EnableMulticast() error {
	return nil
}

func (e errCoreObj) DisableMulticast() error {
	return nil
}

func (e errCoreObj) EnableAdmin(string) error {
	return nil
}

func (e errCoreObj) DisableAdmin() error {
	return nil
}

func (e errCoreObj) RSTDropped() uint64 {
	return 0
}

func (e errCoreObj) Close() error {
	return e.err
}

type blockingCoreObj struct {
	errCoreObj
	started chan struct{}
	release chan struct{}
}

func (b *blockingCoreObj) Close() error {
	close(b.started)
	<-b.release
	return nil
}

type noopSocksObj struct{}

func (noopSocksObj) Close() error      { return nil }
func (noopSocksObj) Addr() string      { return "" }
func (noopSocksObj) IsUnix() bool      { return false }
func (noopSocksObj) IsEnabled() bool   { return false }
func (noopSocksObj) ServeError() error { return nil }
func (noopSocksObj) MaxConnections() int {
	return 0
}
func (noopSocksObj) SetMaxConnections(int)  {}
func (noopSocksObj) ActiveConnections() int { return 0 }

// //

func TestNew_nilConfig(t *testing.T) {
	// nil Config → random keys
	node, err := New(ConfigObj{CoreStopTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New with nil config: %v", err)
	}
	_ = node.Close()
}

func TestNew_nilLogger(t *testing.T) {
	// nil Logger uses the shared discard logger internally; must not panic.
	node, err := New(ConfigObj{CoreStopTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = node.Close()
}

func TestNew_conflictingPeers(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.Peers = []string{"tls://h:1"}
	pmCfg := &peermgr.ConfigObj{
		Peers:  []string{"tls://other:1"},
		Logger: common.DiscardLoggerObj{},
	}
	_, err := New(ConfigObj{Config: cfg, Peers: pmCfg})
	if err == nil {
		t.Fatal("expected error: Config.Peers and Peers manager simultaneously")
	}
}

func TestNew_canceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(ConfigObj{Ctx: ctx, CoreStopTimeout: 3 * time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestNew_acceptsRSTQueueSize(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{
		Config:          cfg,
		RSTQueueSize:    1,
		CoreStopTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = node.Close()
}

func TestNew_doesNotMutateConfigNodeInfo(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.NodeInfo = map[string]any{"custom": "value"}
	inetSigil, err := inet.New([]string{"example.org"})
	if err != nil {
		t.Fatalf("inet.New: %v", err)
	}

	node, err := New(ConfigObj{
		Config:          cfg,
		Sigils:          []sigils.Interface{inetSigil},
		CoreStopTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = node.Close() }()

	if cfg.NodeInfo["custom"] != "value" {
		t.Fatal("base NodeInfo value changed")
	}
	if _, ok := cfg.NodeInfo[target.Name]; ok {
		t.Fatal("ratatoskr metadata leaked into caller config")
	}
	if _, ok := cfg.NodeInfo[inet.Name()]; ok {
		t.Fatal("sigil params leaked into caller config")
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

func TestClose_idempotentPreservesError(t *testing.T) {
	want := errors.New("close failed")
	node := &Obj{
		Interface: errCoreObj{err: want},
		socks:     noopSocksObj{},
		nodeInfo:  &ninfo.Obj{},
		done:      make(chan struct{}),
	}

	if err := node.Close(); !errors.Is(err, want) {
		t.Fatalf("first Close error = %v, want %v", err, want)
	}
	if err := node.Close(); !errors.Is(err, want) {
		t.Fatalf("second Close error = %v, want %v", err, want)
	}
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
	_ = node.Close()
}

func TestRetryPeers_afterCloseReturnsError(t *testing.T) {
	node := newTestNode(t)
	if err := node.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := node.RetryPeers(); err == nil {
		t.Fatal("expected error after close")
	}
}

func TestEnableSOCKS_afterCloseReturnsErrClosed(t *testing.T) {
	node := newTestNode(t)
	if err := node.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := node.EnableSOCKS(SOCKSConfigObj{Addr: "127.0.0.1:0"})
	if err == nil {
		_ = node.DisableSOCKS()
		t.Fatal("expected ErrClosed")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestAsk_afterCloseReturnsErrClosed(t *testing.T) {
	node := newTestNode(t)
	if err := node.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := node.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestAskAddr_afterCloseReturnsErrClosed(t *testing.T) {
	node := newTestNode(t)
	if err := node.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := node.AskAddr(context.Background(), "200::1")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
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

func TestSnapshot_doesNotBlockDuringClose(t *testing.T) {
	coreObj := &blockingCoreObj{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	node := &Obj{
		Interface: coreObj,
		socks:     noopSocksObj{},
		nodeInfo:  &ninfo.Obj{},
		done:      make(chan struct{}),
	}

	closeDone := make(chan struct{})
	go func() {
		_ = node.Close()
		close(closeDone)
	}()

	select {
	case <-coreObj.started:
	case <-time.After(time.Second):
		t.Fatal("Close did not reach blocking core")
	}

	snapshotDone := make(chan struct{})
	go func() {
		_ = node.Snapshot()
		close(snapshotDone)
	}()

	select {
	case <-snapshotDone:
	case <-time.After(100 * time.Millisecond):
		close(coreObj.release)
		t.Fatal("Snapshot blocked behind Close")
	}

	close(coreObj.release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after release")
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
	defer func() { _ = node.DisableSOCKS() }()
	err := node.EnableSOCKS(SOCKSConfigObj{Addr: "127.0.0.1:0"})
	if !errors.Is(err, socks.ErrAlreadyEnabled) {
		t.Fatalf("expected socks.ErrAlreadyEnabled, got: %v", err)
	}
}

func TestModuleHandles(t *testing.T) {
	node := newTestNode(t)
	if node.nodeInfo == nil {
		t.Fatal("nodeInfo handle is nil")
	}
	if node.socks == nil {
		t.Fatal("socks handle is nil")
	}
	if node.socks.IsEnabled() {
		t.Fatal("socks handle should be disabled before EnableSOCKS")
	}
	socksHandle := node.socks
	if err := node.EnableSOCKS(SOCKSConfigObj{Addr: "127.0.0.1:0"}); err != nil {
		t.Fatalf("EnableSOCKS: %v", err)
	}
	if node.socks != socksHandle {
		t.Fatal("socks handle should stay stable after EnableSOCKS")
	}
	node.SetSOCKSMaxConnections(17)
	if got := node.SOCKSMaxConnections(); got != 17 {
		t.Fatalf("SOCKS MaxConnections = %d, want 17", got)
	}
	if err := node.DisableSOCKS(); err != nil {
		t.Fatalf("DisableSOCKS: %v", err)
	}
	if node.socks != socksHandle {
		t.Fatal("socks handle should stay stable after DisableSOCKS")
	}
	if node.socks.IsEnabled() {
		t.Fatal("socks handle should be disabled after DisableSOCKS")
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

func TestRetryPeers_onRunningNode(t *testing.T) {
	node := newTestNode(t)
	if err := node.RetryPeers(); err != nil {
		t.Fatalf("RetryPeers: %v", err)
	}
}

// //

func TestNew_withPeerManager_passiveMode(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	pmCfg := &peermgr.ConfigObj{
		Peers:        []string{"tls://nonexistent.example.invalid:4443"},
		MaxPerProto:  -1,
		ProbeTimeout: 10 * time.Millisecond,
		Logger:       common.DiscardLoggerObj{},
	}
	node, err := New(ConfigObj{
		Config:          cfg,
		Peers:           pmCfg,
		CoreStopTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New with peer manager: %v", err)
	}
	defer func() { _ = node.Close() }()

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
		_ = node.Close()
	}
}

func BenchmarkSnapshot(b *testing.B) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{Config: cfg, CoreStopTimeout: time.Second})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer func() { _ = node.Close() }()

	for b.Loop() {
		node.Snapshot()
	}
}
