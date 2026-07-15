package forward

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
)

// // // // // // // // // //

type mockNodeObj struct {
	addr net.IP
	mtu  uint64
}

func (m *mockNodeObj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func (m *mockNodeObj) Listen(network, address string) (net.Listener, error) {
	return net.Listen(network, address)
}

func (m *mockNodeObj) ListenPacket(network, address string) (net.PacketConn, error) {
	return net.ListenPacket(network, address)
}

func (m *mockNodeObj) Address() net.IP {
	if m.addr != nil {
		return m.addr
	}
	return net.ParseIP("127.0.0.1")
}

func (m *mockNodeObj) MTU() uint64 {
	if m.mtu > 0 {
		return m.mtu
	}
	return 65535
}

// //

type noopLogObj = common.DiscardLoggerObj

// //

func newBareTestObj(node NetworkInterface, timeout time.Duration, cfg ConfigObj) *Obj {
	cfg.Logger = noopLogObj{}
	cfg.Node = node
	cfg.UDPTimeout = timeout
	obj := &Obj{}
	obj.applyConfig(cfg)
	return obj
}

func newRunningTestObj(t *testing.T, node NetworkInterface, timeout time.Duration, cfg ConfigObj) *Obj {
	t.Helper()
	cfg.Logger = noopLogObj{}
	cfg.Node = node
	cfg.UDPTimeout = timeout
	obj, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := obj.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return obj
}

// //

func checkIPv6(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 not available on this host")
	}
	_ = ln.Close()
}

func echoTCPServer6(t *testing.T) *net.TCPAddr {
	t.Helper()
	checkIPv6(t)
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatalf("echoTCPServer6: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go echoConn(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr)
}

func echoConn(c net.Conn) {
	defer func() { _ = c.Close() }()
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if err != nil || n == 0 {
			return
		}
		if _, err := c.Write(buf[:n]); err != nil {
			return
		}
	}
}

func freePort(t *testing.T, ip string) int {
	t.Helper()
	network := "tcp4"
	if ip == "::1" {
		network = "tcp6"
	}
	ln, err := net.Listen(network, "["+ip+"]:0")
	if err != nil {
		ln, err = net.Listen("tcp", ip+":0")
		if err != nil {
			t.Fatalf("freePort: %v", err)
		}
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
