package forward

import (
	"context"
	"crypto/ed25519"
	"net"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/mod/core"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// mockNodeObj — network-capable node backed by real loopback TCP/UDP
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

func (m *mockNodeObj) Subnet() net.IPNet            { return net.IPNet{} }
func (m *mockNodeObj) PublicKey() ed25519.PublicKey { return nil }
func (m *mockNodeObj) MTU() uint64 {
	if m.mtu > 0 {
		return m.mtu
	}
	return 65535
}
func (m *mockNodeObj) AddPeer(_ string) error       { return nil }
func (m *mockNodeObj) RemovePeer(_ string) error    { return nil }
func (m *mockNodeObj) GetPeers() []yggcore.PeerInfo { return nil }
func (m *mockNodeObj) EnableMulticast() error       { return nil }
func (m *mockNodeObj) DisableMulticast() error      { return nil }
func (m *mockNodeObj) EnableAdmin(_ string) error   { return nil }
func (m *mockNodeObj) DisableAdmin() error          { return nil }
func (m *mockNodeObj) Close() error                 { return nil }

// //

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

func newTestManagerObj(node core.NetworkInterface, timeout time.Duration, cfg ConfigObj) *ManagerObj {
	cfg.Logger = noopLogObj{}
	cfg.Node = node
	cfg.UDPTimeout = timeout
	return New(cfg)
}

// //

// checkIPv6 skips the test if IPv6 loopback is unavailable
func checkIPv6(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 not available on this host")
	}
	_ = ln.Close()
}

// echoTCPServer6 starts a TCP echo server on [::1]:0 and returns its address
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

// freePort returns an unused TCP port on the given IP
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
