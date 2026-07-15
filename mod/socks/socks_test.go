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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"github.com/voluminor/ratatoskr/internal/common"
)

// // // // // // // // // //

type noopLogObj = common.DiscardLoggerObj

// //

func (s *associateSessionObj) target(packet statute.Datagram) (*associateTargetObj, error) {
	if s.maxQueuedPackets == 0 {
		s.maxQueuedPackets = defaultMaxAssociateQueuedPackets
	}
	if s.maxQueuedBytes == 0 {
		s.maxQueuedBytes = defaultMaxAssociateQueuedBytes
	}
	key, err := associateTargetKey(packet.DstAddr)
	if err != nil {
		return nil, err
	}
	return s.createTarget(packet, key)
}

// //

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

type closeTrackConnObj struct {
	closed atomic.Bool
}

type blockingWriteConnObj struct {
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
	once    sync.Once
	writes  atomic.Int64
}

type datagramSequenceConnObj struct {
	reads  chan []byte
	closed chan struct{}
	once   sync.Once
}

func newDatagramSequenceConnObj() *datagramSequenceConnObj {
	return &datagramSequenceConnObj{reads: make(chan []byte, 2), closed: make(chan struct{})}
}

func (c *datagramSequenceConnObj) Read(p []byte) (int, error) {
	select {
	case payload := <-c.reads:
		return copy(p, payload), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *datagramSequenceConnObj) Write(p []byte) (int, error) { return len(p), nil }
func (c *datagramSequenceConnObj) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}
func (c *datagramSequenceConnObj) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *datagramSequenceConnObj) RemoteAddr() net.Addr             { return &net.UDPAddr{} }
func (c *datagramSequenceConnObj) SetDeadline(time.Time) error      { return nil }
func (c *datagramSequenceConnObj) SetReadDeadline(time.Time) error  { return nil }
func (c *datagramSequenceConnObj) SetWriteDeadline(time.Time) error { return nil }

func newBlockingWriteConnObj() *blockingWriteConnObj {
	return &blockingWriteConnObj{
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (c *blockingWriteConnObj) Read([]byte) (int, error) { <-c.closed; return 0, net.ErrClosed }
func (c *blockingWriteConnObj) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		c.writes.Add(1)
		return len(p), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}
func (c *blockingWriteConnObj) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (c *blockingWriteConnObj) LocalAddr() net.Addr              { return nil }
func (c *blockingWriteConnObj) RemoteAddr() net.Addr             { return nil }
func (c *blockingWriteConnObj) SetDeadline(time.Time) error      { return nil }
func (c *blockingWriteConnObj) SetReadDeadline(time.Time) error  { return nil }
func (c *blockingWriteConnObj) SetWriteDeadline(time.Time) error { return nil }

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

type rotatingResolverObj struct {
	name  string
	ips   []net.IP
	calls atomic.Int64
}

type failingResolverObj struct {
	err   error
	calls atomic.Int64
}

type mapResolverObj struct {
	ips   map[string]net.IP
	calls atomic.Int64
}

func (r *staticResolverObj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if name != r.name {
		return ctx, nil, errors.New("unexpected resolver name")
	}
	r.calls.Add(1)
	return ctx, append(net.IP(nil), r.ip...), nil
}

func (r *rotatingResolverObj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if name != canonicalTestName(r.name) {
		return ctx, nil, errors.New("unexpected resolver name")
	}
	call := r.calls.Add(1)
	ip := r.ips[int((call-1)%int64(len(r.ips)))]
	return ctx, append(net.IP(nil), ip...), nil
}

func (r *failingResolverObj) Resolve(ctx context.Context, _ string) (context.Context, net.IP, error) {
	r.calls.Add(1)
	return ctx, nil, r.err
}

func (r *mapResolverObj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	ip, ok := r.ips[canonicalTestName(name)]
	if !ok {
		return ctx, nil, errors.New("unexpected resolver name")
	}
	r.calls.Add(1)
	return ctx, append(net.IP(nil), ip...), nil
}

func canonicalTestName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
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

func associateRelayOnServer(t *testing.T, cfg ConfigObj, ip string, port int) (net.Conn, *net.UDPAddr) {
	t.Helper()
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
	return conn, relay
}

func associateRelay(t *testing.T, cfg ConfigObj, ip string, port int) (*Obj, net.Conn, *net.UDPAddr) {
	t.Helper()
	s := newSocks(t, cfg)
	t.Cleanup(func() { _ = s.Close() })
	conn, relay := associateRelayOnServer(t, cfg, ip, port)
	return s, conn, relay
}

func sendSocksUDPDatagram(t *testing.T, conn net.PacketConn, relay net.Addr, target string, payload []byte) statute.Datagram {
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
	return got
}

func sendSocksUDP(t *testing.T, conn net.PacketConn, relay net.Addr, target string, payload []byte) []byte {
	t.Helper()
	return sendSocksUDPDatagram(t, conn, relay, target, payload).Data
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

func TestStartCloseStartUsesFreshConnectTargetSet(t *testing.T) {
	cfg := tcpCfgOnFreePort(t)
	s := NewDisabled()
	for generation := 1; generation <= 2; generation++ {
		if err := s.Start(cfg); err != nil {
			t.Fatalf("generation %d Start: %v", generation, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("generation %d Close: %v", generation, err)
		}
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

func TestNew_emptyAddr(t *testing.T) {
	_, err := New(ConfigObj{Network: mockDialerObj{}, Logger: noopLogObj{}})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("expected ErrInvalidAddress, got %v", err)
	}
}

func TestStart_emptyAddrDoesNotEnable(t *testing.T) {
	s := NewDisabled()
	err := s.Start(ConfigObj{Network: mockDialerObj{}, Logger: noopLogObj{}})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("expected ErrInvalidAddress, got %v", err)
	}
	if s.IsEnabled() {
		t.Fatal("empty address must not enable the listener")
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

func TestMaxAssociateTargetsPerSession_normalization(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "default", in: 0, want: defaultMaxAssociateTargetsPerSession},
		{name: "custom", in: 7, want: 7},
		{name: "no per-session cap", in: -1, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tcpCfg()
			cfg.MaxAssociateTargetsPerSession = tt.in
			s := newSocks(t, cfg)
			defer func() { _ = s.Close() }()

			if got := s.MaxAssociateTargetsPerSession(); got != tt.want {
				t.Fatalf("MaxAssociateTargetsPerSession = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAssociateQueueLimitNormalization(t *testing.T) {
	tests := []struct {
		name        string
		packets     int
		bytes       int
		wantPackets int
		wantBytes   int
	}{
		{name: "defaults", wantPackets: defaultMaxAssociateQueuedPackets, wantBytes: defaultMaxAssociateQueuedBytes},
		{name: "custom", packets: 7, bytes: 1234, wantPackets: 7, wantBytes: 1234},
		{name: "unlimited", packets: -1, bytes: -1, wantPackets: -1, wantBytes: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tcpCfg()
			cfg.MaxAssociateQueuedPacketsPerTarget = tt.packets
			cfg.MaxAssociateQueuedBytesPerTarget = tt.bytes
			s := newSocks(t, cfg)
			defer func() { _ = s.Close() }()
			if got := s.MaxAssociateQueuedPacketsPerTarget(); got != tt.wantPackets {
				t.Fatalf("packet cap = %d, want %d", got, tt.wantPackets)
			}
			if got := s.MaxAssociateQueuedBytesPerTarget(); got != tt.wantBytes {
				t.Fatalf("byte cap = %d, want %d", got, tt.wantBytes)
			}
		})
	}
}

func TestAssociatePacketQueueBoundsPacketsAndBytes(t *testing.T) {
	q := newAssociatePacketQueue(2, 5)
	if ok, _ := q.enqueue([]byte("abc")); !ok {
		t.Fatal("first packet rejected")
	}
	if ok, _ := q.enqueue([]byte("de")); !ok {
		t.Fatal("second packet rejected")
	}
	if ok, full := q.enqueue([]byte("f")); ok || !full {
		t.Fatalf("over-limit enqueue = (%v, %v), want (false, true)", ok, full)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, ok := q.pop(ctx)
	if !ok || string(first) != "abc" {
		t.Fatalf("first pop = %q, %v", first, ok)
	}
	if ok, _ = q.enqueue([]byte("fgh")); !ok {
		t.Fatal("byte budget was not released after pop")
	}
	q.close()
	if ok, full := q.enqueue([]byte("closed")); ok || full {
		t.Fatalf("closed enqueue = (%v, %v), want (false, false)", ok, full)
	}
}

func TestAssociateTargetCountsQueueFullDrop(t *testing.T) {
	owner := &Obj{}
	target := &associateTargetObj{
		session: &associateSessionObj{owner: owner},
		out:     newAssociatePacketQueue(1, 1024),
	}
	target.enqueue([]byte("first"))
	target.enqueue([]byte("dropped"))
	if got := owner.Snapshot().DroppedAssociatePackets; got != 1 {
		t.Fatalf("DroppedAssociatePackets = %d, want 1", got)
	}
	target.out.close()
}

func TestAssociateTargetWriterIsolatesEstablishedTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session := &associateSessionObj{ctx: ctx, cancel: cancel}
	slowConn := newBlockingWriteConnObj()
	fastConn, fastPeer := net.Pipe()
	defer func() { _ = fastPeer.Close() }()
	slow := &associateTargetObj{session: session, conn: slowConn, out: newAssociatePacketQueue(4, 1024)}
	fast := &associateTargetObj{session: session, conn: fastConn, out: newAssociatePacketQueue(4, 1024)}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); slow.write() }()
	go func() { defer wg.Done(); fast.write() }()
	slow.enqueue([]byte("slow"))
	select {
	case <-slowConn.started:
	case <-time.After(time.Second):
		t.Fatal("slow target did not enter Write")
	}

	fast.enqueue([]byte("fast"))
	if err := fastPeer.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(fastPeer, buf); err != nil {
		t.Fatalf("fast target blocked behind slow target: %v", err)
	}
	if string(buf) != "fast" {
		t.Fatalf("fast payload = %q", buf)
	}

	close(slowConn.release)
	cancel()
	wg.Wait()
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

func TestClose_unblocksHalfClosedConnectWithSilentTarget(t *testing.T) {
	targetLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("target listen: %v", err)
	}
	defer func() { _ = targetLn.Close() }()

	targetAccepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := targetLn.Accept()
		if acceptErr == nil {
			targetAccepted <- conn
		}
	}()

	cfg := tcpCfgOnFreePort(t)
	cfg.TunnelIdleTimeout = -1
	s := newSocks(t, cfg)

	clientRaw, err := net.Dial("tcp", cfg.Addr)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	client := clientRaw.(*net.TCPConn)
	defer func() { _ = client.Close() }()

	if resp := sendNoAuthGreeting(t, client); resp[1] != 0x00 {
		t.Fatalf("expected no-auth accepted, got %v", resp)
	}
	targetAddr := targetLn.Addr().(*net.TCPAddr)
	if resp := sendIPv4ConnectRequest(t, client, "127.0.0.1", targetAddr.Port); resp[1] != 0x00 {
		t.Fatalf("expected CONNECT success, got %v", resp)
	}

	var target net.Conn
	select {
	case target = <-targetAccepted:
	case <-time.After(time.Second):
		t.Fatal("target connection was not accepted")
	}
	defer func() { _ = target.Close() }()

	targetSawEOF := make(chan struct{})
	go func() {
		var buf [1]byte
		_, _ = target.Read(buf[:])
		close(targetSawEOF)
	}()
	if err = client.CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	select {
	case <-targetSawEOF:
	case <-time.After(time.Second):
		t.Fatal("client half-close did not reach target")
	}

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err = <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		_ = target.Close()
		<-closed
		t.Fatal("Close stayed blocked on the silent outbound CONNECT target")
	}
}

func TestConnectTargetSet_trackRacingCloseNeverLeaks(t *testing.T) {
	for i := 0; i < 1000; i++ {
		targets := newConnectTargetSet()
		conn := &closeTrackConnObj{}
		start := make(chan struct{})
		tracked := make(chan error, 1)
		var closeWG sync.WaitGroup
		closeWG.Add(1)
		go func() {
			<-start
			_, trackErr := targets.track(conn)
			tracked <- trackErr
		}()
		go func() {
			defer closeWG.Done()
			<-start
			targets.closeAll()
		}()
		close(start)

		err := <-tracked
		closeWG.Wait()
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("iteration %d: track error = %v, want nil or net.ErrClosed", i, err)
		}
		if !conn.closed.Load() {
			t.Fatalf("iteration %d: target escaped concurrent close", i)
		}
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
		tunnelIdleTimeout: time.Minute,
	}
	conn.refreshActivityDeadline()
	conn.tunnelIdleTimeout = 0
	conn.refreshActivityDeadline()

	recorder := conn.Conn.(*deadlineRecorderConnObj)
	deadlines, lastDeadline := recorder.snapshotDeadline()
	if deadlines != 2 {
		t.Fatalf("expected one arm and one clear, got %d deadline changes", deadlines)
	}
	if !lastDeadline.IsZero() {
		t.Fatalf("expected zero deadline, got %s", lastDeadline)
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
	s.finishServe(ln, nil, boom)
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

func TestAssociate_udpConcurrentTargets(t *testing.T) {
	const targets = 6
	echoes := make([]net.PacketConn, targets)
	for i := range echoes {
		echoes[i] = udpEchoServer(t)
	}
	cfg := tcpCfgOnFreePort(t)
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(3 * time.Second))

	want := make(map[string]struct{}, targets)
	for i, echo := range echoes {
		payload := []byte("ratatoskr-udp-" + strconv.Itoa(i))
		want[string(payload)] = struct{}{}
		packet, err := statute.NewDatagram(echo.LocalAddr().String(), payload)
		if err != nil {
			t.Fatalf("NewDatagram: %v", err)
		}
		if _, err := udpConn.WriteTo(packet.Bytes(), relay); err != nil {
			t.Fatalf("relay write: %v", err)
		}
	}

	got := make(map[string]struct{}, targets)
	buf := make([]byte, 64*1024)
	for len(got) < targets {
		n, _, err := udpConn.ReadFrom(buf)
		if err != nil {
			t.Fatalf("relay read (%d/%d received): %v", len(got), targets, err)
		}
		d, err := statute.ParseDatagram(buf[:n])
		if err != nil {
			t.Fatalf("ParseDatagram: %v", err)
		}
		got[string(d.Data)] = struct{}{}
	}
	for w := range want {
		if _, ok := got[w]; !ok {
			t.Fatalf("missing echo %q", w)
		}
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
	got = sendSocksUDP(t, udpConn, relay, target, payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected second UDP echo payload %q", got)
	}
	if got := resolver.calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}

func TestAssociate_datagramDomainKeyIgnoresResolverRotation(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &rotatingResolverObj{
		name: "udp-target.pk.ygg",
		ips:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 2)},
	}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp-rr")
	target := net.JoinHostPort("UDP-Target.PK.YGG.", strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port))
	got := sendSocksUDPDatagram(t, udpConn, relay, target, payload)
	if !bytes.Equal(got.Data, payload) {
		t.Fatalf("unexpected UDP echo payload %q", got.Data)
	}
	if got.DstAddr.FQDN != "UDP-Target.PK.YGG." {
		t.Fatalf("first response FQDN = %q, want first spelling", got.DstAddr.FQDN)
	}
	got = sendSocksUDPDatagram(t, udpConn, relay, net.JoinHostPort("udp-target.pk.ygg", strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port)), payload)
	if !bytes.Equal(got.Data, payload) {
		t.Fatalf("unexpected second UDP echo payload %q", got.Data)
	}
	if got.DstAddr.FQDN != "UDP-Target.PK.YGG." {
		t.Fatalf("second response FQDN = %q, want first target spelling", got.DstAddr.FQDN)
	}
	if got := resolver.calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}

func TestAssociate_datagramDomainsResolvingSameIPKeepDistinctTargets(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &mapResolverObj{
		ips: map[string]net.IP{
			"one.pk.ygg": net.IPv4(127, 0, 0, 1),
			"two.pk.ygg": net.IPv4(127, 0, 0, 1),
		},
	}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp-same-ip")
	port := strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port)
	first := sendSocksUDPDatagram(t, udpConn, relay, net.JoinHostPort("one.pk.ygg", port), payload)
	if !bytes.Equal(first.Data, payload) {
		t.Fatalf("unexpected first UDP echo payload %q", first.Data)
	}
	second := sendSocksUDPDatagram(t, udpConn, relay, net.JoinHostPort("two.pk.ygg", port), payload)
	if !bytes.Equal(second.Data, payload) {
		t.Fatalf("unexpected second UDP echo payload %q", second.Data)
	}
	if first.DstAddr.FQDN != "one.pk.ygg" {
		t.Fatalf("first response FQDN = %q, want one.pk.ygg", first.DstAddr.FQDN)
	}
	if second.DstAddr.FQDN != "two.pk.ygg" {
		t.Fatalf("second response FQDN = %q, want two.pk.ygg", second.DstAddr.FQDN)
	}
	if got := resolver.calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want 2", got)
	}
}

func TestAssociate_ipLiteralSkipsResolver(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &failingResolverObj{err: errors.New("resolver should not be used")}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()
	_ = udpConn.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp-ip")
	got := sendSocksUDP(t, udpConn, relay, echo.LocalAddr().String(), payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected UDP echo payload %q", got)
	}
	if got := resolver.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
}

func TestAssociateTargetKey_separatesDomainEncodedIPFromIPLiteral(t *testing.T) {
	domainKey, err := associateTargetKey(statute.AddrSpec{
		FQDN:     "127.0.0.1",
		Port:     53,
		AddrType: statute.ATYPDomain,
	})
	if err != nil {
		t.Fatalf("domain key: %v", err)
	}
	literalKey, err := associateTargetKey(statute.AddrSpec{
		IP:       net.IPv4(127, 0, 0, 1),
		Port:     53,
		AddrType: statute.ATYPIPv4,
	})
	if err != nil {
		t.Fatalf("literal key: %v", err)
	}
	if domainKey == literalKey {
		t.Fatalf("domain-form IP and IP literal must not share a target key: %+v", domainKey)
	}
	if domainKey.kind != statute.ATYPDomain || literalKey.kind != statute.ATYPIPv4 {
		t.Fatalf("unexpected key kinds: domain=%d literal=%d", domainKey.kind, literalKey.kind)
	}
}

func TestAssociate_invalidDomainTargetDoesNotResolveOrAcquireSlot(t *testing.T) {
	if _, err := associateTargetKey(statute.AddrSpec{
		FQDN:     string([]byte{0xff}),
		Port:     53,
		AddrType: statute.ATYPDomain,
	}); !errors.Is(err, errAssociateInvalidTarget) {
		t.Fatalf("invalid UTF-8 domain key error = %v, want errAssociateInvalidTarget", err)
	}

	limiter := common.NewDynamicLimit(1)
	resolver := &failingResolverObj{err: errors.New("resolver should not be used")}
	s := &associateSessionObj{
		owner:         &Obj{},
		ctx:           context.Background(),
		network:       mockDialerObj{},
		resolver:      resolver,
		serverLimiter: limiter,
		maxTargets:    defaultMaxAssociateTargetsPerSession,
		targets:       make(map[associateTargetKeyObj]*associateTargetObj),
	}

	target, err := s.target(statute.Datagram{
		DstAddr: statute.AddrSpec{FQDN: ".", Port: 53, AddrType: statute.ATYPDomain},
		Data:    []byte("x"),
	})
	if target != nil {
		t.Fatalf("expected no target, got %+v", target)
	}
	if !errors.Is(err, errAssociateInvalidTarget) {
		t.Fatalf("expected errAssociateInvalidTarget, got %v", err)
	}
	if got := resolver.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := limiter.Active(); got != 0 {
		t.Fatalf("server associate targets = %d, want 0", got)
	}
}

func TestAssociate_unresolvedClientDomainIsRejected(t *testing.T) {
	request := &socks5.Request{
		DestAddr:  &statute.AddrSpec{FQDN: "client.example", Port: 1234, AddrType: statute.ATYPDomain},
		LocalAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
		Reader:    bytes.NewReader(nil),
	}
	var reply bytes.Buffer
	err := (&Obj{}).handleAssociate(context.Background(), &reply, request, mockDialerObj{}, nil)
	if err == nil {
		t.Fatal("unresolved client domain was accepted")
	}
	parsed, parseErr := statute.ParseReply(&reply)
	if parseErr != nil {
		t.Fatalf("ParseReply: %v", parseErr)
	}
	if parsed.Response != statute.RepHostUnreachable {
		t.Fatalf("associate reply = %d, want RepHostUnreachable", parsed.Response)
	}
}

func TestAssociate_oversizedReverseDatagramDoesNotCloseTarget(t *testing.T) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err = client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := newDatagramSequenceConnObj()
	owner := &Obj{}
	session := &associateSessionObj{
		owner:     owner,
		ctx:       ctx,
		cancel:    cancel,
		relay:     relay,
		clientUDP: client.LocalAddr().(*net.UDPAddr),
		targets:   make(map[associateTargetKeyObj]*associateTargetObj),
	}
	header := []byte{0, 0, 0, statute.ATYPIPv4, 127, 0, 0, 1, 0, 53}
	target := &associateTargetObj{
		session: session,
		conn:    upstream,
		header:  header,
		out:     newAssociatePacketQueue(1, 1),
	}
	done := make(chan struct{})
	go func() {
		target.forward()
		close(done)
	}()

	upstream.reads <- make([]byte, 65507)
	upstream.reads <- []byte("small")
	buf := make([]byte, 128)
	n, _, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("small response after oversized datagram: %v", err)
	}
	if want := append(append([]byte(nil), header...), []byte("small")...); !bytes.Equal(buf[:n], want) {
		t.Fatalf("reverse datagram = %x, want %x", buf[:n], want)
	}
	if got := owner.associatePacketDrops.Load(); got != 1 {
		t.Fatalf("associate packet drops = %d, want 1", got)
	}
	target.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("target forward did not stop")
	}
}

func TestAssociate_perSessionTargetLimitDoesNotBlockOtherSessions(t *testing.T) {
	echo1 := udpEchoServer(t)
	echo2 := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	cfg.MaxAssociateTargetsPerSession = 1
	s := newSocks(t, cfg)
	t.Cleanup(func() { _ = s.Close() })

	_, relay1 := associateRelayOnServer(t, cfg, "0.0.0.0", 0)
	first, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	_ = first.SetDeadline(time.Now().Add(time.Second))

	payload := []byte("ratatoskr-socks-udp-cap")
	got := sendSocksUDP(t, first, relay1, echo1.LocalAddr().String(), payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected first UDP echo payload %q", got)
	}

	packet, err := statute.NewDatagram(echo2.LocalAddr().String(), payload)
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	if _, err = first.WriteTo(packet.Bytes(), relay1); err != nil {
		t.Fatalf("UDP relay write: %v", err)
	}
	buf := make([]byte, 1024)
	_ = first.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if n, _, err := first.ReadFrom(buf); err == nil {
		t.Fatalf("per-session cap should drop second target, got packet %x", buf[:n])
	}
	if got := s.associateLimiter.Active(); got != 1 {
		t.Fatalf("server associate targets = %d, want 1", got)
	}

	_, relay2 := associateRelayOnServer(t, cfg, "0.0.0.0", 0)
	second, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	_ = second.SetDeadline(time.Now().Add(time.Second))
	got = sendSocksUDP(t, second, relay2, echo2.LocalAddr().String(), payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected second session UDP echo payload %q", got)
	}
}

func TestAssociate_targetRemovalFreesPerSessionCapacity(t *testing.T) {
	echo1 := udpEchoServer(t)
	echo2 := udpEchoServer(t)
	relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer func() { _ = relay.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limiter := common.NewDynamicLimit(4)
	s := &associateSessionObj{
		owner:         &Obj{},
		ctx:           ctx,
		cancel:        cancel,
		network:       mockDialerObj{},
		relay:         relay,
		serverLimiter: limiter,
		maxTargets:    1,
		targets:       make(map[associateTargetKeyObj]*associateTargetObj),
	}

	firstPacket, err := statute.NewDatagram(echo1.LocalAddr().String(), []byte("one"))
	if err != nil {
		t.Fatalf("first NewDatagram: %v", err)
	}
	firstTarget, err := s.target(firstPacket)
	if err != nil {
		t.Fatalf("first target: %v", err)
	}
	if got := limiter.Active(); got != 1 {
		t.Fatalf("server associate targets = %d, want 1", got)
	}

	secondPacket, err := statute.NewDatagram(echo2.LocalAddr().String(), []byte("two"))
	if err != nil {
		t.Fatalf("second NewDatagram: %v", err)
	}
	if target, err := s.target(secondPacket); target != nil || !errors.Is(err, ErrAssociateTargetLimit) {
		t.Fatalf("second target before removal = (%+v, %v), want ErrAssociateTargetLimit", target, err)
	}

	firstTarget.close()
	s.deleteTarget(firstTarget)

	secondTarget, err := s.target(secondPacket)
	if err != nil {
		t.Fatalf("second target after removal: %v", err)
	}
	secondTarget.close()
	s.deleteTarget(secondTarget)
	if got := limiter.Active(); got != 0 {
		t.Fatalf("server associate targets = %d, want 0", got)
	}
}

func TestAssociate_resolveFailureDoesNotConsumeGlobalLimiter(t *testing.T) {
	limiter := common.NewDynamicLimit(1)
	resolver := &failingResolverObj{err: errors.New("resolve failed")}
	s := &associateSessionObj{
		owner:         &Obj{},
		ctx:           context.Background(),
		network:       mockDialerObj{},
		resolver:      resolver,
		serverLimiter: limiter,
		maxTargets:    defaultMaxAssociateTargetsPerSession,
		targets:       make(map[associateTargetKeyObj]*associateTargetObj),
	}
	packet, err := statute.NewDatagram("blocked.pk.ygg:53", []byte("x"))
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	target, err := s.target(packet)
	if target != nil {
		t.Fatalf("expected no target, got %+v", target)
	}
	if err == nil {
		t.Fatal("expected resolve error")
	}
	if got := limiter.Active(); got != 0 {
		t.Fatalf("server associate targets = %d, want 0", got)
	}
	if got := resolver.calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}

func TestAssociate_noPerSessionCapStillHonorsGlobalLimitBeforeResolve(t *testing.T) {
	limiter := common.NewDynamicLimit(1)
	if !limiter.Acquire() {
		t.Fatal("failed to saturate global limiter")
	}
	resolver := &failingResolverObj{err: errors.New("resolver should not be used")}
	s := &associateSessionObj{
		owner:         &Obj{},
		ctx:           context.Background(),
		network:       mockDialerObj{},
		resolver:      resolver,
		serverLimiter: limiter,
		maxTargets:    -1,
		targets:       make(map[associateTargetKeyObj]*associateTargetObj),
	}
	packet, err := statute.NewDatagram("blocked.pk.ygg:53", []byte("x"))
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	target, err := s.target(packet)
	if target != nil {
		t.Fatalf("expected no target, got %+v", target)
	}
	if !errors.Is(err, ErrAssociateTargetLimit) {
		t.Fatalf("expected ErrAssociateTargetLimit, got %v", err)
	}
	if got := resolver.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := limiter.Active(); got != 1 {
		t.Fatalf("server associate targets = %d, want saturated slot to remain", got)
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

func privateSocketTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNew_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets not supported on Windows")
	}
	path := privateSocketTempDir(t) + "/test.sock"
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
	path := privateSocketTempDir(t) + "/mode.sock"
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
	path := privateSocketTempDir(t) + "/stale.sock"

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	if err = ln.Close(); err != nil {
		t.Fatalf("close stale socket: %v", err)
	}

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
	path := privateSocketTempDir(t) + "/active.sock"

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

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err = f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeUnixSocket(path, info); !errors.Is(err, ErrSocketRefusal) {
		t.Fatalf("expected ErrSocketRefusal, got %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("regular file should stay in place, got %v", err)
	}
}

func TestIsAddrInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	_, err = net.Listen("tcp", ln.Addr().String())
	if err == nil {
		t.Skip("expected EADDRINUSE but got nil error")
	}
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
		serverLimiter: limiter,
		maxTargets:    defaultMaxAssociateTargetsPerSession,
		targets:       make(map[associateTargetKeyObj]*associateTargetObj),
	}

	packet, err := statute.NewDatagram("127.0.0.1:9", []byte("x"))
	if err != nil {
		t.Fatalf("NewDatagram: %v", err)
	}
	type resultObj struct {
		target *associateTargetObj
		err    error
	}
	done := make(chan resultObj, 1)
	go func() {
		tgt, err := s.target(packet)
		done <- resultObj{tgt, err}
	}()

	select {
	case <-dialer.started:
	case <-time.After(time.Second):
		t.Fatal("dialer was not called")
	}

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

func TestAssociate_rejectsForeignSourceIP(t *testing.T) {
	control := net.ParseIP("10.0.0.1")

	accepted := &associateSessionObj{controlIP: control}
	if ok := accepted.acceptClient(&net.UDPAddr{IP: control, Port: 5000}); !ok {
		t.Fatal("datagram from the control host must be accepted")
	}

	foreign := &associateSessionObj{controlIP: control}
	if ok := foreign.acceptClient(&net.UDPAddr{IP: net.ParseIP("10.0.0.9"), Port: 5000}); ok {
		t.Fatal("first datagram from a foreign host must be rejected, not win the relay")
	}

	unpinned := &associateSessionObj{}
	if ok := unpinned.acceptClient(&net.UDPAddr{IP: net.ParseIP("10.0.0.9"), Port: 5000}); !ok {
		t.Fatal("nil control IP should fall back to accepting the first source")
	}
}

func TestAssociate_pendingDialDoesNotBlockExistingTarget(t *testing.T) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()

	dialer := &blockingDialerObj{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := &Obj{
		dialTimeout:      time.Hour,
		associateLimiter: common.NewDynamicLimit(defaultMaxAssociateTargets),
		associatePool:    newAssociateWorkerPool(),
	}
	defer owner.associatePool.close()
	session := &associateSessionObj{
		owner:         owner,
		ctx:           ctx,
		cancel:        cancel,
		network:       dialer,
		resolver:      &staticResolverObj{name: "slow.pk.ygg", ip: net.IPv4(127, 0, 0, 1)},
		relay:         relay,
		serverLimiter: owner.associateLimiter,
		workerPool:    owner.associatePool,
		principal:     "test:hol",
		maxTargets:    defaultMaxAssociateTargetsPerSession,
		targets:       make(map[associateTargetKeyObj]*associateTargetObj),
		pending:       make(map[associateTargetKeyObj]struct{}),
	}
	existingPacket := statute.Datagram{DstAddr: statute.AddrSpec{IP: net.IPv4(127, 0, 0, 1), Port: 7, AddrType: statute.ATYPIPv4}, Data: []byte("existing")}
	existingKey, err := associateTargetKey(existingPacket.DstAddr)
	if err != nil {
		t.Fatal(err)
	}
	existing := &associateTargetObj{session: session, key: existingKey, conn: &closeTrackConnObj{}}
	session.targets[existingKey] = existing

	slowPacket := statute.Datagram{DstAddr: statute.AddrSpec{FQDN: "slow.pk.ygg", Port: 8, AddrType: statute.ATYPDomain}, Data: []byte("slow")}
	if target, err := session.route(slowPacket); err != nil || target != nil {
		t.Fatalf("route slow target = (%v, %v), want pending", target, err)
	}
	select {
	case <-dialer.started:
	case <-time.After(time.Second):
		t.Fatal("slow dial did not start")
	}
	if target, err := session.route(slowPacket); err != nil || target != nil {
		t.Fatalf("route duplicate pending target = (%v, %v)", target, err)
	}
	if got := owner.associatePacketDrops.Load(); got != 1 {
		t.Fatalf("pending-target packet drops = %d, want 1", got)
	}

	start := time.Now()
	target, err := session.route(existingPacket)
	if err != nil || target != existing {
		t.Fatalf("route existing target = (%v, %v)", target, err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("existing target blocked behind pending dial for %s", elapsed)
	}
	cancel()
	session.pendingWG.Wait()
}

func TestAssociate_principalLimitDefaultsToUnlimited(t *testing.T) {
	owner := &Obj{associateLimiter: common.NewDynamicLimit(defaultMaxAssociateTargets)}
	session := &associateSessionObj{owner: owner, serverLimiter: owner.associateLimiter, principal: "unix"}
	for range defaultMaxAssociateTargets {
		if !session.acquireTargetSlot() {
			t.Fatal("default principal policy constrained the server-wide capacity")
		}
	}
	if session.acquireTargetSlot() {
		t.Fatal("server-wide cap was bypassed")
	}
	for range defaultMaxAssociateTargets {
		session.releaseTargetSlot(session.principal)
	}
}

func TestAssociateSessionCopiesImmutableGenerationConfig(t *testing.T) {
	owner := &Obj{
		isUnix:                          true,
		associateLimiter:                common.NewDynamicLimit(7),
		associatePool:                   newAssociateWorkerPool(),
		maxAssociateTargetsPerSession:   3,
		maxAssociateTargetsPerPrincipal: 4,
		maxAssociateQueuedPackets:       5,
		maxAssociateQueuedBytes:         6,
		dialTimeout:                     7 * time.Second,
		tunnelIdleTimeout:               8 * time.Second,
	}
	request := &socks5.Request{DestAddr: &statute.AddrSpec{}}
	session := newAssociateSession(owner, context.Background(), mockDialerObj{}, nil, nil, request)
	defer session.cancel()

	owner.mu.Lock()
	owner.maxAssociateTargetsPerSession = 30
	owner.maxAssociateTargetsPerPrincipal = 40
	owner.maxAssociateQueuedPackets = 50
	owner.maxAssociateQueuedBytes = 60
	owner.dialTimeout = time.Minute
	owner.tunnelIdleTimeout = time.Minute
	owner.mu.Unlock()
	if !session.isUnix || session.maxTargets != 3 || session.maxPrincipal != 4 ||
		session.maxQueuedPackets != 5 || session.maxQueuedBytes != 6 ||
		session.dialTimeout != 7*time.Second || session.idleTimeout != 8*time.Second {
		t.Fatalf("session did not retain its generation config: %+v", session)
	}
}

func TestAssociate_principalLimitIsIndependent(t *testing.T) {
	limiter := common.NewDynamicLimit(defaultMaxAssociateTargets)
	const principalLimit = 128
	owner := &Obj{maxAssociateTargetsPerPrincipal: principalLimit}
	a := &associateSessionObj{owner: owner, serverLimiter: limiter, principal: "test:principal-a", maxPrincipal: principalLimit}
	b := &associateSessionObj{owner: owner, serverLimiter: limiter, principal: "test:principal-b", maxPrincipal: principalLimit}
	for range principalLimit {
		if !a.acquireTargetSlot() {
			t.Fatal("principal A reached its limit too early")
		}
	}
	if a.acquireTargetSlot() {
		t.Fatal("principal A exceeded its quota")
	}
	if !b.acquireTargetSlot() {
		t.Fatal("principal B must retain an independent quota")
	}
	b.releaseTargetSlot(b.principal)
	for range principalLimit {
		a.releaseTargetSlot(a.principal)
	}
}

func TestAssociate_serverLimitsAreIndependent(t *testing.T) {
	const principalLimit = 128
	ownerA := &Obj{associateLimiter: common.NewDynamicLimit(defaultMaxAssociateTargets), maxAssociateTargetsPerPrincipal: principalLimit}
	ownerB := &Obj{associateLimiter: common.NewDynamicLimit(defaultMaxAssociateTargets), maxAssociateTargetsPerPrincipal: principalLimit}
	a := &associateSessionObj{owner: ownerA, serverLimiter: ownerA.associateLimiter, principal: "unix", maxPrincipal: principalLimit}
	b := &associateSessionObj{owner: ownerB, serverLimiter: ownerB.associateLimiter, principal: "unix", maxPrincipal: principalLimit}
	for range principalLimit {
		if !a.acquireTargetSlot() {
			t.Fatal("server A reached its principal limit too early")
		}
	}
	if a.acquireTargetSlot() {
		t.Fatal("server A exceeded its principal quota")
	}
	if !b.acquireTargetSlot() {
		t.Fatal("server B was affected by server A's principal quota")
	}
	b.releaseTargetSlot(b.principal)
	for range principalLimit {
		a.releaseTargetSlot(a.principal)
	}
}

func TestAssociate_workerPoolsAreIndependent(t *testing.T) {
	poolA := newAssociateWorkerPool()
	poolB := newAssociateWorkerPool()
	releaseA := make(chan struct{})
	var startedA atomic.Int64
	for range associateWorkerCount {
		if !poolA.submit(func() {
			startedA.Add(1)
			<-releaseA
		}) {
			t.Fatal("failed to occupy a worker in pool A")
		}
	}
	deadline := time.Now().Add(time.Second)
	for startedA.Load() != associateWorkerCount && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := startedA.Load(); got != associateWorkerCount {
		close(releaseA)
		poolA.close()
		poolB.close()
		t.Fatalf("started workers in pool A = %d, want %d", got, associateWorkerCount)
	}
	for range associateJobQueueSize {
		if !poolA.submit(func() {}) {
			close(releaseA)
			poolA.close()
			poolB.close()
			t.Fatal("pool A queue filled before its documented capacity")
		}
	}
	if poolA.submit(func() {}) {
		close(releaseA)
		poolA.close()
		poolB.close()
		t.Fatal("saturated pool A accepted a job beyond its bounded queue")
	}

	doneB := make(chan struct{})
	if !poolB.submit(func() { close(doneB) }) {
		close(releaseA)
		poolA.close()
		poolB.close()
		t.Fatal("pool B was affected by saturation of pool A")
	}
	select {
	case <-doneB:
	case <-time.After(time.Second):
		close(releaseA)
		poolA.close()
		poolB.close()
		t.Fatal("pool B job did not run")
	}
	close(releaseA)
	poolA.close()
	poolB.close()
}

func TestServerTaskGroupWaitsForNestedTasks(t *testing.T) {
	group := &serverTaskGroupObj{}
	startNested := make(chan struct{})
	nestedStarted := make(chan struct{})
	releaseNested := make(chan struct{})
	if err := group.Submit(func() {
		<-startNested
		_ = group.Submit(func() {
			close(nestedStarted)
			<-releaseNested
		})
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitDone := make(chan struct{})
	go func() {
		group.Wait()
		close(waitDone)
	}()
	close(startNested)
	<-nestedStarted
	select {
	case <-waitDone:
		close(releaseNested)
		t.Fatal("Wait returned before a nested proxy task")
	default:
	}
	close(releaseNested)
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after all nested tasks")
	}
}

func TestFailClosedResolver(t *testing.T) {
	ctx := context.Background()
	if _, _, err := (failClosedResolverObj{}).Resolve(ctx, "example.com"); !errors.Is(err, ErrResolverRequired) {
		t.Fatalf("Resolve error = %v, want ErrResolverRequired", err)
	}
}
