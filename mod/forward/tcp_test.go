package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// // // // // // // // // //

func TestProxyTCP_bidirectional(t *testing.T) {
	// c1↔s1 is the proxy pair; c2/s2 are the user-facing ends
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()

	go ProxyTCP(c1, s1, 100*time.Millisecond)

	go func() { c2.Write([]byte("hello")) }()

	buf := make([]byte, 5)
	if _, err := io.ReadFull(s2, buf); err != nil {
		t.Fatalf("read c2→s2: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("expected 'hello', got %q", buf)
	}

	go func() { s2.Write([]byte("world")) }()

	if _, err := io.ReadFull(c2, buf); err != nil {
		t.Fatalf("read s2→c2: %v", err)
	}
	if string(buf) != "world" {
		t.Errorf("expected 'world', got %q", buf)
	}

	c2.Close()
	s2.Close()
}

func TestProxyTCP_closeTimeout(t *testing.T) {
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()

	done := make(chan struct{})
	go func() {
		ProxyTCP(c1, s1, 10*time.Millisecond)
		close(done)
	}()

	c2.Close()
	s2.Close()

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

	go func() {
		c2.Write(payload)
		c2.Close()
	}()

	received, err := io.ReadAll(s2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(received) != len(payload) {
		t.Errorf("expected %d bytes, got %d", len(payload), len(received))
	}
	s2.Close()
}

// //

func TestStartLocalTCP_echoRoundtrip(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := New(noopLogObj{}, 5*time.Second)
	mgr.SetTCPCloseTimeout(50 * time.Millisecond)
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: echo,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mgr.Start(ctx, node)
	defer func() { cancel(); mgr.Wait() }()

	addr := fmt.Sprintf("[::1]:%d", localPort)
	var conn net.Conn
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp6", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial local TCP: %v", err)
	}
	defer conn.Close()

	msg := []byte("forward-test")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf, msg)
	}
}

func TestStartLocalTCP_cancelStopsListener(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	localPort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := New(noopLogObj{}, 5*time.Second)
	mgr.AddLocalTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: localPort},
		Mapped: echo,
	})

	ctx, cancel := context.WithCancel(context.Background())
	mgr.Start(ctx, node)

	// Wait for listener to start
	addr := fmt.Sprintf("[::1]:%d", localPort)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp6", addr, 50*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	done := make(chan struct{})
	go func() { mgr.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("mgr.Wait() timed out after context cancel")
	}
}

// //

func TestStartRemoteTCP_echoRoundtrip(t *testing.T) {
	checkIPv6(t)
	echo := echoTCPServer6(t)
	remotePort := freePort(t, "::1")

	node := &mockNodeObj{addr: net.ParseIP("::1")}
	mgr := New(noopLogObj{}, 5*time.Second)
	mgr.SetTCPCloseTimeout(50 * time.Millisecond)
	mgr.AddRemoteTCP(TCPMappingObj{
		Listen: &net.TCPAddr{IP: net.ParseIP("::1"), Port: remotePort},
		Mapped: echo,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mgr.Start(ctx, node)
	defer func() { cancel(); mgr.Wait() }()

	addr := fmt.Sprintf("[::1]:%d", remotePort)
	var conn net.Conn
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp6", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial remote TCP: %v", err)
	}
	defer conn.Close()

	msg := []byte("remote-forward-test")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
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
		c2.Write(payload)
		c2.Close()
		s2.Close()
		<-done
	}
}
