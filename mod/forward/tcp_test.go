package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

func TestStartLocalTCP_echoRoundtrip(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: echo,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { cancel(); mgr.Wait() }()

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

func TestStartLocalTCP_dialDoesNotBlockAcceptLoop(t *testing.T) {
	checkIPv6(t)
	localPort := freePort(t, "::1")
	node := &blockingDialNodeObj{
		mockNodeObj: mockNodeObj{addr: net.ParseIP("::1")},
		started:     make(chan struct{}, 2),
	}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{
		DialTimeout:       time.Second,
		MaxTCPConnections: 2,
	})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { cancel(); mgr.Wait() }()

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

func TestStartLocalTCP_maxConnectionsLimitsDialFanout(t *testing.T) {
	checkIPv6(t)
	localPort := freePort(t, "::1")
	node := &blockingDialNodeObj{
		mockNodeObj: mockNodeObj{addr: net.ParseIP("::1")},
		started:     make(chan struct{}, 2),
	}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{
		DialTimeout:       time.Second,
		MaxTCPConnections: 1,
	})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { cancel(); mgr.Wait() }()

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
		t.Fatal("connection limit allowed an extra backend dial")
	case <-time.After(200 * time.Millisecond):
	}

	// The limiter registers one active session; analytics must reflect it.
	if active := mgr.ActiveTCPConnections(); active != 1 {
		t.Fatalf("ActiveTCPConnections = %d, want 1", active)
	}
}

func TestStartLocalTCP_cancelClosesActiveProxy(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: echo,
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

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

	cancel()
	done := make(chan struct{})
	go func() { mgr.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mgr.Wait() timed out after context cancel")
	}

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline after cancel: %v", err)
	}
	_, err := conn.Read(buf)
	if err == nil || isTimeoutErr(err) {
		t.Fatalf("active proxy stayed open after cancel: %v", err)
	}
}

func TestStartLocalTCP_cancelStopsListener(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: echo,
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for listener to start
	addr := fmt.Sprintf("[::1]:%d", localPort)
	c := dialTCP6WithRetry(t, addr)
	_ = c.Close()

	cancel()
	done := make(chan struct{})
	go func() { mgr.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("mgr.Wait() timed out after context cancel")
	}
}

func TestStartLocalTCP_bindErrorReturned(t *testing.T) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().(*net.TCPAddr)
	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: addr.Port},
		Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
	})

	if err = mgr.Start(context.Background()); err == nil {
		t.Fatal("Start returned nil for occupied TCP listen address")
	}
}

func TestStartTCPOnlyDoesNotRequireUDPTimeout(t *testing.T) {
	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 0, ConfigObj{})
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
		Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start TCP-only: %v", err)
	}
	cancel()
	mgr.Wait()
}

func TestStart_invalidTCPMapping(t *testing.T) {
	mgr := newTestManagerObj(&mockNodeObj{}, 0, ConfigObj{})
	mgr.AddLocalTCP(TCPMappingObj{Mapped: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}})
	err := mgr.Start(context.Background())
	if !errors.Is(err, ErrInvalidMapping) {
		t.Fatalf("Start = %v, want ErrInvalidMapping", err)
	}
}

// //

func TestStartRemoteTCP_echoRoundtrip(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	remotePort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := newTestManagerObj(node, 5*time.Second, ConfigObj{})
	mgr.AddRemoteTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: remotePort},
		Mapped: echo,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { cancel(); mgr.Wait() }()

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
