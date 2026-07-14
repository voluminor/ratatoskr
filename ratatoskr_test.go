package ratatoskr

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/probe"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/inet"
	"github.com/voluminor/ratatoskr/mod/socks"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

var (
	_ ninfo.SourceInterface = (core.Interface)(nil)
	_ probe.SourceInterface = (core.Interface)(nil)
)

// //

func newTestNode(t *testing.T) *Obj {
	t.Helper()
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{Config: cfg, CloseTimeout: 3 * time.Second})
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

func (e errCoreObj) SetAdmin(yggcore.AddHandler) error {
	return nil
}

func (e errCoreObj) SendLookup(ed25519.PublicKey) {}

func (e errCoreObj) GetSelf() yggcore.SelfInfo {
	return yggcore.SelfInfo{}
}

func (e errCoreObj) GetSessions() []yggcore.SessionInfo {
	return nil
}

func (e errCoreObj) GetTree() []yggcore.TreeEntryInfo {
	return nil
}

func (e errCoreObj) GetPaths() []yggcore.PathEntryInfo {
	return nil
}

func (e errCoreObj) Close() error {
	return e.err
}

type blockingCoreObj struct {
	errCoreObj
	started chan struct{}
	release chan struct{}
}

type trackedConnObj struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackedConnObj) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

type trackedListenerObj struct {
	net.Listener
	closed atomic.Bool
}

func (l *trackedListenerObj) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
}

type trackedPacketConnObj struct {
	net.PacketConn
	closed atomic.Bool
}

func (c *trackedPacketConnObj) Close() error {
	c.closed.Store(true)
	return c.PacketConn.Close()
}

type lateResourceCoreObj struct {
	errCoreObj
	started  chan struct{}
	release  chan struct{}
	conn     net.Conn
	listener net.Listener
	packet   net.PacketConn
}

func (c *lateResourceCoreObj) wait() {
	close(c.started)
	<-c.release
}

func (c *lateResourceCoreObj) DialContext(context.Context, string, string) (net.Conn, error) {
	c.wait()
	return c.conn, nil
}

func (c *lateResourceCoreObj) Listen(string, string) (net.Listener, error) {
	c.wait()
	return c.listener, nil
}

func (c *lateResourceCoreObj) ListenPacket(string, string) (net.PacketConn, error) {
	c.wait()
	return c.packet, nil
}

type orderedCloseCoreObj struct {
	errCoreObj
	handlerStarted chan struct{}
	handlerRelease chan struct{}
	coreStarted    chan struct{}
}

func (o *orderedCloseCoreObj) SetAdmin(admin yggcore.AddHandler) error {
	return admin.AddHandler("getNodeInfo", "test", []string{"key"}, func(json.RawMessage) (interface{}, error) {
		close(o.handlerStarted)
		<-o.handlerRelease
		return yggcore.GetNodeInfoResponse{"node": json.RawMessage(`{"name":"test"}`)}, nil
	})
}

func (o *orderedCloseCoreObj) Close() error {
	close(o.coreStarted)
	return nil
}

func (b *blockingCoreObj) Close() error {
	close(b.started)
	<-b.release
	return nil
}

// //

func TestNew_nilConfig(t *testing.T) {
	// nil Config → random keys
	node, err := New(ConfigObj{CloseTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New with nil config: %v", err)
	}
	_ = node.Close()
}

func TestNew_rejectsCyclicNodeInfo(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.NodeInfo = make(map[string]any)
	cfg.NodeInfo["self"] = cfg.NodeInfo
	if _, err := New(ConfigObj{Config: cfg}); !errors.Is(err, ErrInvalidNodeInfo) {
		t.Fatalf("New error = %v, want ErrInvalidNodeInfo", err)
	}
}

func TestNewRejectsAnySigilAssemblyError(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	if _, err := New(ConfigObj{Config: cfg, Sigils: []sigils.Interface{nil}}); !errors.Is(err, ErrInvalidSigils) {
		t.Fatalf("New error = %v, want ErrInvalidSigils", err)
	}
}

func TestNewMapsNodeInfoParserErrorToErrInvalidSigils(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	_, err := New(ConfigObj{
		Config:   cfg,
		NodeInfo: &ninfo.ConfigObj{Sigils: []sigils.Interface{nil}},
	})
	if !errors.Is(err, ErrInvalidSigils) || !errors.Is(err, ninfo.ErrInvalidSigil) {
		t.Fatalf("New error = %v, want ErrInvalidSigils and ninfo.ErrInvalidSigil", err)
	}
}

func TestRollbackNewErrorPreservesCauseAndDeadline(t *testing.T) {
	cause := errors.New("construction failed")
	coreObj := &blockingCoreObj{started: make(chan struct{}), release: make(chan struct{})}
	started := time.Now()
	err := rollbackNewError(20*time.Millisecond, cause, nil, common.NamedCloseObj{Name: "core", Close: coreObj.Close})
	if !errors.Is(err, cause) || !errors.Is(err, ErrCloseTimedOut) {
		t.Fatalf("rollback error = %v, want cause and ErrCloseTimedOut", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("rollback exceeded deadline: %s", elapsed)
	}
	select {
	case <-coreObj.started:
	case <-time.After(time.Second):
		t.Fatal("best-effort core rollback did not start")
	}
	close(coreObj.release)
}

func TestNew_nilLogger(t *testing.T) {
	// nil Logger uses the shared discard logger internally; must not panic.
	node, err := New(ConfigObj{CloseTimeout: 3 * time.Second})
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
	_, err := New(ConfigObj{Ctx: ctx, CloseTimeout: 3 * time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
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
		Config:       cfg,
		Sigils:       []sigils.Interface{inetSigil},
		CloseTimeout: 3 * time.Second,
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
		core:     errCoreObj{err: want},
		socks:    socks.NewDisabled(),
		nodeInfo: &ninfo.Obj{},
		done:     make(chan struct{}),
	}

	if err := node.Close(); !errors.Is(err, want) {
		t.Fatalf("first Close error = %v, want %v", err, want)
	}
	if err := node.Close(); !errors.Is(err, want) {
		t.Fatalf("second Close error = %v, want %v", err, want)
	}
}

func TestNew_rejectsNegativeCloseTimeout(t *testing.T) {
	_, err := New(ConfigObj{CloseTimeout: -time.Second})
	if !errors.Is(err, ErrInvalidCloseTimeout) {
		t.Fatalf("New error = %v, want ErrInvalidCloseTimeout", err)
	}
}

func TestClose_returnsOnConfiguredDeadline(t *testing.T) {
	coreObj := &blockingCoreObj{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	node := &Obj{
		core:         coreObj,
		socks:        socks.NewDisabled(),
		nodeInfo:     &ninfo.Obj{},
		closeTimeout: 25 * time.Millisecond,
		done:         make(chan struct{}),
	}

	started := time.Now()
	err := node.Close()
	elapsed := time.Since(started)
	if !errors.Is(err, ErrCloseTimedOut) {
		t.Fatalf("Close error = %v, want ErrCloseTimedOut", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("Close exceeded bounded budget: %s", elapsed)
	}
	select {
	case <-coreObj.started:
	default:
		t.Fatal("Close did not start core teardown")
	}

	secondStarted := time.Now()
	if secondErr := node.Close(); !errors.Is(secondErr, ErrCloseTimedOut) {
		t.Fatalf("second Close error = %v, want ErrCloseTimedOut", secondErr)
	}
	if elapsed := time.Since(secondStarted); elapsed > 25*time.Millisecond {
		t.Fatalf("second Close did not return cached result: %s", elapsed)
	}
	close(coreObj.release)
}

func TestCloseStopsDependentsBeforeCore(t *testing.T) {
	coreObj := &orderedCloseCoreObj{
		handlerStarted: make(chan struct{}),
		handlerRelease: make(chan struct{}),
		coreStarted:    make(chan struct{}),
	}
	ni, err := ninfo.New(ninfo.ConfigObj{Source: coreObj, AskRetryPause: -1})
	if err != nil {
		t.Fatalf("ninfo.New: %v", err)
	}
	askDone := make(chan error, 1)
	go func() {
		_, askErr := ni.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
		askDone <- askErr
	}()
	<-coreObj.handlerStarted

	node := &Obj{
		core:         coreObj,
		socks:        socks.NewDisabled(),
		nodeInfo:     ni,
		closeTimeout: time.Second,
		done:         make(chan struct{}),
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- node.Close() }()

	select {
	case <-coreObj.coreStarted:
		close(coreObj.handlerRelease)
		t.Fatal("core teardown started before ninfo left its captured handler")
	case <-time.After(20 * time.Millisecond):
	}
	close(coreObj.handlerRelease)
	select {
	case err = <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after dependent teardown")
	}
	if askErr := <-askDone; askErr != nil {
		t.Fatalf("accepted Ask did not finish cleanly: %v", askErr)
	}
}

func TestCloseDeadlineStillStartsCoreAfterBlockedDependent(t *testing.T) {
	coreObj := &orderedCloseCoreObj{
		handlerStarted: make(chan struct{}),
		handlerRelease: make(chan struct{}),
		coreStarted:    make(chan struct{}),
	}
	ni, err := ninfo.New(ninfo.ConfigObj{Source: coreObj, AskRetryPause: -1})
	if err != nil {
		t.Fatalf("ninfo.New: %v", err)
	}
	askDone := make(chan error, 1)
	go func() {
		_, askErr := ni.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
		askDone <- askErr
	}()
	<-coreObj.handlerStarted

	node := &Obj{
		core:         coreObj,
		socks:        socks.NewDisabled(),
		nodeInfo:     ni,
		closeTimeout: 25 * time.Millisecond,
		done:         make(chan struct{}),
	}
	started := time.Now()
	err = node.Close()
	if !errors.Is(err, ErrCloseTimedOut) {
		close(coreObj.handlerRelease)
		t.Fatalf("Close error = %v, want ErrCloseTimedOut", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		close(coreObj.handlerRelease)
		t.Fatalf("Close exceeded its deadline by too much: %s", elapsed)
	}
	select {
	case <-coreObj.coreStarted:
	case <-time.After(time.Second):
		close(coreObj.handlerRelease)
		t.Fatal("best-effort core teardown did not start after the deadline")
	}

	close(coreObj.handlerRelease)
	if askErr := <-askDone; askErr != nil {
		t.Fatalf("accepted Ask did not finish cleanly after timeout: %v", askErr)
	}
}

func TestClose_contextShutdown(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"

	ctx, cancel := context.WithCancel(context.Background())
	node, err := New(ConfigObj{
		Ctx:          ctx,
		Config:       cfg,
		CloseTimeout: 3 * time.Second,
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
	if err := node.Core().RetryPeers(); err == nil {
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

func TestRootErrorMethodsAfterCloseReturnErrClosed(t *testing.T) {
	node := newTestNode(t)
	if err := node.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	checks := []struct {
		name string
		call func() error
	}{
		{name: "DialContext", call: func() error { _, err := node.DialContext(context.Background(), "tcp", "[200::1]:1"); return err }},
		{name: "Listen", call: func() error { _, err := node.Listen("tcp", ":0"); return err }},
		{name: "ListenPacket", call: func() error { _, err := node.ListenPacket("udp", ":0"); return err }},
		{name: "AddPeer", call: func() error { return node.AddPeer("tcp://127.0.0.1:1") }},
		{name: "RemovePeer", call: func() error { return node.RemovePeer("tcp://127.0.0.1:1") }},
		{name: "DisableSOCKS", call: node.DisableSOCKS},
		{name: "SetSOCKSMaxConnections", call: func() error { return node.SetSOCKSMaxConnections(1) }},
		{name: "PeerManagerOptimize", call: node.PeerManagerOptimize},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, ErrClosed) {
				t.Fatalf("error = %v, want ErrClosed", err)
			}
		})
	}
}

func assertLateResourceClosed(t *testing.T, coreObj *lateResourceCoreObj, closed *atomic.Bool, call func(*Obj) error) {
	t.Helper()
	node := &Obj{
		core:         coreObj,
		socks:        socks.NewDisabled(),
		nodeInfo:     &ninfo.Obj{},
		closeTimeout: time.Second,
		done:         make(chan struct{}),
	}
	result := make(chan error, 1)
	go func() { result <- call(node) }()
	select {
	case <-coreObj.started:
	case <-time.After(time.Second):
		t.Fatal("network operation did not reach the core")
	}
	if err := node.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(coreObj.release)
	select {
	case err := <-result:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("operation error = %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("network operation did not return")
	}
	if !closed.Load() {
		t.Fatal("resource returned after Close was not closed")
	}
}

func TestNetworkOperationsCloseLateResources(t *testing.T) {
	t.Run("DialContext", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = server.Close() }()
		tracked := &trackedConnObj{Conn: client}
		coreObj := &lateResourceCoreObj{
			started: make(chan struct{}), release: make(chan struct{}), conn: tracked,
		}
		assertLateResourceClosed(t, coreObj, &tracked.closed, func(node *Obj) error {
			_, err := node.DialContext(context.Background(), "tcp", "[200::1]:1")
			return err
		})
	})

	t.Run("Listen", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
		tracked := &trackedListenerObj{Listener: listener}
		coreObj := &lateResourceCoreObj{
			started: make(chan struct{}), release: make(chan struct{}), listener: tracked,
		}
		assertLateResourceClosed(t, coreObj, &tracked.closed, func(node *Obj) error {
			_, callErr := node.Listen("tcp", ":0")
			return callErr
		})
	})

	t.Run("ListenPacket", func(t *testing.T) {
		packet, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("ListenPacket: %v", err)
		}
		tracked := &trackedPacketConnObj{PacketConn: packet}
		coreObj := &lateResourceCoreObj{
			started: make(chan struct{}), release: make(chan struct{}), packet: tracked,
		}
		assertLateResourceClosed(t, coreObj, &tracked.closed, func(node *Obj) error {
			_, callErr := node.ListenPacket("udp", ":0")
			return callErr
		})
	})
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
		core:     coreObj,
		socks:    socks.NewDisabled(),
		nodeInfo: &ninfo.Obj{},
		done:     make(chan struct{}),
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

func TestEnableSOCKS_emptyAddr(t *testing.T) {
	node := newTestNode(t)
	err := node.EnableSOCKS(SOCKSConfigObj{})
	if !errors.Is(err, socks.ErrInvalidAddress) {
		t.Fatalf("expected socks.ErrInvalidAddress, got: %v", err)
	}
	if node.Snapshot().SOCKS.Enabled {
		t.Fatal("SOCKS should stay disabled after empty address")
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
	if err := node.EnableSOCKS(SOCKSConfigObj{
		Addr:                               "127.0.0.1:0",
		MaxAssociateTargetsPerSession:      3,
		MaxAssociateQueuedPacketsPerTarget: 5,
		MaxAssociateQueuedBytesPerTarget:   2048,
	}); err != nil {
		t.Fatalf("EnableSOCKS: %v", err)
	}
	if node.socks != socksHandle {
		t.Fatal("socks handle should stay stable after EnableSOCKS")
	}
	if got := node.socks.MaxAssociateTargetsPerSession(); got != 3 {
		t.Fatalf("SOCKS MaxAssociateTargetsPerSession = %d, want 3", got)
	}
	if got := node.socks.MaxAssociateQueuedPacketsPerTarget(); got != 5 {
		t.Fatalf("SOCKS MaxAssociateQueuedPacketsPerTarget = %d, want 5", got)
	}
	if got := node.socks.MaxAssociateQueuedBytesPerTarget(); got != 2048 {
		t.Fatalf("SOCKS MaxAssociateQueuedBytesPerTarget = %d, want 2048", got)
	}
	if err := node.SetSOCKSMaxConnections(17); err != nil {
		t.Fatalf("SetSOCKSMaxConnections: %v", err)
	}
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
	if err := node.Core().RetryPeers(); err != nil {
		t.Fatalf("RetryPeers: %v", err)
	}
}

// //

func TestNew_withPeerManager(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	pmCfg := &peermgr.ConfigObj{
		Peers:        []string{"tls://nonexistent.example.invalid:4443"},
		MaxPerProto:  1,
		ProbeTimeout: 10 * time.Millisecond,
		Logger:       common.DiscardLoggerObj{},
	}
	node, err := New(ConfigObj{
		Config:       cfg,
		Peers:        pmCfg,
		CloseTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New with peer manager: %v", err)
	}
	defer func() { _ = node.Close() }()
	if pmCfg.Node != nil {
		t.Fatal("New mutated the caller's peer manager config")
	}

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
		node, err := New(ConfigObj{Config: cfg, CloseTimeout: time.Second})
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		_ = node.Close()
	}
}

func BenchmarkSnapshot(b *testing.B) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{Config: cfg, CloseTimeout: time.Second})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer func() { _ = node.Close() }()

	for b.Loop() {
		node.Snapshot()
	}
}
