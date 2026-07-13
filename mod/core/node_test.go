package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// // // // // // // // // //

type noopLoggerObj struct{}

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

type networkDispatcherObj struct{}

func (networkDispatcherObj) DeliverNetworkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {}
func (networkDispatcherObj) DeliverLinkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer)    {}

// //

func TestNewNilLoggerDoesNotPanic(t *testing.T) {
	node, err := New(ConfigObj{})
	if err != nil {
		t.Fatalf("unexpected new node error: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })
}

func TestNewRejectsCyclicNodeInfo(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.NodeInfo = make(map[string]any)
	cfg.NodeInfo["self"] = cfg.NodeInfo
	if _, err := New(ConfigObj{Config: cfg}); !errors.Is(err, ErrInvalidNodeInfo) {
		t.Fatalf("New error = %v, want ErrInvalidNodeInfo", err)
	}
}

func TestDialContextNilContextDoesNotPanic(t *testing.T) {
	node, err := New(ConfigObj{})
	if err != nil {
		t.Fatalf("unexpected new node error: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	var nilCtx context.Context
	panicked := make(chan interface{}, 1)
	go func() {
		defer func() { panicked <- recover() }()
		// Unreachable overlay target: with the ctx normalized the dial blocks in
		// connect until the node is closed; the point is that nil ctx must not panic.
		_, _ = node.DialContext(nilCtx, "tcp", "[200::1]:1")
	}()
	time.Sleep(100 * time.Millisecond)
	_ = node.Close()
	if r := <-panicked; r != nil {
		t.Fatalf("DialContext(nil) panicked: %v", r)
	}
}

func TestNewUsesConfiguredMTU(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.IfMTU = 4096

	node, err := New(ConfigObj{Config: cfg, Logger: noopLoggerObj{}})
	if err != nil {
		t.Fatalf("unexpected new node error: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	if got := node.MTU(); got != cfg.IfMTU {
		t.Fatalf("expected MTU %d, got %d", cfg.IfMTU, got)
	}
}

func TestNewClampsLowMTU(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.IfMTU = 1

	node, err := New(ConfigObj{Config: cfg, Logger: noopLoggerObj{}})
	if err != nil {
		t.Fatalf("unexpected new node error: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	if got := node.MTU(); got != 1280 {
		t.Fatalf("expected MTU clamp to 1280, got %d", got)
	}
}

func TestNewUsesConfiguredRSTQueueSize(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"

	node, err := New(ConfigObj{Config: cfg, Logger: noopLoggerObj{}, RSTQueueSize: 7})
	if err != nil {
		t.Fatalf("unexpected new node error: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	if node.rstQueueSize != 7 {
		t.Fatalf("expected rstQueueSize 7, got %d", node.rstQueueSize)
	}
	ns := node.netstackPtr.Load()
	if ns == nil || ns.nic == nil {
		t.Fatal("expected netstack NIC")
	}
	if got := cap(ns.nic.rstPackets); got != 7 {
		t.Fatalf("expected RST queue capacity 7, got %d", got)
	}
}

func TestNewRejectsTooLargeRSTQueueSize(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"

	node, err := New(ConfigObj{Config: cfg, Logger: noopLoggerObj{}, RSTQueueSize: maxRSTQueue + 1})
	if err == nil {
		if node != nil {
			_ = node.Close()
		}
		t.Fatal("expected RST queue size error")
	}
	if !errors.Is(err, ErrRSTQueueTooLarge) {
		t.Fatalf("New error = %v, want ErrRSTQueueTooLarge", err)
	}
}

func TestBuildCoreOptionsRejectsInvalidAllowedPublicKey(t *testing.T) {
	for _, bad := range []string{strings.Repeat("00", 31), "not-hex"} {
		cfg := config.GenerateConfig()
		cfg.AllowedPublicKeys = []string{strings.Repeat("00", 32), bad}
		if _, err := buildCoreOptions(cfg); !errors.Is(err, ErrInvalidAllowedPublicKey) {
			t.Fatalf("AllowedPublicKey %q: expected ErrInvalidAllowedPublicKey, got %v", bad, err)
		}
	}
}

func TestBuildCoreOptionsAcceptsValidAllowedPublicKey(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AllowedPublicKeys = []string{strings.Repeat("00", 32)}
	opts, err := buildCoreOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allowed := 0
	for _, opt := range opts {
		if _, ok := opt.(yggcore.AllowedPublicKey); ok {
			allowed++
		}
	}
	if allowed != 1 {
		t.Fatalf("expected exactly one valid AllowedPublicKey option, got %d", allowed)
	}
}

func TestParseAddressRequiresExplicitPort(t *testing.T) {
	_, err := parseAddress("[200::1]:")
	if !errors.Is(err, ErrPortRequired) {
		t.Fatalf("expected ErrPortRequired, got %v", err)
	}
}

func TestParseAddressRejectsIPv4Literal(t *testing.T) {
	_, err := parseAddress("127.0.0.1:80")
	if !errors.Is(err, ErrIPv6Only) {
		t.Fatalf("expected ErrIPv6Only, got %v", err)
	}
}

func TestDisableAdminReturnsStopError(t *testing.T) {
	stopErr := errors.New("stop admin")
	node := &Obj{logger: noopLoggerObj{}}
	node.adminSocket.name = "admin"
	node.adminSocket.active = true
	node.adminSocket.stopFn = func() error { return stopErr }

	err := node.DisableAdmin()
	if !errors.Is(err, stopErr) {
		t.Fatalf("expected stop error, got %v", err)
	}
	if _, active := node.adminSocket.get(); active {
		t.Fatal("admin socket component should be inactive after failed admin stop")
	}
}

// //

func TestObj_CloseDoesNotReenterNIC(t *testing.T) {
	node, err := New(ConfigObj{Logger: noopLoggerObj{}})
	if err != nil {
		t.Fatalf("unexpected new node error: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- node.Close()
	}()

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("unexpected close error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close timed out; possible NIC close re-entry")
	}

	if err = node.Close(); err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}
}

func TestNICAttachNilDetaches(t *testing.T) {
	nic := &nicObj{}
	nic.Attach(networkDispatcherObj{})
	if !nic.IsAttached() {
		t.Fatal("expected NIC to be attached")
	}
	nic.Attach(nil)
	if nic.IsAttached() {
		t.Fatal("Attach(nil) should detach the NIC")
	}
}

func newTestPacketBufferObj() *stack.PacketBuffer {
	return stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData([]byte{1}),
	})
}

func TestNICEnqueueRSTDropsWhenFull(t *testing.T) {
	nic := &nicObj{
		rstPackets: make(chan *stack.PacketBuffer, 1),
		done:       make(chan struct{}),
	}
	first := newTestPacketBufferObj()
	second := newTestPacketBufferObj()
	nic.enqueueRST(first)
	nic.enqueueRST(second)

	if got := len(nic.rstPackets); got != 1 {
		t.Fatalf("expected queue length 1, got %d", got)
	}
	if got := nic.rstDropped.Load(); got != 1 {
		t.Fatalf("expected one dropped RST packet, got %d", got)
	}
	(<-nic.rstPackets).DecRef()
}

func TestNICEnqueueRSTDropsAfterClose(t *testing.T) {
	nic := &nicObj{
		rstPackets: make(chan *stack.PacketBuffer, 1),
		rstClosed:  true,
		done:       make(chan struct{}),
	}
	nic.enqueueRST(newTestPacketBufferObj())
	if got := len(nic.rstPackets); got != 0 {
		t.Fatalf("expected closed RST queue to stay empty, got %d", got)
	}
	if got := nic.rstDropped.Load(); got != 1 {
		t.Fatalf("expected one dropped RST packet, got %d", got)
	}
}
