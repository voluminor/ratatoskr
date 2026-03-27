package forward

import (
	"context"
	"net"
	"testing"
	"time"
)

// // // // // // // // // //

// echoUDPServer starts a UDP echo server on 127.0.0.1:0 and returns its address
func echoUDPServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("echoUDPServer: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], addr)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

// //

func TestReverseProxyUDP_forwardsData(t *testing.T) {
	// dst listens on UDP; src is a net.Conn wrapping a pair
	dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close()

	srcConn, srcWriter := net.Pipe()
	defer srcConn.Close()
	defer srcWriter.Close()

	dstAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: dstConn.LocalAddr().(*net.UDPAddr).Port}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go ReverseProxyUDP(ctx, 4096, dstConn, dstAddr, srcConn)

	// Write to srcWriter → should appear on dstConn
	msg := []byte("reverse-udp-test")
	if _, err := srcWriter.Write(msg); err != nil {
		t.Fatalf("write to src: %v", err)
	}

	buf := make([]byte, 128)
	dstConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := dstConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read from dst: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("expected %q, got %q", msg, buf[:n])
	}
}

func TestReverseProxyUDP_stopsOnContextCancel(t *testing.T) {
	dstConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close()

	srcConn, srcWriter := net.Pipe()
	defer srcWriter.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ReverseProxyUDP(ctx, 4096, dstConn, dstConn.LocalAddr(), srcConn)
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
	defer dstConn.Close()

	srcConn, srcWriter := net.Pipe()

	done := make(chan struct{})
	ctx := context.Background()
	go func() {
		ReverseProxyUDP(ctx, 4096, dstConn, dstConn.LocalAddr(), srcConn)
		close(done)
	}()

	srcWriter.Close() // closing writer signals EOF to reader
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
	defer listenConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go RunUDPLoop(ctx, noopLogObj{}, 65535, listenConn, func() (net.Conn, error) {
		return net.DialUDP("udp4", nil, echoAddr)
	}, 2*time.Second, 0)

	// Simulate a client: dial listenConn directly
	clientConn, err := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	msg := []byte("udp-loop-test")
	if _, err := clientConn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 128)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf[:n], msg)
	}
}

func TestRunUDPLoop_sessionTimeout(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listenConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Very short timeout to trigger session cleanup
	const sessionTimeout = 100 * time.Millisecond
	go RunUDPLoop(ctx, noopLogObj{}, 65535, listenConn, func() (net.Conn, error) {
		return net.DialUDP("udp4", nil, echoAddr)
	}, sessionTimeout, 0)

	clientConn, _ := net.DialUDP("udp4", nil, listenConn.LocalAddr().(*net.UDPAddr))
	defer clientConn.Close()

	clientConn.Write([]byte("x"))
	clientConn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 8)
	clientConn.Read(buf) // wait for echo

	// Wait for session to expire
	time.Sleep(sessionTimeout * 6)
	// No panic or deadlock — test passes
}

func TestRunUDPLoop_maxSessions(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listenConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go RunUDPLoop(ctx, noopLogObj{}, 65535, listenConn, func() (net.Conn, error) {
		return net.DialUDP("udp4", nil, echoAddr)
	}, 5*time.Second, 1) // max 1 session

	addr := listenConn.LocalAddr().(*net.UDPAddr)

	// First client
	c1, _ := net.DialUDP("udp4", nil, addr)
	defer c1.Close()
	c1.Write([]byte("first"))
	c1.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 8)
	c1.Read(buf)

	// Second client (different source port → different session → should be dropped)
	c2, _ := net.DialUDP("udp4", nil, addr)
	defer c2.Close()
	c2.Write([]byte("second"))
	c2.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	n, _ := c2.Read(buf)
	_ = n
	// The second packet may or may not arrive; just verify no panic
}

func TestRunUDPLoop_cancelStops(t *testing.T) {
	echoAddr := echoUDPServer(t)

	listenConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listenConn.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		RunUDPLoop(ctx, noopLogObj{}, 65535, listenConn, func() (net.Conn, error) {
			return net.DialUDP("udp4", nil, echoAddr)
		}, 5*time.Second, 0)
		close(done)
	}()

	cancel()
	listenConn.Close() // unblock ReadFrom after cancel

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("RunUDPLoop did not stop after context cancel")
	}
}

// //

func BenchmarkReverseProxyUDP(b *testing.B) {
	msg := []byte("bench-payload")
	for b.Loop() {
		dstConn, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		srcConn, srcWriter := net.Pipe()

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			ReverseProxyUDP(ctx, 65535, dstConn, dstConn.LocalAddr(), srcConn)
			close(done)
		}()

		srcWriter.Write(msg)
		dstConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 128)
		dstConn.ReadFrom(buf)

		cancel()
		srcWriter.Close()
		<-done
		dstConn.Close()
	}
}
