package socks

import (
	"context"
	"net"
	"os"
	"runtime"
	"testing"
)

// // // // // // // // // //

// noopLogObj — yggcore.Logger that discards all messages
type noopLogObj struct{}

func (noopLogObj) Printf(string, ...interface{}) {}
func (noopLogObj) Println(...interface{})        {}
func (noopLogObj) Infof(string, ...interface{})  {}
func (noopLogObj) Infoln(...interface{})         {}
func (noopLogObj) Warnf(string, ...interface{})  {}
func (noopLogObj) Warnln(...interface{})         {}
func (noopLogObj) Errorf(string, ...interface{}) {}
func (noopLogObj) Errorln(...interface{})        {}
func (noopLogObj) Debugf(string, ...interface{}) {}
func (noopLogObj) Debugln(...interface{})        {}
func (noopLogObj) Traceln(...interface{})        {}

// //

// mockDialerObj — proxy.ContextDialer backed by real TCP
type mockDialerObj struct{}

func (mockDialerObj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// //

func newSocks() *Obj { return New(mockDialerObj{}) }

func tcpCfg() EnableConfigObj {
	return EnableConfigObj{Addr: "127.0.0.1:0", Logger: noopLogObj{}}
}

// //

func TestEnable_TCP(t *testing.T) {
	s := newSocks()
	if err := s.Enable(tcpCfg()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	defer s.Disable()
	if !s.IsEnabled() {
		t.Error("expected IsEnabled=true")
	}
	if s.Addr() == "" {
		t.Error("expected non-empty Addr()")
	}
	if s.IsUnix() {
		t.Error("expected IsUnix=false for TCP")
	}
}

func TestEnable_twice(t *testing.T) {
	s := newSocks()
	if err := s.Enable(tcpCfg()); err != nil {
		t.Fatalf("first Enable: %v", err)
	}
	defer s.Disable()
	if err := s.Enable(tcpCfg()); err == nil {
		t.Fatal("expected error on double Enable")
	}
}

func TestDisable_whenNotEnabled(t *testing.T) {
	s := newSocks()
	if err := s.Disable(); err != nil {
		t.Fatalf("Disable on inactive: %v", err)
	}
}

func TestDisable_clearsState(t *testing.T) {
	s := newSocks()
	s.Enable(tcpCfg())
	s.Disable()
	if s.IsEnabled() {
		t.Error("expected IsEnabled=false after Disable")
	}
	if s.Addr() != "" {
		t.Errorf("expected empty Addr() after Disable, got %q", s.Addr())
	}
}

func TestEnableDisableEnable(t *testing.T) {
	s := newSocks()
	if err := s.Enable(tcpCfg()); err != nil {
		t.Fatalf("first Enable: %v", err)
	}
	s.Disable()
	if err := s.Enable(tcpCfg()); err != nil {
		t.Fatalf("second Enable: %v", err)
	}
	s.Disable()
}

func TestEnable_invalidAddr(t *testing.T) {
	s := newSocks()
	if err := s.Enable(EnableConfigObj{Addr: "not-valid-address", Logger: noopLogObj{}}); err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestEnable_maxConnections(t *testing.T) {
	s := newSocks()
	cfg := tcpCfg()
	cfg.MaxConnections = 5
	if err := s.Enable(cfg); err != nil {
		t.Fatalf("Enable with MaxConnections: %v", err)
	}
	defer s.Disable()
	if !s.IsEnabled() {
		t.Error("expected IsEnabled=true")
	}
}

// //

func TestEnable_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	s := newSocks()
	path := t.TempDir() + "/test.sock"
	if err := s.Enable(EnableConfigObj{Addr: path, Logger: noopLogObj{}}); err != nil {
		t.Fatalf("Enable Unix: %v", err)
	}
	defer s.Disable()
	if !s.IsUnix() {
		t.Error("expected IsUnix=true")
	}
	if s.Addr() != path {
		t.Errorf("expected Addr=%q, got %q", path, s.Addr())
	}
}

func TestListenUnix_staleSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	path := t.TempDir() + "/stale.sock"

	// Create and immediately close a listener → stale socket file remains
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	ln.Close()

	// listenUnix should detect the stale socket, remove it, and re-bind
	ln2, err := listenUnix(path)
	if err != nil {
		t.Fatalf("listenUnix stale: %v", err)
	}
	ln2.Close()
}

func TestListenUnix_activeSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	path := t.TempDir() + "/active.sock"

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("create active socket: %v", err)
	}
	defer ln.Close()

	_, err = listenUnix(path)
	if err == nil {
		t.Fatal("expected error: another instance is listening")
	}
}

func TestRemoveUnixSocket_regular(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	path := t.TempDir() + "/regular.sock"

	// Create a plain file; removeUnixSocket only checks it's not a symlink
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	f.Close()

	if err := removeUnixSocket(path); err != nil {
		t.Fatalf("removeUnixSocket: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Error("file should be removed after removeUnixSocket")
	}
}

func TestIsAddrInUse(t *testing.T) {
	// Bind a port and try to bind again to trigger EADDRINUSE
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, err = net.Listen("tcp", ln.Addr().String())
	if err == nil {
		t.Skip("expected EADDRINUSE but got nil error")
	}
	// Just verify it doesn't panic and returns a bool
	_ = isAddrInUse(err)
}

// //

func TestLimitedListener_semaphoreAcquired(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()

	limited := &limitedListenerObj{
		Listener: inner,
		sem:      make(chan struct{}, 3),
	}
	addr := inner.Addr().String()

	done := make(chan net.Conn, 1)
	go func() {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			done <- c
		}
	}()

	conn, err := limited.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()

	if c := <-done; c != nil {
		c.Close()
	}

	if len(limited.sem) != 1 {
		t.Errorf("expected 1 semaphore slot used, got %d", len(limited.sem))
	}
}

func TestLimitedConn_releasesSemaphoreOnClose(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()

	go func() {
		c, _ := net.Dial("tcp", inner.Addr().String())
		if c != nil {
			c.Close()
		}
	}()

	ac, err := inner.Accept()
	if err != nil {
		t.Fatal(err)
	}

	// sem <- struct{}{} simulates what Accept() does when a connection is made
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	lc := &limitedConnObj{Conn: ac, sem: sem}
	lc.Close()

	// Close() drains one slot from the channel; sem should now be empty
	if len(sem) != 0 {
		t.Errorf("expected empty sem after Close, got %d", len(sem))
	}
}

func TestLimitedConn_closeOnce(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()

	go func() {
		c, _ := net.Dial("tcp", inner.Addr().String())
		if c != nil {
			c.Close()
		}
	}()
	ac, _ := inner.Accept()

	sem := make(chan struct{}, 2)
	sem <- struct{}{}
	lc := &limitedConnObj{Conn: ac, sem: sem}

	lc.Close()
	lc.Close()
	lc.Close()

	if len(sem) != 0 {
		t.Errorf("expected empty sem after Close, got %d", len(sem))
	}
}

// //

func BenchmarkEnableDisable(b *testing.B) {
	for b.Loop() {
		s := newSocks()
		s.Enable(tcpCfg())
		s.Disable()
	}
}
