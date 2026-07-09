package socks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/things-go/go-socks5/statute"
	"github.com/voluminor/ratatoskr/internal/common"
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

type blockingDialerObj struct {
	started chan struct{}
	once    sync.Once
}

func (d *blockingDialerObj) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	d.once.Do(func() {
		close(d.started)
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

// gateDialerObj signals when a dial starts, then blocks until released and
// returns a preset conn. It lets a test wedge close() between dial and insert.
type gateDialerObj struct {
	started chan struct{}
	release chan struct{}
	conn    net.Conn
}

func (d *gateDialerObj) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	close(d.started)
	<-d.release
	return d.conn, nil
}

// closeTrackConnObj records whether Close was called.
type closeTrackConnObj struct {
	closed atomic.Bool
}

func (c *closeTrackConnObj) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *closeTrackConnObj) Write(p []byte) (int, error)      { return len(p), nil }
func (c *closeTrackConnObj) Close() error                     { c.closed.Store(true); return nil }
func (c *closeTrackConnObj) LocalAddr() net.Addr              { return nil }
func (c *closeTrackConnObj) RemoteAddr() net.Addr             { return nil }
func (c *closeTrackConnObj) SetDeadline(time.Time) error      { return nil }
func (c *closeTrackConnObj) SetReadDeadline(time.Time) error  { return nil }
func (c *closeTrackConnObj) SetWriteDeadline(time.Time) error { return nil }

type staticResolverObj struct {
	name  string
	ip    net.IP
	calls atomic.Int64
}

func (r *staticResolverObj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if name != r.name {
		return ctx, nil, errors.New("unexpected resolver name")
	}
	r.calls.Add(1)
	return ctx, append(net.IP(nil), r.ip...), nil
}

type credentialsObj struct{}

func (credentialsObj) Valid(user, password, _ string) bool {
	return user == "user" && password == "pass"
}

type deadlineRecorderConnObj struct {
	mu            sync.Mutex
	deadlines     int
	readDeadlines int
	lastDeadline  time.Time
}

func (c *deadlineRecorderConnObj) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *deadlineRecorderConnObj) Write(p []byte) (int, error)      { return len(p), nil }
func (c *deadlineRecorderConnObj) Close() error                     { return nil }
func (c *deadlineRecorderConnObj) LocalAddr() net.Addr              { return nil }
func (c *deadlineRecorderConnObj) RemoteAddr() net.Addr             { return nil }
func (c *deadlineRecorderConnObj) SetWriteDeadline(time.Time) error { return nil }
func (c *deadlineRecorderConnObj) SetReadDeadline(time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadlines++
	return nil
}
func (c *deadlineRecorderConnObj) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlines++
	c.lastDeadline = deadline
	return nil
}

func (c *deadlineRecorderConnObj) snapshotDeadline() (int, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadlines, c.lastDeadline
}

type temporaryAcceptListenerObj struct {
	attempts atomic.Int64
	conn     net.Conn
	closed   chan struct{}
}

func (l *temporaryAcceptListenerObj) Accept() (net.Conn, error) {
	if l.attempts.Add(1) == 1 {
		return nil, &net.OpError{
			Op:  "accept",
			Net: "tcp",
			Err: &os.SyscallError{Syscall: "accept", Err: syscall.EMFILE},
		}
	}
	return l.conn, nil
}

func (l *temporaryAcceptListenerObj) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *temporaryAcceptListenerObj) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

// //

func newSocks(t *testing.T, cfg ConfigObj) *Obj {
	t.Helper()
	if cfg.Network == nil {
		cfg.Network = mockDialerObj{}
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func tcpCfg() ConfigObj {
	return ConfigObj{Network: mockDialerObj{}, Addr: "127.0.0.1:0", Logger: noopLogObj{}}
}

func tcpCfgOnFreePort(t *testing.T) ConfigObj {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err = ln.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := tcpCfg()
	cfg.Addr = addr
	cfg.HandshakeTimeout = time.Hour
	return cfg
}

func waitActiveConns(t *testing.T, s *Obj, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ln, _ := s.listener.(*limitedListenerObj)
		s.mu.Unlock()
		got := 0
		if ln != nil {
			ln.mu.Lock()
			got = len(ln.conns)
			ln.mu.Unlock()
		}
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d active connections", want)
}

func newTestLimitedListener(inner net.Listener, maxConnections int, handshakeTimeout, tunnelIdleTimeout time.Duration) *limitedListenerObj {
	return newLimitedListener(inner, common.NewDynamicLimit(maxConnections), handshakeTimeout, tunnelIdleTimeout)
}

func readFull(t *testing.T, conn net.Conn, buf []byte) {
	t.Helper()
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
}

func sendNoAuthGreeting(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	readFull(t, conn, resp)
	return resp
}

func sendIPv6ConnectRequest(t *testing.T, conn net.Conn, ip string, port byte) []byte {
	t.Helper()
	req := []byte{0x05, 0x01, 0x00, 0x04}
	req = append(req, net.ParseIP(ip).To16()...)
	req = append(req, 0x00, port)
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 10)
	readFull(t, conn, resp)
	return resp
}

func sendIPv4ConnectRequest(t *testing.T, conn net.Conn, ip string, port int) []byte {
	t.Helper()
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, net.ParseIP(ip).To4()...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 10)
	readFull(t, conn, resp)
	return resp
}

func sendIPv4AssociateRequest(t *testing.T, conn net.Conn, ip string, port int) []byte {
	t.Helper()
	req := []byte{0x05, 0x03, 0x00, 0x01}
	req = append(req, net.ParseIP(ip).To4()...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 10)
	readFull(t, conn, resp)
	return resp
}

func udpEchoServer(t *testing.T) net.PacketConn {
	t.Helper()
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		buf := make([]byte, 1024)
		for {
			n, addr, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(buf[:n], addr)
		}
	}()
	return echo
}

func associateRelay(t *testing.T, cfg ConfigObj, ip string, port int) (*Obj, net.Conn, *net.UDPAddr) {
	t.Helper()
	s := newSocks(t, cfg)
	t.Cleanup(func() { _ = s.Close() })

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	resp := sendNoAuthGreeting(t, conn)
	if resp[1] != 0x00 {
		t.Fatalf("expected no-auth accepted, got %v", resp)
	}
	resp = sendIPv4AssociateRequest(t, conn, ip, port)
	if resp[1] != 0x00 {
		t.Fatalf("expected associate success reply, got %v", resp)
	}
	reply, err := statute.ParseReply(bytes.NewReader(resp))
	if err != nil {
		t.Fatalf("ParseReply: %v", err)
	}
	relay, err := net.ResolveUDPAddr("udp", reply.BndAddr.String())
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	if relay.IP.IsUnspecified() {
		relay.IP = net.IPv4(127, 0, 0, 1)
	}
	return s, conn, relay
}

func sendSocksUDP(t *testing.T, conn net.PacketConn, relay net.Addr, target string, payload []byte) []byte {
	t.Helper()
	packet, err := statute.NewDatagram(target, payload)
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	if _, err = conn.WriteTo(packet.Bytes(), relay); err != nil {
		t.Fatalf("UDP relay write: %v", err)
	}
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("UDP relay read: %v", err)
	}
	got, err := statute.ParseDatagram(buf[:n])
	if err != nil {
		t.Fatalf("ParseDatagram: %v", err)
	}
	return got.Data
}

func silentTCPServer(t *testing.T) *net.TCPAddr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr)
}

// //

func TestNew_TCP(t *testing.T) {
	s := newSocks(t, tcpCfg())
	defer func() { _ = s.Close() }()
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

func TestNewDisabled_Start(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	s := NewDisabled()
	if s.IsEnabled() {
		t.Fatal("disabled handle should not start a listener")
	}
	if err := s.Start(cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !s.IsEnabled() {
		t.Fatal("Start should enable the listener")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNew_nilLoggerDoesNotPanic(t *testing.T) {
	s := newSocks(t, ConfigObj{Network: mockDialerObj{}, Addr: "127.0.0.1:0"})
	defer func() { _ = s.Close() }()
}

func TestNew_addressAlreadyInUse(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	s := newSocks(t, cfg)
	defer func() { _ = s.Close() }()
	if _, err := New(cfg); err == nil {
		t.Fatal("expected error when address is already in use")
	}
}

func TestClose_whenNotEnabled(t *testing.T) {
	s := &Obj{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close on inactive: %v", err)
	}
}

func TestClose_clearsState(t *testing.T) {
	s := newSocks(t, tcpCfg())
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if s.IsEnabled() {
		t.Error("expected IsEnabled=false after Close")
	}
	if s.Addr() != "" {
		t.Errorf("expected empty Addr() after Close, got %q", s.Addr())
	}
}

func TestNewCloseNew(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	s := newSocks(t, cfg)
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	s = newSocks(t, cfg)
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestNew_invalidAddr(t *testing.T) {
	if _, err := New(ConfigObj{Network: mockDialerObj{}, Addr: "not-valid-address", Logger: noopLogObj{}}); err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestNew_failedUnixReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	if _, err := New(ConfigObj{Network: mockDialerObj{}, Addr: t.TempDir() + "/missing/test.sock", Logger: noopLogObj{}}); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestNew_maxConnections(t *testing.T) {
	cfg := tcpCfg()
	cfg.MaxConnections = 5
	s := newSocks(t, cfg)
	defer func() { _ = s.Close() }()
	if !s.IsEnabled() {
		t.Error("expected IsEnabled=true")
	}
}

func TestNew_defaultMaxConnections(t *testing.T) {
	s := newSocks(t, tcpCfg())
	defer func() { _ = s.Close() }()

	ln, ok := s.listener.(*limitedListenerObj)
	if !ok {
		t.Fatal("expected limitedListenerObj")
	}
	if ln.limit.Limit() != defaultMaxConnections {
		t.Fatalf("expected default max connections %d, got %d", defaultMaxConnections, ln.limit.Limit())
	}
	if got := s.TunnelIdleTimeout(); got != defaultTunnelIdleTime {
		t.Fatalf("expected default tunnel idle timeout %s, got %s", defaultTunnelIdleTime, got)
	}
}

func TestClose_unblocksWhenConnectionLimitFull(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	cfg.MaxConnections = 1
	s := newSocks(t, cfg)

	c1, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c1.Close() }()
	waitActiveConns(t, s, 1)

	c2, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	time.Sleep(50 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- s.Close() }()

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close stayed blocked after listener close")
	}
}

func TestClose_closesIdleConnections(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	s := newSocks(t, cfg)

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	waitActiveConns(t, s, 1)

	if err = s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err = conn.Read(buf); err == nil {
		t.Fatal("expected idle connection to be closed")
	}
}

func TestHandshakeTimeout_closesSilentClient(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	cfg.HandshakeTimeout = 50 * time.Millisecond
	s := newSocks(t, cfg)
	defer func() { _ = s.Close() }()

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	waitActiveConns(t, s, 1)

	if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err = conn.Read(buf); err == nil {
		t.Fatal("expected silent client to be closed by handshake timeout")
	}
	waitActiveConns(t, s, 0)
}

func TestDialTimeout_boundsConnect(t *testing.T) {
	dialer := &blockingDialerObj{started: make(chan struct{})}
	cfg := tcpCfgOnFreePort(t)
	cfg.Network = dialer
	cfg.DialTimeout = 50 * time.Millisecond
	cfg.HandshakeTimeout = 10 * time.Millisecond
	s := newSocks(t, cfg)
	defer func() { _ = s.Close() }()

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if resp := sendNoAuthGreeting(t, conn); resp[1] != 0x00 {
		t.Fatalf("expected no-auth accepted, got %v", resp)
	}
	if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp := sendIPv6ConnectRequest(t, conn, "200::1", 80)
	if resp[1] == 0x00 {
		t.Fatalf("expected CONNECT failure after dial timeout, got %v", resp)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("CONNECT was not bounded by DialTimeout, elapsed=%s", elapsed)
	}
	select {
	case <-dialer.started:
	default:
		t.Fatal("dialer was not called")
	}
}

func TestTunnelIdleTimeout_closesIdleTunnel(t *testing.T) {
	target := silentTCPServer(t)
	cfg := tcpCfgOnFreePort(t)
	cfg.HandshakeTimeout = time.Second
	cfg.TunnelIdleTimeout = 50 * time.Millisecond
	s := newSocks(t, cfg)
	defer func() { _ = s.Close() }()

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if resp := sendNoAuthGreeting(t, conn); resp[1] != 0x00 {
		t.Fatalf("expected no-auth accepted, got %v", resp)
	}
	if resp := sendIPv4ConnectRequest(t, conn, "127.0.0.1", target.Port); resp[1] != 0x00 {
		t.Fatalf("expected CONNECT success, got %v", resp)
	}
	if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err = conn.Read(buf); err == nil {
		t.Fatal("expected idle tunnel to be closed")
	}
	waitActiveConns(t, s, 0)
}

func TestTunnelIdleTimeout_disabledClearsStaleDeadline(t *testing.T) {
	conn := &limitedConnObj{
		Conn:              &deadlineRecorderConnObj{},
		tunnelIdleTimeout: 0,
	}
	conn.tunnelDeadline.Store(time.Now().Add(time.Minute).UnixNano())

	conn.refreshActivityDeadline()

	recorder := conn.Conn.(*deadlineRecorderConnObj)
	deadlines, lastDeadline := recorder.snapshotDeadline()
	if deadlines != 1 {
		t.Fatalf("expected stale deadline to be cleared once, got %d clears", deadlines)
	}
	if !lastDeadline.IsZero() {
		t.Fatalf("expected zero deadline, got %s", lastDeadline)
	}
	if got := conn.tunnelDeadline.Load(); got != 0 {
		t.Fatalf("expected tunnel deadline state to be reset, got %d", got)
	}
}

func TestServeErrorClearsEnabledState(t *testing.T) {
	s := &Obj{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	s.mu.Lock()
	s.listener = ln
	s.addr = addr
	s.logger = noopLogObj{}
	s.mu.Unlock()

	boom := errors.New("accept failed")
	s.finishServe(ln, boom)
	if s.IsEnabled() {
		t.Fatal("server should not stay enabled after serve error")
	}

	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("listener should be closed after serve error: %v", err)
	}
	_ = rebound.Close()
}

func TestNew_credentialsRejectNoAuth(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	cfg.Credentials = credentialsObj{}
	s := newSocks(t, cfg)
	defer func() { _ = s.Close() }()

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	resp := sendNoAuthGreeting(t, conn)
	if resp[1] != 0xff {
		t.Fatalf("expected no acceptable auth methods, got %v", resp)
	}
}

func TestAssociate_udpEcho(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp")
	got := sendSocksUDP(t, udpConn, relay, echo.LocalAddr().String(), payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected UDP echo payload %q", got)
	}
}

func TestAssociate_usesResolverForDatagramDomain(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &staticResolverObj{name: "udp-target.pk.ygg", ip: net.IPv4(127, 0, 0, 1)}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp-domain")
	target := net.JoinHostPort("udp-target.pk.ygg", strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port))
	got := sendSocksUDP(t, udpConn, relay, target, payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected UDP echo payload %q", got)
	}
	if resolver.calls.Load() == 0 {
		t.Fatal("resolver was not used for UDP datagram domain")
	}
}

func TestAssociate_pinsFirstUDPClientWhenRequestUnspecified(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	first, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	_ = first.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp-first")
	got := sendSocksUDP(t, first, relay, echo.LocalAddr().String(), payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected first UDP echo payload %q", got)
	}

	second, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	secondPayload := []byte("ratatoskr-socks-udp-second")
	packet, err := statute.NewDatagram(echo.LocalAddr().String(), secondPayload)
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	if _, err = second.WriteTo(packet.Bytes(), relay); err != nil {
		t.Fatalf("second UDP relay write: %v", err)
	}

	buf := make([]byte, 1024)
	_ = second.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if n, _, err := second.ReadFrom(buf); err == nil {
		t.Fatalf("second UDP client received unexpected packet %x", buf[:n])
	}
	_ = first.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if n, _, err := first.ReadFrom(buf); err == nil {
		got, parseErr := statute.ParseDatagram(buf[:n])
		if parseErr != nil || bytes.Equal(got.Data, secondPayload) {
			t.Fatalf("first UDP client received packet from rejected source: data=%x parseErr=%v", buf[:n], parseErr)
		}
	}
}

func TestAssociate_controlCloseCancelsPendingUDPDial(t *testing.T) {
	dialer := &blockingDialerObj{started: make(chan struct{})}
	cfg := tcpCfgOnFreePort(t)
	cfg.Network = dialer
	cfg.DialTimeout = time.Hour
	s, control, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()

	packet, err := statute.NewDatagram("127.0.0.1:9", []byte("pending-dial"))
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	if _, err = udpConn.WriteTo(packet.Bytes(), relay); err != nil {
		t.Fatalf("UDP relay write: %v", err)
	}
	select {
	case <-dialer.started:
	case <-time.After(time.Second):
		t.Fatal("UDP dialer was not called")
	}

	if err = control.Close(); err != nil {
		t.Fatalf("control close: %v", err)
	}
	waitActiveConns(t, s, 0)
}

// //

func TestNew_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	path := t.TempDir() + "/test.sock"
	s := newSocks(t, ConfigObj{Network: mockDialerObj{}, Addr: path, Logger: noopLogObj{}})
	defer func() { _ = s.Close() }()
	if !s.IsUnix() {
		t.Error("expected IsUnix=true")
	}
	if s.Addr() != path {
		t.Errorf("expected Addr=%q, got %q", path, s.Addr())
	}
}

func TestNew_UnixDefaultMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	path := t.TempDir() + "/mode.sock"
	s := newSocks(t, ConfigObj{Network: mockDialerObj{}, Addr: path, Logger: noopLogObj{}})
	defer func() { _ = s.Close() }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != defaultUnixSocketMode {
		t.Fatalf("expected unix socket mode %o, got %o", defaultUnixSocketMode, got)
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
	if err = ln.Close(); err != nil {
		t.Fatalf("close stale socket: %v", err)
	}

	// listenUnix should detect the stale socket, remove it, and re-bind
	ln2, err := listenUnix(path, defaultUnixSocketMode)
	if err != nil {
		t.Fatalf("listenUnix stale: %v", err)
	}
	if err = ln2.Close(); err != nil {
		t.Fatalf("close rebound socket: %v", err)
	}
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
	defer func() { _ = ln.Close() }()

	_, err = listenUnix(path, defaultUnixSocketMode)
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
	if err = f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	if err := removeUnixSocket(path); !errors.Is(err, ErrSocketRefusal) {
		t.Fatalf("expected ErrSocketRefusal, got %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("regular file should stay in place, got %v", err)
	}
}

func TestIsAddrInUse(t *testing.T) {
	// Bind a port and try to bind again to trigger EADDRINUSE
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

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
	defer func() { _ = inner.Close() }()

	limited := newTestLimitedListener(inner, 3, 0, 0)
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
	defer func() { _ = conn.Close() }()

	if c := <-done; c != nil {
		_ = c.Close()
	}

	if limited.limit.Active() != 1 {
		t.Errorf("expected 1 active slot, got %d", limited.limit.Active())
	}
}

func TestLimitedListener_retriesTemporaryAcceptError(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	inner := &temporaryAcceptListenerObj{
		conn:   server,
		closed: make(chan struct{}),
	}
	limited := newTestLimitedListener(inner, 1, 0, 0)
	defer func() { _ = limited.Close() }()

	conn, err := limited.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if got := inner.attempts.Load(); got != 2 {
		t.Fatalf("expected one retry after temporary error, got %d attempts", got)
	}
}

func TestLimitedListener_closeUnblocksSemaphoreWait(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inner.Close() }()

	limited := newTestLimitedListener(inner, 1, 0, 0)
	if !limited.limit.Acquire() {
		t.Fatal("failed to prefill limiter")
	}

	done := make(chan error, 1)
	go func() {
		conn, err := limited.Accept()
		if conn != nil {
			_ = conn.Close()
		}
		done <- err
	}()

	client, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	time.Sleep(50 * time.Millisecond)
	if err = limited.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		<-done
		t.Fatal("Accept stayed blocked after Close")
	}
}

func TestLimitedConn_releasesSemaphoreOnClose(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inner.Close() }()

	go func() {
		c, _ := net.Dial("tcp", inner.Addr().String())
		if c != nil {
			_ = c.Close()
		}
	}()

	ac, err := inner.Accept()
	if err != nil {
		t.Fatal(err)
	}

	owner := newTestLimitedListener(inner, 1, 0, 0)
	if !owner.limit.Acquire() {
		t.Fatal("failed to acquire limiter")
	}
	lc := &limitedConnObj{Conn: ac, owner: owner}
	if err = lc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if owner.limit.Active() != 0 {
		t.Errorf("expected no active slots after Close, got %d", owner.limit.Active())
	}
}

func TestLimitedConn_closeOnce(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inner.Close() }()

	go func() {
		c, _ := net.Dial("tcp", inner.Addr().String())
		if c != nil {
			_ = c.Close()
		}
	}()
	ac, err := inner.Accept()
	if err != nil {
		t.Fatal(err)
	}

	owner := newTestLimitedListener(inner, 2, 0, 0)
	if !owner.limit.Acquire() {
		t.Fatal("failed to acquire limiter")
	}
	lc := &limitedConnObj{Conn: ac, owner: owner}

	if err = lc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	_ = lc.Close()
	_ = lc.Close()

	if owner.limit.Active() != 0 {
		t.Errorf("expected no active slots after Close, got %d", owner.limit.Active())
	}
}

func TestLimitedConn_activityRefreshesDeadline(t *testing.T) {
	rec := &deadlineRecorderConnObj{}
	conn := &limitedConnObj{
		Conn:              rec,
		tunnelIdleTimeout: time.Minute,
	}
	conn.finishHandshake()

	if _, err := conn.Write([]byte{0x01}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if rec.deadlines != 1 {
		t.Fatalf("first activity should refresh deadline, got %d", rec.deadlines)
	}
	if _, err := conn.Write([]byte{0x02}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if rec.deadlines != 1 {
		t.Fatalf("immediate second activity should not refresh deadline, got %d", rec.deadlines)
	}
}

func TestLimitedConn_activitySkipsDeadlineWhenIdleTimeoutDisabled(t *testing.T) {
	rec := &deadlineRecorderConnObj{}
	conn := &limitedConnObj{
		Conn:              rec,
		tunnelIdleTimeout: 0,
	}
	conn.finishHandshake()

	if _, err := conn.Write([]byte{0x01}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.deadlines != 0 {
		t.Fatalf("disabled idle timeout should not touch SetDeadline, got %d", rec.deadlines)
	}
}

func TestLimitedConn_handshakeActivityDoesNotRefreshTunnelDeadline(t *testing.T) {
	rec := &deadlineRecorderConnObj{}
	conn := &limitedConnObj{
		Conn:              rec,
		tunnelIdleTimeout: time.Minute,
	}

	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		t.Fatalf("handshake write: %v", err)
	}
	if rec.deadlines != 0 {
		t.Fatalf("handshake activity should not refresh tunnel deadline, got %d", rec.deadlines)
	}

	conn.finishHandshake()
	if rec.readDeadlines != 1 {
		t.Fatalf("finishing handshake should clear read deadline once, got %d", rec.readDeadlines)
	}
	if rec.deadlines != 1 {
		t.Fatalf("finishing handshake should start tunnel deadline, got %d", rec.deadlines)
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		t.Fatalf("post-handshake write: %v", err)
	}
	if rec.deadlines != 1 {
		t.Fatalf("immediate post-handshake write should not refresh deadline, got %d", rec.deadlines)
	}
}

func TestLimitedConn_associateHandshakeSkipsTunnelIdle(t *testing.T) {
	rec := &deadlineRecorderConnObj{}
	conn := &limitedConnObj{
		Conn:              rec,
		tunnelIdleTimeout: time.Minute,
	}

	conn.finishHandshakeWithoutTunnelIdle()
	if rec.readDeadlines != 1 {
		t.Fatalf("associate handshake should clear read deadline once, got %d", rec.readDeadlines)
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		t.Fatalf("associate reply write: %v", err)
	}
	if rec.deadlines != 0 {
		t.Fatalf("associate control connection should not start tunnel idle deadline, got %d", rec.deadlines)
	}
}

func TestFinishHandshake_ignoresNonLimitedWriter(t *testing.T) {
	if err := finishHandshake(context.Background(), io.Discard, nil); err != nil {
		t.Fatalf("finishHandshake: %v", err)
	}
}

// //

func TestConcurrentClose(t *testing.T) {
	for i := 0; i < 300; i++ {
		s := newSocks(t, tcpCfg())
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = s.Close() }()
		go func() { defer wg.Done(); _ = s.Close() }()
		wg.Wait()
		if err := s.Close(); err != nil {
			t.Fatalf("iter %d final close: %v", i, err)
		}
	}
}

// TestAssociate_targetDialedAfterCloseReleasesResources drives the close/dial
// race: close() runs after DialContext succeeds but before the target insert.
// The just-dialed conn and the global limiter slot must be released, no target
// inserted, and no forward() goroutine spawned.
func TestAssociate_targetDialedAfterCloseReleasesResources(t *testing.T) {
	relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer func() { _ = relay.Close() }()

	limiter := common.NewDynamicLimit(4)
	trackConn := &closeTrackConnObj{}
	dialer := &gateDialerObj{
		started: make(chan struct{}),
		release: make(chan struct{}),
		conn:    trackConn,
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &associateSessionObj{
		owner:         &Obj{},
		ctx:           ctx,
		cancel:        cancel,
		network:       dialer,
		relay:         relay,
		targetLimit:   10,
		globalLimiter: limiter,
		targets:       make(map[string]*associateTargetObj),
	}

	packet, err := statute.NewDatagram("127.0.0.1:9", []byte("x"))
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	client := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}

	type resultObj struct {
		target *associateTargetObj
		err    error
	}
	done := make(chan resultObj, 1)
	go func() {
		tgt, err := s.target(packet, client)
		done <- resultObj{tgt, err}
	}()

	select {
	case <-dialer.started:
	case <-time.After(time.Second):
		t.Fatal("dialer was not called")
	}

	// Close mid-dial: session marks itself closed and snapshots an empty target set.
	s.close()
	close(dialer.release)

	var res resultObj
	select {
	case res = <-done:
	case <-time.After(time.Second):
		t.Fatal("target did not return after close")
	}

	if res.target != nil {
		t.Fatalf("expected no target after close, got %+v", res.target)
	}
	if !errors.Is(res.err, net.ErrClosed) {
		t.Fatalf("expected net.ErrClosed, got %v", res.err)
	}
	if !trackConn.closed.Load() {
		t.Fatal("dialed conn was not closed on the closed-after-dial path")
	}
	if got := limiter.Active(); got != 0 {
		t.Fatalf("expected global limiter slot released, got %d active", got)
	}
	s.targetMu.Lock()
	n := len(s.targets)
	s.targetMu.Unlock()
	if n != 0 {
		t.Fatalf("expected empty target set, got %d", n)
	}
}

func TestDisabled_analyticsReturnZero(t *testing.T) {
	s := NewDisabled()
	if got := s.ActiveConnections(); got != 0 {
		t.Fatalf("expected 0 active connections on disabled server, got %d", got)
	}
}

func TestActiveConnections_reflectsLiveCount(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	s := newSocks(t, cfg)

	if got := s.ActiveConnections(); got != 0 {
		t.Fatalf("expected 0 active connections before dialing, got %d", got)
	}

	conn, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	waitActiveConns(t, s, 1)
	if got := s.ActiveConnections(); got != 1 {
		t.Fatalf("expected 1 active connection, got %d", got)
	}

	_ = conn.Close()
	waitActiveConns(t, s, 0)
	if got := s.ActiveConnections(); got != 0 {
		t.Fatalf("expected 0 active connections after close, got %d", got)
	}
}

func BenchmarkNewClose(b *testing.B) {
	for b.Loop() {
		s, err := New(tcpCfg())
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		if err := s.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}
}

// Adversarial: on a non-loopback listener a client may declare 0.0.0.0/0 in the
// ASSOCIATE request; the relay must still refuse datagrams from any host other
// than the one that opened the control connection.
func TestAssociate_rejectsForeignSourceIP(t *testing.T) {
	control := net.ParseIP("10.0.0.1")

	accepted := &associateSessionObj{controlIP: control}
	if _, ok := accepted.acceptClient(&net.UDPAddr{IP: control, Port: 5000}); !ok {
		t.Fatal("datagram from the control host must be accepted")
	}

	foreign := &associateSessionObj{controlIP: control}
	if _, ok := foreign.acceptClient(&net.UDPAddr{IP: net.ParseIP("10.0.0.9"), Port: 5000}); ok {
		t.Fatal("first datagram from a foreign host must be rejected, not win the relay")
	}

	// When the control IP is unknown (writer is not a net.Conn), pinning is off
	// and the prior first-source-wins behavior is preserved.
	unpinned := &associateSessionObj{}
	if _, ok := unpinned.acceptClient(&net.UDPAddr{IP: net.ParseIP("10.0.0.9"), Port: 5000}); !ok {
		t.Fatal("nil control IP should fall back to accepting the first source")
	}
}
