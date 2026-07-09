package socks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"github.com/voluminor/ratatoskr/internal/common"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

const (
	associatePacketBufferSize  = 64 * 1024
	maxTargetsPerSession       = 256
	associateTargetIdleTimeout = 30 * time.Second
	associateWriteTimeout      = 10 * time.Second
)

var errAssociateTargetLimit = errors.New("SOCKS UDP associate target limit reached")

// associateBufferPool recycles the 64 KiB per-target relay buffers so targets
// that open, go idle, and expire do not churn the allocator under load.
var associateBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, associatePacketBufferSize)
		return &b
	},
}

// //

// associateSessionObj owns one UDP relay created by a SOCKS UDP ASSOCIATE request.
type associateSessionObj struct {
	owner         *Obj
	ctx           context.Context
	cancel        context.CancelFunc
	network       proxy.ContextDialer
	resolver      socks5.NameResolver
	relay         *net.UDPConn
	clientSpec    *statute.AddrSpec
	controlIP     net.IP
	targetLimit   int
	globalLimiter *common.DynamicLimitObj

	clientMu  sync.Mutex
	clientUDP *net.UDPAddr

	targetMu sync.Mutex
	targets  map[string]*associateTargetObj
	closed   bool

	deadlineMu        sync.Mutex
	relayReadDeadline int64
	closeOnce         sync.Once
}

type associateTargetObj struct {
	session   *associateSessionObj
	key       string
	conn      net.Conn
	header    []byte
	client    *net.UDPAddr
	closeOnce sync.Once
}

// //

func associateListenAddr(addr net.Addr) *net.UDPAddr {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr == nil {
		return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	}
	return &net.UDPAddr{IP: tcpAddr.IP, Port: 0, Zone: tcpAddr.Zone}
}

// controlConnIP returns the IP of the SOCKS control connection. It is used to
// pin the UDP relay to the host that set up the association, so on a non-loopback
// listener another host cannot seize the relay by racing the first datagram.
func controlConnIP(writer io.Writer) net.IP {
	conn, ok := writer.(net.Conn)
	if !ok {
		return nil
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

func associateControlClose(writer io.Writer) bool {
	closer, ok := writer.(io.Closer)
	if ok {
		_ = closer.Close()
	}
	return ok
}

func associateNormalError(err error) error {
	if err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) {
		return nil
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return nil
	}
	return err
}

func drainAssociateControl(reader io.Reader) error {
	if reader == nil {
		return nil
	}
	var buf [1024]byte
	for {
		_, err := reader.Read(buf[:])
		if err != nil {
			return associateNormalError(err)
		}
	}
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	clone := *addr
	if addr.IP != nil {
		clone.IP = append(net.IP(nil), addr.IP...)
	}
	return &clone
}

func udpAddrEqual(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Port == b.Port && a.Zone == b.Zone && a.IP.Equal(b.IP)
}

func associateSpecAllowsClient(spec *statute.AddrSpec, addr *net.UDPAddr) bool {
	if addr == nil {
		return false
	}
	if spec == nil {
		return true
	}
	if spec.Port != 0 && spec.Port != addr.Port {
		return false
	}
	if len(spec.IP) == 0 || spec.IP.IsUnspecified() {
		return spec.FQDN == ""
	}
	return spec.IP.Equal(addr.IP)
}

func associateTargetAddress(ctx context.Context, resolver socks5.NameResolver, addr statute.AddrSpec) (context.Context, string, error) {
	if addr.FQDN == "" {
		return ctx, (&addr).String(), nil
	}
	if resolver == nil {
		return ctx, (&addr).String(), nil
	}
	nextCtx, ip, err := resolver.Resolve(ctx, addr.FQDN)
	if err != nil {
		return nextCtx, "", err
	}
	if ip == nil {
		return nextCtx, "", fmt.Errorf("resolver returned no address for %s", addr.FQDN)
	}
	return nextCtx, net.JoinHostPort(ip.String(), strconv.Itoa(addr.Port)), nil
}

// //

func newAssociateSession(owner *Obj, ctx context.Context, network proxy.ContextDialer, resolver socks5.NameResolver, relay *net.UDPConn, request *socks5.Request) *associateSessionObj {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	return &associateSessionObj{
		owner:         owner,
		ctx:           sessionCtx,
		cancel:        cancel,
		network:       network,
		resolver:      resolver,
		relay:         relay,
		clientSpec:    request.DestAddr,
		targetLimit:   maxTargetsPerSession,
		globalLimiter: owner.associateLimiter,
		targets:       make(map[string]*associateTargetObj),
	}
}

func (s *associateSessionObj) run() error {
	buf := make([]byte, associatePacketBufferSize)
	s.refreshIdleDeadline()
	for {
		n, src, err := s.relay.ReadFromUDP(buf)
		if err != nil {
			return associateNormalError(err)
		}
		s.refreshIdleDeadline()
		client, ok := s.acceptClient(src)
		if !ok {
			continue
		}
		packet, err := statute.ParseDatagram(buf[:n])
		if err != nil || packet.Frag != 0 {
			continue
		}
		target, err := s.target(packet, client)
		if err != nil {
			if errors.Is(err, errAssociateTargetLimit) {
				continue
			}
			continue
		}
		// Bound the write to the ygg-side (gonet) target so a wedged userspace
		// write cannot stall the whole session relay (head-of-line). On a gonet
		// conn SetWriteDeadline is a cheap userspace op, not a syscall.
		_ = target.conn.SetWriteDeadline(time.Now().Add(associateWriteTimeout))
		if _, err = target.conn.Write(packet.Data); err != nil {
			target.close()
			continue
		}
		target.touch()
	}
}

func (s *associateSessionObj) acceptClient(addr *net.UDPAddr) (*net.UDPAddr, bool) {
	if !associateSpecAllowsClient(s.clientSpec, addr) {
		return nil, false
	}
	// Pin to the control connection's host: a datagram from a different IP cannot
	// claim the relay, even when the client declared an unspecified address.
	if s.controlIP != nil && (addr == nil || !s.controlIP.Equal(addr.IP)) {
		return nil, false
	}
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if s.clientUDP == nil {
		s.clientUDP = cloneUDPAddr(addr)
		return cloneUDPAddr(s.clientUDP), true
	}
	if !udpAddrEqual(s.clientUDP, addr) {
		return nil, false
	}
	return cloneUDPAddr(s.clientUDP), true
}

func (s *associateSessionObj) target(packet statute.Datagram, client *net.UDPAddr) (*associateTargetObj, error) {
	targetCtx := s.ctx
	timeout := s.owner.DialTimeout()
	cancel := func() {}
	if timeout > 0 {
		targetCtx, cancel = context.WithTimeout(targetCtx, timeout)
	}
	defer cancel()

	targetCtx, address, err := associateTargetAddress(targetCtx, s.resolver, packet.DstAddr)
	if err != nil {
		return nil, err
	}

	// run() is the sole inserter and is single-threaded; forward() goroutines only
	// delete. No target for this address can appear between the check here and the
	// insert after dialing, so a single lock/insert replaces double-checked locking.
	s.targetMu.Lock()
	if target, ok := s.targets[address]; ok {
		s.targetMu.Unlock()
		return target, nil
	}
	if len(s.targets) >= s.targetLimit {
		s.targetMu.Unlock()
		return nil, errAssociateTargetLimit
	}
	s.targetMu.Unlock()

	// Process-wide cap: one client must not exhaust UDP sockets across sessions.
	if s.globalLimiter != nil && !s.globalLimiter.Acquire() {
		return nil, errAssociateTargetLimit
	}

	conn, err := s.network.DialContext(targetCtx, "udp", address)
	if err != nil {
		if s.globalLimiter != nil {
			s.globalLimiter.Release()
		}
		return nil, err
	}

	target := &associateTargetObj{
		session: s,
		key:     address,
		conn:    conn,
		header:  append([]byte(nil), packet.Header()...),
		client:  cloneUDPAddr(client),
	}

	// A concurrent close() may have snapshotted targets between dial start and now.
	// If the session is already closed, nothing will ever close this target, so
	// release its resources here (conn + global slot) instead of inserting it.
	s.targetMu.Lock()
	if s.closed {
		s.targetMu.Unlock()
		_ = conn.Close()
		if s.globalLimiter != nil {
			s.globalLimiter.Release()
		}
		return nil, net.ErrClosed
	}
	s.targets[address] = target
	s.targetMu.Unlock()

	go target.forward()
	return target, nil
}

func (s *associateSessionObj) refreshIdleDeadline() {
	s.deadlineMu.Lock()
	defer s.deadlineMu.Unlock()

	timeout := s.owner.TunnelIdleTimeout()
	action, deadline := common.DeadlineRefresh(time.Now(), timeout, timeout, s.relayReadDeadline)
	switch action {
	case common.DeadlineClear:
		if s.relayReadDeadline != 0 {
			s.relayReadDeadline = 0
			_ = s.relay.SetReadDeadline(time.Time{})
		}
	case common.DeadlineArm:
		s.relayReadDeadline = deadline.UnixNano()
		_ = s.relay.SetReadDeadline(deadline)
	}
}

func (s *associateSessionObj) close() {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.relay.Close()
		s.targetMu.Lock()
		// Mark closed under the lock so any concurrent target() insert that runs
		// after this snapshot observes it and cleans up its own dialed conn.
		s.closed = true
		targets := make([]*associateTargetObj, 0, len(s.targets))
		for _, target := range s.targets {
			targets = append(targets, target)
		}
		s.targetMu.Unlock()
		for _, target := range targets {
			target.close()
		}
	})
}

func (s *associateSessionObj) deleteTarget(target *associateTargetObj) {
	s.targetMu.Lock()
	if s.targets[target.key] == target {
		delete(s.targets, target.key)
	}
	s.targetMu.Unlock()
}

// //

// touch arms the target's idle deadline; an idle target self-expires and releases
// its goroutine, upstream conn, socket slot, and pooled buffer.
func (t *associateTargetObj) touch() {
	_ = t.conn.SetReadDeadline(time.Now().Add(associateTargetIdleTimeout))
}

func (t *associateTargetObj) forward() {
	defer func() {
		t.close()
		t.session.deleteTarget(t)
	}()

	bufp := associateBufferPool.Get().(*[]byte)
	defer associateBufferPool.Put(bufp)
	buf := *bufp
	for {
		t.touch()
		n, err := t.conn.Read(buf)
		if err != nil {
			return
		}
		packet := make([]byte, 0, len(t.header)+n)
		packet = append(packet, t.header...)
		packet = append(packet, buf[:n]...)
		if _, err = t.session.relay.WriteToUDP(packet, t.client); err != nil {
			return
		}
		t.session.refreshIdleDeadline()
	}
}

func (t *associateTargetObj) close() {
	t.closeOnce.Do(func() {
		_ = t.conn.Close()
		if t.session.globalLimiter != nil {
			t.session.globalLimiter.Release()
		}
	})
}

// //

func (s *Obj) handleAssociate(ctx context.Context, writer io.Writer, request *socks5.Request, network proxy.ContextDialer, resolver socks5.NameResolver) error {
	relay, err := net.ListenUDP("udp", associateListenAddr(request.LocalAddr))
	if err != nil {
		if replyErr := socks5.SendReply(writer, statute.RepServerFailure, nil); replyErr != nil {
			return fmt.Errorf("failed to send SOCKS UDP ASSOCIATE failure reply: %w", replyErr)
		}
		return fmt.Errorf("listen SOCKS UDP relay: %w", err)
	}

	if err = socks5.SendReply(writer, statute.RepSuccess, relay.LocalAddr()); err != nil {
		_ = relay.Close()
		return fmt.Errorf("failed to send SOCKS UDP ASSOCIATE reply: %w", err)
	}

	session := newAssociateSession(s, ctx, network, resolver, relay, request)
	session.controlIP = controlConnIP(writer)
	udpDone := make(chan error, 1)
	controlDone := make(chan error, 1)
	go func() {
		udpDone <- session.run()
	}()
	go func() {
		controlDone <- drainAssociateControl(request.Reader)
	}()

	select {
	case err = <-controlDone:
		session.close()
		<-udpDone
		return err
	case err = <-udpDone:
		session.close()
		if associateControlClose(writer) {
			<-controlDone
		}
		return err
	}
}
