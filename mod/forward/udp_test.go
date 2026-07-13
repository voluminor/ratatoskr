package forward

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// // // // // // // // // //

type recordingConnObj struct {
	writes chan []byte
	closed chan struct{}
}

type blockingUDPConnObj struct {
	closeOnce sync.Once
	wrote     chan struct{}
	closed    chan struct{}
	readDone  chan struct{}
}

type stubbornReadConnObj struct {
	readStarted chan struct{}
	release     chan struct{}
	startOnce   sync.Once
}

func newStubbornReadConnObj() *stubbornReadConnObj {
	return &stubbornReadConnObj{readStarted: make(chan struct{}), release: make(chan struct{})}
}

func (c *stubbornReadConnObj) Read([]byte) (int, error) {
	c.startOnce.Do(func() { close(c.readStarted) })
	<-c.release
	return 0, net.ErrClosed
}

func (c *stubbornReadConnObj) Write(p []byte) (int, error) { return len(p), nil }
func (c *stubbornReadConnObj) Close() error                { return nil }
func (c *stubbornReadConnObj) LocalAddr() net.Addr         { return &net.UDPAddr{} }
func (c *stubbornReadConnObj) RemoteAddr() net.Addr        { return &net.UDPAddr{} }
func (c *stubbornReadConnObj) SetDeadline(time.Time) error { return nil }
func (c *stubbornReadConnObj) SetReadDeadline(time.Time) error {
	return nil
}
func (c *stubbornReadConnObj) SetWriteDeadline(time.Time) error { return nil }

func newBlockingUDPConnObj() *blockingUDPConnObj {
	return &blockingUDPConnObj{
		wrote:    make(chan struct{}),
		closed:   make(chan struct{}),
		readDone: make(chan struct{}),
	}
}

func newRecordingConnObj() *recordingConnObj {
	return &recordingConnObj{
		writes: make(chan []byte, 8),
		closed: make(chan struct{}),
	}
}

func (c *recordingConnObj) Read([]byte) (int, error) {
	<-c.closed
	return 0, net.ErrClosed
}

func (c *recordingConnObj) Write(p []byte) (int, error) {
	cp := append([]byte(nil), p...)
	c.writes <- cp
	return len(p), nil
}

func (c *recordingConnObj) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *recordingConnObj) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *recordingConnObj) RemoteAddr() net.Addr             { return &net.UDPAddr{} }
func (c *recordingConnObj) SetDeadline(time.Time) error      { return nil }
func (c *recordingConnObj) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingConnObj) SetWriteDeadline(time.Time) error { return nil }

func (c *blockingUDPConnObj) Read([]byte) (int, error) {
	defer close(c.readDone)
	<-c.closed
	return 0, net.ErrClosed
}

func (c *blockingUDPConnObj) Write(p []byte) (int, error) {
	select {
	case <-c.wrote:
	default:
		close(c.wrote)
	}
	return len(p), nil
}

func (c *blockingUDPConnObj) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return nil
}

func (c *blockingUDPConnObj) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *blockingUDPConnObj) RemoteAddr() net.Addr             { return &net.UDPAddr{} }
func (c *blockingUDPConnObj) SetDeadline(time.Time) error      { return nil }
func (c *blockingUDPConnObj) SetReadDeadline(time.Time) error  { return nil }
func (c *blockingUDPConnObj) SetWriteDeadline(time.Time) error { return nil }

// //

func TestUDPQueueSize_capsByBytes(t *testing.T) {
	if got := udpQueueSize(512); got != udpSessionQueueMaxPackets {
		t.Fatalf("small packet queue = %d, want %d", got, udpSessionQueueMaxPackets)
	}
	if got := udpQueueSize(8192); got != 8 {
		t.Fatalf("8 KiB packet queue = %d, want 8", got)
	}
	if got := udpQueueSize(maxUDPDatagramSize); got != 1 {
		t.Fatalf("max datagram queue = %d, want 1", got)
	}
}

// //

// echoUDPServer starts a UDP echo server on 127.0.0.1:0 and returns its address
func echoUDPServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("echoUDPServer: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if _, err = conn.WriteToUDP(buf[:n], addr); err != nil {
				return
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

func readUDPEchoWithRetry(t *testing.T, conn *net.UDPConn, msg []byte) []byte {
	t.Helper()
	buf := make([]byte, 128)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		n, err := conn.Read(buf)
		if err == nil {
			return append([]byte(nil), buf[:n]...)
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read echo: %v", err)
		}
	}
	t.Fatal("timed out waiting for UDP echo")
	return nil
}

func writeUDPUntilRecorded(t *testing.T, conn *net.UDPConn, upstream *recordingConnObj, msg []byte) []byte {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("write: %v", err)
		}
		select {
		case got := <-upstream.writes:
			return got
		case <-deadline:
			t.Fatal("timed out waiting for upstream write")
		case <-ticker.C:
		}
	}
}

func writeUDPUntilSignal(t *testing.T, conn *net.UDPConn, signal <-chan struct{}, msg []byte) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("write: %v", err)
		}
		select {
		case <-signal:
			return
		case <-deadline:
			t.Fatal("timed out waiting for upstream signal")
		case <-ticker.C:
		}
	}
}

// //

func TestManagerDefaultsAndConfig(t *testing.T) {
	mgr := newTestManagerObj(&mockNodeObj{}, 5*time.Second, ConfigObj{})
	if mgr.maxTCPConnections != DefaultMaxTCPConnections {
		t.Fatalf("default TCP limit = %d, want %d", mgr.maxTCPConnections, DefaultMaxTCPConnections)
	}
	if mgr.maxUDPSessions != DefaultMaxUDPSessions {
		t.Fatalf("default UDP limit = %d, want %d", mgr.maxUDPSessions, DefaultMaxUDPSessions)
	}
	// dialTimeout is stored raw; the default is applied at dial time by dialTimeoutContext.
	dialCtx, cancelDial := dialTimeoutContext(context.Background(), mgr.dialTimeout)
	dl, ok := dialCtx.Deadline()
	cancelDial()
	if !ok || time.Until(dl) < DefaultDialTimeout-time.Second {
		t.Fatalf("default dial timeout not applied at dial time (deadline set=%v)", ok)
	}
	if mgr.tcpIdleTimeout != DefaultTCPIdleTimeout {
		t.Fatalf("default TCP idle timeout = %s, want %s", mgr.tcpIdleTimeout, DefaultTCPIdleTimeout)
	}
	mgr = newTestManagerObj(&mockNodeObj{mtu: 1280}, 5*time.Second, ConfigObj{})
	if got := mgr.effectiveUDPMaxPacketSize(); got != 1280 {
		t.Fatalf("default UDP max packet size = %d, want node MTU", got)
	}

	// Zero config values resolve to defaults through the effective* helpers.
	zero := newTestManagerObj(&mockNodeObj{}, 5*time.Second, ConfigObj{})
	if zero.maxTCPConnections != DefaultMaxTCPConnections {
		t.Fatalf("TCP default value = %d, want %d", zero.maxTCPConnections, DefaultMaxTCPConnections)
	}
	if zero.maxUDPSessions != DefaultMaxUDPSessions {
		t.Fatalf("UDP default value = %d, want %d", zero.maxUDPSessions, DefaultMaxUDPSessions)
	}

	// Negative config values disable the respective limits.
	unlimited := newTestManagerObj(&mockNodeObj{}, 5*time.Second, ConfigObj{
		MaxTCPConnections: -1,
		MaxUDPSessions:    -1,
	})
	if unlimited.maxTCPConnections != -1 {
		t.Fatalf("TCP unlimited value = %d, want -1", unlimited.maxTCPConnections)
	}
	if unlimited.maxUDPSessions != -1 {
		t.Fatalf("UDP unlimited value = %d, want -1", unlimited.maxUDPSessions)
	}

	mgr = newTestManagerObj(&mockNodeObj{}, 5*time.Second, ConfigObj{UDPMaxPacketSize: 512})
	if mgr.udpMaxPacketSize != 512 {
		t.Fatalf("configured UDP max packet size = %d, want 512", mgr.udpMaxPacketSize)
	}
}

func TestUDPMaxPacketSizeHelpers(t *testing.T) {
	if got := clampUDPMaxPacketSize(0); got != maxUDPDatagramSize {
		t.Fatalf("zero max packet size = %d, want %d", got, maxUDPDatagramSize)
	}
	if got := clampUDPMaxPacketSize(maxUDPDatagramSize + 1); got != maxUDPDatagramSize {
		t.Fatalf("oversized max packet size = %d, want %d", got, maxUDPDatagramSize)
	}
	if got := clampUDPMaxPacketSize(1280); got != 1280 {
		t.Fatalf("max packet size = %d, want 1280", got)
	}
	if got := udpMaxPacketSizeFromMTU(4096); got != 4096 {
		t.Fatalf("MTU max packet size = %d, want 4096", got)
	}
	if got := udpReadBufferSize(1280); got != 1281 {
		t.Fatalf("read buffer size = %d, want 1281", got)
	}
	if got := udpReadBufferSize(maxUDPDatagramSize); got != maxUDPDatagramSize {
		t.Fatalf("max read buffer size = %d, want %d", got, maxUDPDatagramSize)
	}
}

func TestManagerNilLoggerNormalized(t *testing.T) {
	mgr := New(ConfigObj{Node: &mockNodeObj{}, UDPTimeout: time.Second})
	if mgr.log == nil {
		t.Fatal("expected nil logger to be normalized")
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start with nil logger: %v", err)
	}
}

func TestStart_invalidSessionTimeout(t *testing.T) {
	mgr := newTestManagerObj(&mockNodeObj{}, 0, ConfigObj{})
	if err := mgr.AddLocalUDP(UDPMappingObj{
		Listen: &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
		Mapped: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
	}); err != nil {
		t.Fatal(err)
	}
	err := mgr.Start(context.Background())
	if !errors.Is(err, ErrInvalidSessionTimeout) {
		t.Fatalf("Start = %v, want ErrInvalidSessionTimeout", err)
	}
}

func TestStart_invalidUDPMapping(t *testing.T) {
	mgr := newTestManagerObj(&mockNodeObj{}, time.Second, ConfigObj{})
	if err := mgr.AddLocalUDP(UDPMappingObj{Mapped: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}}); err != nil {
		t.Fatal(err)
	}
	err := mgr.Start(context.Background())
	if !errors.Is(err, ErrInvalidMapping) {
		t.Fatalf("Start = %v, want ErrInvalidMapping", err)
	}
}

func TestStartLocalUDP_bindErrorReturned(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	addr := conn.LocalAddr().(*net.UDPAddr)
	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{})
	if err := mgr.AddLocalUDP(UDPMappingObj{
		Listen: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: addr.Port},
		Mapped: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
	}); err != nil {
		t.Fatal(err)
	}

	if err = mgr.Start(context.Background()); err == nil {
		t.Fatal("Start returned nil for occupied UDP listen address")
	}
}

func TestRunUDPLoop_nilLoggerInvalidTimeoutDoesNotPanic(t *testing.T) {
	RunUDPLoop(context.Background(), UDPLoopConfigObj{})
}

// //

func TestReverseProxyUDP_forwardsData(t *testing.T) {
	// dst listens on UDP; src is a net.Conn wrapping a pair
	dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstConn.Close() }()

	srcConn, srcWriter := net.Pipe()
	defer func() { _ = srcConn.Close() }()
	defer func() { _ = srcWriter.Close() }()

	dstAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: dstConn.LocalAddr().(*net.UDPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	activity := make(chan struct{}, 1)
	go ReverseProxyUDP(ctx, UDPReverseConfigObj{
		Dst:           dstConn,
		DstAddr:       dstAddr,
		Src:           srcConn,
		MaxPacketSize: 4096,
		Activity: func() {
			activity <- struct{}{}
		},
	})

	// Write to srcWriter → should appear on dstConn
	msg := []byte("reverse-udp-test")
	if _, err := srcWriter.Write(msg); err != nil {
		t.Fatalf("write to src: %v", err)
	}

	buf := make([]byte, 128)
	if err = dstConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	n, _, err := dstConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read from dst: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("expected %q, got %q", msg, buf[:n])
	}
	select {
	case <-activity:
	default:
		t.Fatal("activity callback was not called")
	}
}

func TestReverseProxyUDP_dropsOversizedPacket(t *testing.T) {
	dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstConn.Close() }()

	srcConn, srcWriter := net.Pipe()
	defer func() { _ = srcConn.Close() }()
	defer func() { _ = srcWriter.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ReverseProxyUDP(ctx, UDPReverseConfigObj{
		Dst:           dstConn,
		DstAddr:       dstConn.LocalAddr(),
		Src:           srcConn,
		MaxPacketSize: 8,
	})

	if _, err := srcWriter.Write([]byte("123456789")); err != nil {
		t.Fatalf("write oversized: %v", err)
	}
	buf := make([]byte, 16)
	if err = dstConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err = dstConn.ReadFrom(buf); err == nil {
		t.Fatal("oversized reverse packet should be dropped")
	}

	if _, err := srcWriter.Write([]byte("12345678")); err != nil {
		t.Fatalf("write exact-size: %v", err)
	}
	if err = dstConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	n, _, err := dstConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read exact-size packet: %v", err)
	}
	if string(buf[:n]) != "12345678" {
		t.Fatalf("unexpected exact-size packet %q", buf[:n])
	}
}

func TestReverseProxyUDP_stopsOnContextCancel(t *testing.T) {
	dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstConn.Close() }()

	srcConn, srcWriter := net.Pipe()
	defer func() { _ = srcConn.Close() }()
	defer func() { _ = srcWriter.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ReverseProxyUDP(ctx, UDPReverseConfigObj{Dst: dstConn, DstAddr: dstConn.LocalAddr(), Src: srcConn, MaxPacketSize: 4096})
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("ReverseProxyUDP did not stop after context cancel")
	}
}

func TestReverseProxyUDP_stopsOnSrcClose(t *testing.T) {
	dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dstConn.Close() }()

	srcConn, srcWriter := net.Pipe()

	done := make(chan struct{})
	ctx := context.Background()
	go func() {
		ReverseProxyUDP(ctx, UDPReverseConfigObj{Dst: dstConn, DstAddr: dstConn.LocalAddr(), Src: srcConn, MaxPacketSize: 4096})
		close(done)
	}()

	if err = srcWriter.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("ReverseProxyUDP did not stop after src close")
	}
}

// //

func TestRunUDPLoop_echoRoundtrip(t *testing.T) {
	echoAddr := echoUDPServer(t)

	// Local UDP listener (plays the role of "Yggdrasil" side)
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial:       func(context.Context, net.Addr) (net.Conn, error) { return net.DialUDP("udp4", nil, echoAddr) },
		Timeout:    2 * time.Second,
	})

	// Simulate a client: dial listenConn directly
	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()

	msg := []byte("udp-loop-test")
	got := readUDPEchoWithRetry(t, clientConn, msg)
	if string(got) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", got, msg)
	}
}

func TestManagerUDPStandaloneAppliesSafeDefaults(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		// Zero MaxSessions must resolve to a safe default on the standalone
		// entrypoint, not "unlimited"; the loop must still relay traffic.
		RunUDPLoop(ctx, UDPLoopConfigObj{
			Logger:     noopLogObj{},
			ListenConn: listenConn,
			Dial:       func(context.Context, net.Addr) (net.Conn, error) { return net.DialUDP("udp4", nil, echoAddr) },
			Timeout:    2 * time.Second,
		})
	}()

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()

	if got := readUDPEchoWithRetry(t, clientConn, []byte("active")); string(got) != "active" {
		t.Fatalf("echo mismatch: got %q, want %q", got, "active")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("UDP loop did not stop after cancel")
	}
}

func TestRunUDPLoop_forwardsFirstPacket(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial:       func(context.Context, net.Addr) (net.Conn, error) { return net.DialUDP("udp4", nil, echoAddr) },
		Timeout:    2 * time.Second,
	})

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()

	msg := []byte("first-packet")
	if _, err = clientConn.Write(msg); err != nil {
		t.Fatalf("write first packet: %v", err)
	}
	if err = clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buf := make([]byte, 128)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("first packet was not forwarded: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("echo mismatch: got %q, want %q", buf[:n], msg)
	}
}

func TestRunUDPLoop_defaultBufferStillForwards(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial:       func(context.Context, net.Addr) (net.Conn, error) { return net.DialUDP("udp4", nil, echoAddr) },
		Timeout:    2 * time.Second,
	})

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()

	msg := []byte("zero-mtu")
	got := readUDPEchoWithRetry(t, clientConn, msg)
	if string(got) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", got, msg)
	}
}

func TestRunUDPLoop_dropsOversizedPacketBeforeDial(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dialed := make(chan struct{}, 1)
	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial: func(context.Context, net.Addr) (net.Conn, error) {
			dialed <- struct{}{}
			return newRecordingConnObj(), nil
		},
		MaxPacketSize: 8,
		Timeout:       time.Second,
	})

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()
	if _, err := clientConn.Write([]byte("123456789")); err != nil {
		t.Fatalf("write oversized: %v", err)
	}

	select {
	case <-dialed:
		t.Fatal("oversized UDP packet should be dropped before dialing")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunUDPLoop_dropsOversizedPacketForExistingSession(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	upstream := newRecordingConnObj()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:        noopLogObj{},
		ListenConn:    listenConn,
		Dial:          func(context.Context, net.Addr) (net.Conn, error) { return upstream, nil },
		MaxPacketSize: 8,
		Timeout:       time.Second,
	})

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()
	got := writeUDPUntilRecorded(t, clientConn, upstream, []byte("12345678"))
	if string(got) != "12345678" {
		t.Fatalf("unexpected first write %q", got)
	}

	if _, err := clientConn.Write([]byte("123456789")); err != nil {
		t.Fatalf("write oversized: %v", err)
	}
	select {
	case got := <-upstream.writes:
		t.Fatalf("oversized packet was forwarded as %q", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunUDPLoop_asyncDialDoesNotBlockOtherSources(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondUpstream := newRecordingConnObj()
	var calls atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial: func(ctx context.Context, _ net.Addr) (net.Conn, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				select {
				case <-releaseFirst:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return newRecordingConnObj(), nil
			}
			return secondUpstream, nil
		},
		DialTimeout: time.Second,
		Timeout:     5 * time.Second,
		MaxSessions: 2,
	})

	addr := listenConn.LocalAddr().(*net.UDPAddr)
	firstClient, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstClient.Close() }()
	if _, err = firstClient.Write([]byte("blocked")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first dial did not start")
	}

	secondClient, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondClient.Close() }()
	got := writeUDPUntilRecorded(t, secondClient, secondUpstream, []byte("second"))
	if string(got) != "second" {
		t.Fatalf("unexpected second upstream write %q", got)
	}
	close(releaseFirst)
}

func TestRunUDPLoop_sessionTimeoutCancelsInFlightDial(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	dialStarted := make(chan struct{})
	dialDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial: func(ctx context.Context, _ net.Addr) (net.Conn, error) {
			close(dialStarted)
			<-ctx.Done()
			dialDone <- ctx.Err()
			return nil, ctx.Err()
		},
		DialTimeout: -1,
		Timeout:     20 * time.Millisecond,
		MaxSessions: 1,
	})

	client, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if _, err = client.Write([]byte("blocked")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-dialStarted:
	case <-time.After(time.Second):
		t.Fatal("dial did not start")
	}
	select {
	case err = <-dialDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dial ended with %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session timeout did not cancel in-flight dial")
	}
}

func TestRunUDPLoop_invalidTimeoutReturns(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	done := make(chan struct{})
	go func() {
		RunUDPLoop(context.Background(), UDPLoopConfigObj{
			Logger:     noopLogObj{},
			ListenConn: listenConn,
			Dial:       func(context.Context, net.Addr) (net.Conn, error) { return nil, nil },
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunUDPLoop did not return on invalid timeout")
	}
}

func TestRunUDPLoop_sessionTimeout(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Very short timeout to trigger session cleanup
	const sessionTimeout = 100 * time.Millisecond
	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial:       func(context.Context, net.Addr) (net.Conn, error) { return net.DialUDP("udp4", nil, echoAddr) },
		Timeout:    sessionTimeout,
	})

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()

	_ = readUDPEchoWithRetry(t, clientConn, []byte("x"))

	// Wait for session to expire
	time.Sleep(sessionTimeout * 6)
	// No panic or deadlock — test passes
}

func TestRunUDPLoop_maxSessions(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dials := make(chan *recordingConnObj, 2)

	go RunUDPLoop(ctx, UDPLoopConfigObj{
		Logger:     noopLogObj{},
		ListenConn: listenConn,
		Dial: func(context.Context, net.Addr) (net.Conn, error) {
			c := newRecordingConnObj()
			dials <- c
			return c, nil
		},
		Timeout:     5 * time.Second,
		MaxSessions: 1,
	})

	addr := listenConn.LocalAddr().(*net.UDPAddr)

	// First client
	c1, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c1.Close() }()
	var firstUpstream *recordingConnObj
	if _, err = c1.Write([]byte("first")); err != nil {
		t.Fatalf("write first primer: %v", err)
	}
	select {
	case firstUpstream = <-dials:
	case <-time.After(time.Second):
		t.Fatal("first session did not dial upstream")
	}
	got := writeUDPUntilRecorded(t, c1, firstUpstream, []byte("first"))
	if string(got) != "first" {
		t.Fatalf("unexpected first upstream write %q", got)
	}

	// Second client (different source port → different session → should be dropped)
	c2, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	if _, err = c2.Write([]byte("second")); err != nil {
		t.Fatalf("write second: %v", err)
	}
	select {
	case <-dials:
		t.Fatal("second session should be dropped before dialing upstream")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunUDPLoop_cancelStops(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		RunUDPLoop(ctx, UDPLoopConfigObj{
			Logger:     noopLogObj{},
			ListenConn: listenConn,
			Dial:       func(context.Context, net.Addr) (net.Conn, error) { return net.DialUDP("udp4", nil, echoAddr) },
			Timeout:    5 * time.Second,
		})
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("RunUDPLoop did not stop after context cancel")
	}
}

func TestRunUDPLoopWithWait_waitsForSessionWorkers(t *testing.T) {
	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listenConn.Close() }()

	upstream := newBlockingUDPConnObj()
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		runUDPLoopWithWait(ctx, UDPLoopConfigObj{
			Logger:      noopLogObj{},
			ListenConn:  listenConn,
			Dial:        func(context.Context, net.Addr) (net.Conn, error) { return upstream, nil },
			Timeout:     5 * time.Second,
			MaxSessions: 10,
		}, &wg)
		close(loopDone)
	}()

	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientConn.Close() }()
	writeUDPUntilSignal(t, clientConn, upstream.wrote, []byte("session"))

	cancel()
	_ = listenConn.Close()
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("runUDPLoopWithWait did not return after cancel")
	}

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("tracked UDP workers did not stop")
	}
	select {
	case <-upstream.readDone:
	default:
		t.Fatal("Wait returned before reverse worker exited")
	}
}

func TestUDPSessionCountHeldUntilReverseReaderExits(t *testing.T) {
	dst, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dst.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := newStubbornReadConnObj()
	var count atomic.Int64
	count.Store(1)
	sessionCtx, sessionCancel := context.WithCancel(ctx)
	session := &udpSessionObj{
		ctx:     sessionCtx,
		cancel:  sessionCancel,
		out:     make(chan *udpPacketObj, 1),
		counter: &count,
	}
	key := netip.MustParseAddrPort("127.0.0.1:1234")
	sessions := newUDPSessionMap()
	sessions.store(key, session)
	pool := newUDPBufferPool(512)
	reverseWriter := newUDPReverseWriter(ctx, dst, time.Second, pool, 512)
	var wg sync.WaitGroup
	startUDPSessionWorker(ctx, UDPLoopConfigObj{
		ListenConn: dst,
		Dial:       func(context.Context, net.Addr) (net.Conn, error) { return upstream, nil },
	}, sessions, key, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}, session, pool, reverseWriter, 512, &wg, noopLogObj{})

	select {
	case <-upstream.readStarted:
	case <-time.After(time.Second):
		t.Fatal("reverse reader did not start")
	}
	session.stop()
	time.Sleep(20 * time.Millisecond)
	if got := count.Load(); got != 1 {
		t.Fatalf("session count released while reverse reader is alive: %d", got)
	}
	close(upstream.release)
	wg.Wait()
	if got := count.Load(); got != 0 {
		t.Fatalf("session count after reverse reader exit = %d, want 0", got)
	}
}

// //

func BenchmarkReverseProxyUDP(b *testing.B) {
	msg := []byte("bench-payload")
	for b.Loop() {
		dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		if err != nil {
			b.Fatalf("listen UDP: %v", err)
		}
		srcConn, srcWriter := net.Pipe()

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			ReverseProxyUDP(ctx, UDPReverseConfigObj{Dst: dstConn, DstAddr: dstConn.LocalAddr(), Src: srcConn})
			close(done)
		}()

		if _, err = srcWriter.Write(msg); err != nil {
			b.Fatalf("write: %v", err)
		}
		if err = dstConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			b.Fatalf("set read deadline: %v", err)
		}
		buf := make([]byte, 128)
		if _, _, err = dstConn.ReadFrom(buf); err != nil {
			b.Fatalf("read: %v", err)
		}

		cancel()
		_ = srcWriter.Close()
		<-done
		_ = dstConn.Close()
	}
}

// BenchmarkUDPSessionRouting exercises the per-datagram hot path: derive the NAT
// key from the source address and look up its session. It must stay at 0 allocs/op
// — the typed netip.AddrPort key must never be boxed into interface{}.
func BenchmarkUDPSessionRouting(b *testing.B) {
	addr := &net.UDPAddr{IP: net.ParseIP("200:1234::1"), Port: 5000}
	sessions := newUDPSessionMap()
	key, ok := udpSessionKey(addr)
	if !ok {
		b.Fatal("udpSessionKey failed")
	}
	sessions.store(key, &udpSessionObj{})

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		k, keyOK := udpSessionKey(addr)
		if !keyOK {
			b.Fatal("udpSessionKey failed")
		}
		if _, found := sessions.load(k); !found {
			b.Fatal("session not found")
		}
	}
}

func BenchmarkUDPBufferPool(b *testing.B) {
	const packetSize = 1200

	pool := newUDPBufferPool(maxUDPDatagramSize)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		packet := pool.get(packetSize)
		packet.buf[0] = 1
		pool.put(packet)
	}
}
