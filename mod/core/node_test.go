package core

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
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

func TestPublicKeyDoesNotExposeCoreStorage(t *testing.T) {
	node, err := New(ConfigObj{Logger: noopLoggerObj{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	first := node.PublicKey()
	if len(first) == 0 {
		t.Fatal("PublicKey returned an empty key")
	}
	want := append(ed25519.PublicKey(nil), first...)
	first[0] ^= 0xff
	if got := node.PublicKey(); !got.Equal(want) {
		t.Fatal("mutating the returned public key changed core state")
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
