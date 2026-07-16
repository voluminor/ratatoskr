package socks

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// // // // // // // // // //

type countingResolverObj struct {
	ip    net.IP
	calls atomic.Int64
}

func (r *countingResolverObj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	r.calls.Add(1)
	return ctx, append(net.IP(nil), r.ip...), nil
}

// //

func sendOne(t *testing.T, udpConn net.PacketConn, relay net.Addr, target string, payload []byte) {
	t.Helper()
	_ = udpConn.SetDeadline(time.Now().Add(time.Second))
	got := sendSocksUDP(t, udpConn, relay, target, payload)
	if !bytes.Equal(got, payload) {
		t.Fatalf("target %q: unexpected echo %q, want %q", target, got, payload)
	}
}

// // // // // // // // // //

func TestAssociate_resolvesDomainOncePerTarget(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &countingResolverObj{ip: net.IPv4(127, 0, 0, 1)}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()

	port := echo.LocalAddr().(*net.UDPAddr).Port
	target := net.JoinHostPort("udp-target.pk.ygg", strconv.Itoa(port))
	const n = 5
	for i := 0; i < n; i++ {
		sendOne(t, udpConn, relay, target, []byte(fmt.Sprintf("datagram-%d", i)))
	}
	if calls := resolver.calls.Load(); calls != 1 {
		t.Fatalf("resolver called %d times for %d datagrams to one FQDN target; want 1 (resolve-on-miss)", calls, n)
	}
}

// //

func TestAssociate_domainKeyIsCanonical(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &countingResolverObj{ip: net.IPv4(127, 0, 0, 1)}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()

	port := strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port)
	variants := []string{
		net.JoinHostPort("Udp-Target.PK.Ygg", port),
		net.JoinHostPort("udp-target.pk.ygg", port),
		net.JoinHostPort("udp-target.pk.ygg.", port),
	}
	for i, target := range variants {
		sendOne(t, udpConn, relay, target, []byte(fmt.Sprintf("variant-%d", i)))
	}
	if calls := resolver.calls.Load(); calls != 1 {
		t.Fatalf("resolver called %d times for case/dot variants of one host; want 1 (canonical key)", calls)
	}
}

// //

func TestAssociate_distinctDomainsResolveIndependently(t *testing.T) {
	echo := udpEchoServer(t)
	cfg := tcpCfgOnFreePort(t)
	resolver := &countingResolverObj{ip: net.IPv4(127, 0, 0, 1)}
	cfg.Resolver = resolver
	_, _, relay := associateRelay(t, cfg, "0.0.0.0", 0)

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()

	port := strconv.Itoa(echo.LocalAddr().(*net.UDPAddr).Port)
	hostA := net.JoinHostPort("host-a.pk.ygg", port)
	hostB := net.JoinHostPort("host-b.pk.ygg", port)
	for i := 0; i < 2; i++ {
		sendOne(t, udpConn, relay, hostA, []byte(fmt.Sprintf("a-%d", i)))
		sendOne(t, udpConn, relay, hostB, []byte(fmt.Sprintf("b-%d", i)))
	}
	if calls := resolver.calls.Load(); calls != 2 {
		t.Fatalf("resolver called %d times for 2 distinct FQDN targets; want 2 (one resolve per target)", calls)
	}
}
