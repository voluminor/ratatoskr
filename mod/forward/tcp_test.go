package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// // // // // // // // // //

type blockingDialNodeObj struct {
	mockNodeObj
	started chan struct{}
}

func (n *blockingDialNodeObj) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	select {
	case n.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func dialTCP6WithRetry(t *testing.T, addr string) net.Conn {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp6", addr, 100*time.Millisecond)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("dial TCP: %v", err)
	return nil
}

func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func tcpConnPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen TCP pair: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	acceptCh := make(chan *net.TCPConn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.AcceptTCP()
		if err != nil {
			errCh <- err
			return
		}
		acceptCh <- conn
	}()
	client, err := net.DialTCP("tcp4", nil, ln.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatalf("dial TCP pair: %v", err)
	}
	select {
	case server := <-acceptCh:
		return client, server
	case err := <-errCh:
		_ = client.Close()
		t.Fatalf("accept TCP pair: %v", err)
	case <-time.After(time.Second):
		_ = client.Close()
		t.Fatal("accept TCP pair timed out")
	}
	return nil, nil
}

// //

func TestProxyTCP_bidirectional(t *testing.T) {
	// c1↔s1 is the proxy pair; c2/s2 are the user-facing ends
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()

	go ProxyTCP(c1, s1, 100*time.Millisecond)

	writeErr := make(chan error, 1)
	go func() {
		_, err := c2.Write([]byte("hello"))
		writeErr <- err
	}()

	buf := make([]byte, 5)
	if _, err := io.ReadFull(s2, buf); err != nil {
		t.Fatalf("read c2→s2: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("expected 'hello', got %q", buf)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write c2: %v", err)
	}

	writeErr = make(chan error, 1)
	go func() {
		_, err := s2.Write([]byte("world"))
		writeErr <- err
	}()

	if _, err := io.ReadFull(c2, buf); err != nil {
		t.Fatalf("read s2→c2: %v", err)
	}
	if string(buf) != "world" {
		t.Errorf("expected 'world', got %q", buf)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write s2: %v", err)
	}

	_ = c2.Close()
	_ = s2.Close()
}

func TestProxyTCPContext_stopsOnContextCancel(t *testing.T) {
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()
	defer func() { _ = c2.Close() }()
	defer func() { _ = s2.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ProxyTCPContext(ctx, c1, s1, time.Second)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("ProxyTCPContext did not stop after context cancel")
	}
}

func TestProxyTCP_closeTimeout(t *testing.T) {
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()

	done := make(chan struct{})
	go func() {
		ProxyTCP(c1, s1, 10*time.Millisecond)
		close(done)
	}()

	_ = c2.Close()
	_ = s2.Close()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("ProxyTCP did not return within timeout")
	}
}

func TestProxyTCP_largePayload(t *testing.T) {
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()
	go ProxyTCP(c1, s1, 100*time.Millisecond)

	payload := make([]byte, 1<<16) // 64 KiB
	for i := range payload {
		payload[i] = byte(i)
	}

	writeErr := make(chan error, 1)
	go func() {
		_, err := c2.Write(payload)
		_ = c2.Close()
		writeErr <- err
	}()

	received, err := io.ReadAll(s2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(received) != len(payload) {
		t.Errorf("expected %d bytes, got %d", len(payload), len(received))
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write payload: %v", err)
	}
	_ = s2.Close()
}

func TestProxyTCP_halfCloseAllowsResponse(t *testing.T) {
	client, proxyClient := tcpConnPair(t)
	proxyServer, server := tcpConnPair(t)
	defer func() { _ = client.Close() }()
	defer func() { _ = proxyClient.Close() }()
	defer func() { _ = proxyServer.Close() }()
	defer func() { _ = server.Close() }()

	done := make(chan struct{})
	go func() {
		ProxyTCP(proxyClient, proxyServer, 500*time.Millisecond)
		close(done)
	}()

	response := make([]byte, 64*1024)
	for i := range response {
		response[i] = byte(i)
	}
	serverDone := make(chan error, 1)
	go func() {
		req, err := io.ReadAll(server)
		if err != nil {
			serverDone <- err
			return
		}
		if string(req) != "request" {
			serverDone <- fmt.Errorf("request = %q", req)
			return
		}
		if _, err = server.Write(response); err != nil {
			serverDone <- err
			return
		}
		serverDone <- server.CloseWrite()
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	got, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(got) != string(response) {
		t.Fatalf("response mismatch: got %d bytes, want %d", len(got), len(response))
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProxyTCP did not finish after half-close exchange")
	}
}

func TestProxyTCP_halfCloseTimeout(t *testing.T) {
	client, proxyClient := tcpConnPair(t)
	proxyServer, server := tcpConnPair(t)
	defer func() { _ = client.Close() }()
	defer func() { _ = proxyClient.Close() }()
	defer func() { _ = proxyServer.Close() }()
	defer func() { _ = server.Close() }()

	done := make(chan struct{})
	go func() {
		ProxyTCP(proxyClient, proxyServer, 50*time.Millisecond)
		close(done)
	}()

	serverReadDone := make(chan error, 1)
	go func() {
		req, err := io.ReadAll(server)
		if err != nil {
			serverReadDone <- err
			return
		}
		if string(req) != "request" {
			serverReadDone <- fmt.Errorf("request = %q", req)
			return
		}
		serverReadDone <- nil
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := <-serverReadDone; err != nil {
		t.Fatalf("server read: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProxyTCP did not return after closeTimeout")
	}
}

func TestProxyTCP_halfCloseTimeoutIsIdle(t *testing.T) {
	client, proxyClient := tcpConnPair(t)
	proxyServer, server := tcpConnPair(t)
	defer func() { _ = client.Close() }()
	defer func() { _ = proxyClient.Close() }()
	defer func() { _ = proxyServer.Close() }()
	defer func() { _ = server.Close() }()

	done := make(chan struct{})
	go func() {
		ProxyTCP(proxyClient, proxyServer, 200*time.Millisecond)
		close(done)
	}()

	serverDone := make(chan error, 1)
	go func() {
		req, err := io.ReadAll(server)
		if err != nil {
			serverDone <- err
			return
		}
		if string(req) != "request" {
			serverDone <- fmt.Errorf("request = %q", req)
			return
		}
		for _, b := range []byte("stream") {
			if _, err = server.Write([]byte{b}); err != nil {
				serverDone <- err
				return
			}
			time.Sleep(60 * time.Millisecond)
		}
		serverDone <- server.CloseWrite()
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("client CloseWrite: %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	got, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(got) != "stream" {
		t.Fatalf("response = %q, want stream", got)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProxyTCP did not finish after streaming half-close response")
	}
}

func TestProxyTCP_idleTimeoutClosesIdleSession(t *testing.T) {
	client, proxyClient := tcpConnPair(t)
	proxyServer, server := tcpConnPair(t)
	defer func() { _ = client.Close() }()
	defer func() { _ = proxyClient.Close() }()
	defer func() { _ = proxyServer.Close() }()
	defer func() { _ = server.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		proxyTCPContext(ctx, proxyClient, proxyServer, time.Second, 50*time.Millisecond)
		close(done)
	}()

	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected idle timeout to close local side")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy did not return after idle timeout")
	}
}

// //

func TestNewLocalTCP_echoRoundtrip(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	newRunningTestObj(t, node, 5*time.Second, ConfigObj{
		LocalTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
			Mapped: echo,
		}},
	})

	addr := fmt.Sprintf("[::1]:%d", localPort)
	conn := dialTCP6WithRetry(t, addr)
	defer func() { _ = conn.Close() }()

	msg := []byte("forward-test")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf, msg)
	}
}

func TestNewLocalTCP_dialDoesNotBlockAcceptLoop(t *testing.T) {
	checkIPv6(t)
	localPort := freePort(t, "::1")
	node := &blockingDialNodeObj{
		mockNodeObj: mockNodeObj{addr: net.ParseIP("::1")},
		started:     make(chan struct{}, 2),
	}
	newRunningTestObj(t, node, 5*time.Second, ConfigObj{
		DialTimeout:       time.Second,
		MaxTCPConnections: 2,
		LocalTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
			Mapped: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1},
		}},
	})

	addr := fmt.Sprintf("[::1]:%d", localPort)
	c1 := dialTCP6WithRetry(t, addr)
	defer func() { _ = c1.Close() }()
	select {
	case <-node.started:
	case <-time.After(time.Second):
		t.Fatal("first backend dial did not start")
	}

	c2 := dialTCP6WithRetry(t, addr)
	defer func() { _ = c2.Close() }()
	select {
	case <-node.started:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("second backend dial was blocked by the first dial")
	}
}

func TestNewLocalTCP_maxConnectionsIsSharedAcrossMappings(t *testing.T) {
	checkIPv6(t)
	firstPort := freePort(t, "::1")
	secondPort := freePort(t, "::1")
	for secondPort == firstPort {
		secondPort = freePort(t, "::1")
	}
	node := &blockingDialNodeObj{
		mockNodeObj: mockNodeObj{addr: net.ParseIP("::1")},
		started:     make(chan struct{}, 2),
	}
	newRunningTestObj(t, node, 5*time.Second, ConfigObj{
		DialTimeout:       time.Second,
		MaxTCPConnections: 1,
		LocalTCP: []TCPMappingObj{
			{
				Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: firstPort},
				Mapped: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1},
			},
			{
				Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: secondPort},
				Mapped: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 2},
			},
		},
	})

	c1 := dialTCP6WithRetry(t, fmt.Sprintf("[::1]:%d", firstPort))
	defer func() { _ = c1.Close() }()
	select {
	case <-node.started:
	case <-time.After(time.Second):
		t.Fatal("first backend dial did not start")
	}

	c2 := dialTCP6WithRetry(t, fmt.Sprintf("[::1]:%d", secondPort))
	defer func() { _ = c2.Close() }()
	select {
	case <-node.started:
		t.Fatal("connection limit allowed an extra backend dial")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCloseLocalTCP_closesActiveProxy(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newRunningTestObj(t, node, 5*time.Second, ConfigObj{
		LocalTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
			Mapped: echo,
		}},
	})

	conn := dialTCP6WithRetry(t, fmt.Sprintf("[::1]:%d", localPort))
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatalf("write before cancel: %v", err)
	}
	buf := make([]byte, 1)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read before cancel: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline after cancel: %v", err)
	}
	_, err := conn.Read(buf)
	if err == nil || isTimeoutErr(err) {
		t.Fatalf("active proxy stayed open after cancel: %v", err)
	}
}

func TestCloseLocalTCP_stopsListener(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newRunningTestObj(t, node, 5*time.Second, ConfigObj{
		LocalTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
			Mapped: echo,
		}},
	})

	// Wait for listener to start
	addr := fmt.Sprintf("[::1]:%d", localPort)
	c := dialTCP6WithRetry(t, addr)
	_ = c.Close()

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewLocalTCP_bindErrorReturned(t *testing.T) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().(*net.TCPAddr)
	node := &mockNodeObj{addr: net.ParseIP("::1")}
	_, err = New(ConfigObj{
		Logger:     noopLogObj{},
		Node:       node,
		UDPTimeout: 5 * time.Second,
		LocalTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: addr.Port},
			Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
		}},
	})
	if err == nil {
		t.Fatal("New returned nil for occupied TCP listen address")
	}
}

func TestNew_bindFailureClosesEarlierListeners(t *testing.T) {
	first := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: freePort(t, "127.0.0.1")}
	occupied, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()

	_, err = New(ConfigObj{
		Node: &mockNodeObj{},
		LocalTCP: []TCPMappingObj{
			{Listen: first, Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}},
			{Listen: occupied.Addr().(*net.TCPAddr), Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2}},
		},
	})
	if err == nil {
		t.Fatal("New succeeded despite an occupied second listener")
	}

	listener, err := net.ListenTCP("tcp", first)
	if err != nil {
		t.Fatalf("first listener leaked after rollback: %v", err)
	}
	_ = listener.Close()
}

func TestNew_udpBindFailureClosesEarlierTCPListener(t *testing.T) {
	first := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: freePort(t, "127.0.0.1")}
	occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()

	_, err = New(ConfigObj{
		Node:       &mockNodeObj{},
		UDPTimeout: time.Second,
		LocalTCP: []TCPMappingObj{{
			Listen: first,
			Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
		}},
		LocalUDP: []UDPMappingObj{{
			Listen: occupied.LocalAddr().(*net.UDPAddr),
			Mapped: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
		}},
	})
	if err == nil {
		t.Fatal("New succeeded despite an occupied UDP listener")
	}

	listener, err := net.ListenTCP("tcp", first)
	if err != nil {
		t.Fatalf("earlier TCP listener leaked after UDP rollback: %v", err)
	}
	_ = listener.Close()
}

func TestNewTCPOnlyDoesNotRequireUDPTimeout(t *testing.T) {
	node := &mockNodeObj{addr: net.ParseIP("::1")}
	newRunningTestObj(t, node, 0, ConfigObj{
		LocalTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
			Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
		}},
	})
}

func TestNew_invalidTCPMapping(t *testing.T) {
	_, err := New(ConfigObj{
		Node:     &mockNodeObj{},
		LocalTCP: []TCPMappingObj{{Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}}},
	})
	if !errors.Is(err, ErrInvalidMapping) {
		t.Fatalf("New = %v, want ErrInvalidMapping", err)
	}
}

// //

func TestNewRemoteTCP_echoRoundtrip(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	remotePort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	newRunningTestObj(t, node, 5*time.Second, ConfigObj{
		RemoteTCP: []TCPMappingObj{{
			Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: remotePort},
			Mapped: echo,
		}},
	})

	addr := fmt.Sprintf("[::1]:%d", remotePort)
	conn := dialTCP6WithRetry(t, addr)
	defer func() { _ = conn.Close() }()

	msg := []byte("remote-forward-test")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf, msg)
	}
}

// //

func TestClose_idempotent(t *testing.T) {
	mgr := newRunningTestObj(t, &mockNodeObj{}, time.Second, ConfigObj{})
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestClose_zeroValue(t *testing.T) {
	var obj Obj
	if err := obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := obj.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestNew_requiresNode(t *testing.T) {
	if _, err := New(ConfigObj{}); !errors.Is(err, ErrNodeRequired) {
		t.Fatalf("New error = %v, want ErrNodeRequired", err)
	}
}

func TestIOErrorStreakTerminatesClosedAndRepeatedErrors(t *testing.T) {
	var streak ioErrorStreakObj
	if !streak.terminal(net.ErrClosed) {
		t.Fatal("net.ErrClosed must be terminal immediately")
	}
	streak.reset()
	err := errors.New("permanent abort")
	for i := 1; i < terminalErrorLimit; i++ {
		if streak.terminal(err) {
			t.Fatalf("error became terminal after %d failures", i)
		}
	}
	if !streak.terminal(err) {
		t.Fatalf("error did not become terminal after %d failures", terminalErrorLimit)
	}
}

func TestIOErrorStreakNeverTerminatesRetryableResourceErrors(t *testing.T) {
	for _, errno := range []error{syscall.EMFILE, syscall.ENFILE, syscall.ENOBUFS, syscall.ECONNABORTED} {
		t.Run(errno.Error(), func(t *testing.T) {
			var streak ioErrorStreakObj
			err := &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept4", errno)}
			for i := 0; i < terminalErrorLimit*3; i++ {
				if streak.terminal(err) {
					t.Fatalf("retryable error became terminal after %d attempts: %v", i+1, err)
				}
			}
		})
	}
}

func BenchmarkProxyTCP(b *testing.B) {
	payload := make([]byte, 4096)
	for b.Loop() {
		c1, c2 := net.Pipe()
		s1, s2 := net.Pipe()
		done := make(chan struct{})
		go func() { ProxyTCP(c1, s1, 10*time.Millisecond); close(done) }()
		if _, err := c2.Write(payload); err != nil {
			b.Fatalf("write: %v", err)
		}
		_ = c2.Close()
		_ = s2.Close()
		<-done
	}
}
