package socks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"github.com/voluminor/ratatoskr/internal/common"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

const (
	maxUDPDatagramPayloadSize  = 65507
	maxSOCKSUDPHeaderSize      = 262
	associatePacketBufferSize  = maxUDPDatagramPayloadSize + maxSOCKSUDPHeaderSize
	associateTargetIdleTimeout = 30 * time.Second
	associateWriteTimeout      = 10 * time.Second
	associateWorkerCount       = 64
	associateJobQueueSize      = 1024
)

var errAssociateInvalidTarget = errors.New("SOCKS UDP associate target is invalid")
var errAssociateInvalidClientSpec = errors.New("SOCKS UDP associate client domain is unresolved")

var associateBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, associatePacketBufferSize)
		return &b
	},
}

type associateWorkerPoolObj struct {
	jobs    chan func()
	workers sync.WaitGroup
	mu      sync.RWMutex
	closed  bool
}

func newAssociateWorkerPool() *associateWorkerPoolObj {
	p := &associateWorkerPoolObj{jobs: make(chan func(), associateJobQueueSize)}
	p.workers.Add(associateWorkerCount)
	for range associateWorkerCount {
		go func() {
			defer p.workers.Done()
			for job := range p.jobs {
				job()
			}
		}()
	}
	return p
}

func (p *associateWorkerPoolObj) submit(job func()) bool {
	if p == nil || job == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return false
	}
	select {
	case p.jobs <- job:
		return true
	default:
		return false
	}
}

func (p *associateWorkerPoolObj) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.jobs)
	}
	p.mu.Unlock()
	p.workers.Wait()
}

// //

type associateSessionObj struct {
	owner            *Obj
	ctx              context.Context
	cancel           context.CancelFunc
	network          proxy.ContextDialer
	resolver         socks5.NameResolver
	relay            *net.UDPConn
	clientSpec       *statute.AddrSpec
	controlIP        net.IP
	serverLimiter    *common.DynamicLimitObj
	workerPool       *associateWorkerPoolObj
	principal        string
	isUnix           bool
	maxTargets       int
	maxPrincipal     int
	maxQueuedPackets int
	maxQueuedBytes   int
	dialTimeout      time.Duration
	idleTimeout      time.Duration

	clientUDP *net.UDPAddr

	targetMu  sync.Mutex
	targets   map[associateTargetKeyObj]*associateTargetObj
	pending   map[associateTargetKeyObj]struct{}
	closed    bool
	pendingWG sync.WaitGroup
	targetWG  sync.WaitGroup

	relayReadDeadline common.DeadlineGateObj
	closeOnce         sync.Once
}

type associateSessionConfigObj struct {
	serverLimiter    *common.DynamicLimitObj
	workerPool       *associateWorkerPoolObj
	isUnix           bool
	maxTargets       int
	maxPrincipal     int
	maxQueuedPackets int
	maxQueuedBytes   int
	dialTimeout      time.Duration
	idleTimeout      time.Duration
}

type associateTargetObj struct {
	session   *associateSessionObj
	key       associateTargetKeyObj
	conn      net.Conn
	header    []byte
	principal string
	out       *associatePacketQueueObj
	closeOnce sync.Once
}

type associatePacketQueueObj struct {
	mu         sync.Mutex
	packets    [][]byte
	head       int
	bytes      int
	maxPackets int
	maxBytes   int
	notify     chan struct{}
	closed     bool
}

type associateTargetKeyObj struct {
	kind byte
	host string
	port int
}

func newAssociatePacketQueue(maxPackets, maxBytes int) *associatePacketQueueObj {
	return &associatePacketQueueObj{
		maxPackets: maxPackets,
		maxBytes:   maxBytes,
		notify:     make(chan struct{}, 1),
	}
}

func (q *associatePacketQueueObj) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *associatePacketQueueObj) enqueue(payload []byte) (ok, full bool) {
	if q == nil {
		return false, false
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false, false
	}
	count := len(q.packets) - q.head
	if (q.maxPackets >= 0 && count >= q.maxPackets) ||
		(q.maxBytes >= 0 && q.bytes+len(payload) > q.maxBytes) {
		q.mu.Unlock()
		return false, true
	}
	packet := append([]byte(nil), payload...)
	q.packets = append(q.packets, packet)
	q.bytes += len(packet)
	q.mu.Unlock()
	q.signal()
	return true, false
}

func (q *associatePacketQueueObj) pop(ctx context.Context) ([]byte, bool) {
	for {
		q.mu.Lock()
		if q.head < len(q.packets) {
			packet := q.packets[q.head]
			q.packets[q.head] = nil
			q.head++
			q.bytes -= len(packet)
			if q.head == len(q.packets) {
				q.packets = q.packets[:0]
				q.head = 0
			} else if q.head >= 64 && q.head*2 >= len(q.packets) {
				q.packets = append(q.packets[:0], q.packets[q.head:]...)
				q.head = 0
			}
			q.mu.Unlock()
			return packet, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-q.notify:
		}
	}
}

func (q *associatePacketQueueObj) close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.closed = true
	clear(q.packets)
	q.packets = nil
	q.head = 0
	q.bytes = 0
	q.mu.Unlock()
	q.signal()
}

// //

func associateListenAddr(addr net.Addr) *net.UDPAddr {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr == nil {
		return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	}
	return &net.UDPAddr{IP: tcpAddr.IP, Port: 0, Zone: tcpAddr.Zone}
}

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

func associatePrincipal(request *socks5.Request, controlIP net.IP, isUnix bool) string {
	if request != nil && request.AuthContext != nil {
		if user := request.AuthContext.Payload["username"]; user != "" {
			return "user:" + user
		}
	}
	if controlIP != nil {
		return "ip:" + controlIP.String()
	}
	if isUnix {
		return "unix"
	}
	return "unknown"
}

func (s *associateSessionObj) acquireTargetSlot() bool {
	if s.serverLimiter != nil && !s.serverLimiter.Acquire() {
		if s.owner != nil {
			s.owner.associateRejected.Add(1)
		}
		return false
	}
	principal := s.principal
	if principal == "" {
		principal = "unknown"
	}
	if s.owner == nil {
		return true
	}
	limit := s.maxPrincipal
	if limit <= 0 {
		return true
	}
	s.owner.associatePrincipalMu.Lock()
	active := s.owner.associatePrincipals[principal]
	if active >= limit {
		s.owner.associatePrincipalMu.Unlock()
		if s.serverLimiter != nil {
			s.serverLimiter.Release()
		}
		s.owner.associateRejected.Add(1)
		return false
	}
	if s.owner.associatePrincipals == nil {
		s.owner.associatePrincipals = make(map[string]int)
	}
	s.owner.associatePrincipals[principal] = active + 1
	s.owner.associatePrincipalMu.Unlock()
	return true
}

func (s *associateSessionObj) releaseTargetSlot(principal string) {
	if principal == "" {
		principal = "unknown"
	}
	if s.owner != nil && s.maxPrincipal > 0 {
		s.owner.associatePrincipalMu.Lock()
		if active := s.owner.associatePrincipals[principal]; active <= 1 {
			delete(s.owner.associatePrincipals, principal)
		} else {
			s.owner.associatePrincipals[principal] = active - 1
		}
		s.owner.associatePrincipalMu.Unlock()
	}
	if s.serverLimiter != nil {
		s.serverLimiter.Release()
	}
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

func validateAssociateClientSpec(spec *statute.AddrSpec) error {
	if spec == nil || spec.FQDN == "" {
		return nil
	}
	if len(spec.IP) == 0 || spec.IP.IsUnspecified() {
		return fmt.Errorf("%w: %q", errAssociateInvalidClientSpec, spec.FQDN)
	}
	return nil
}

func associateTargetAddrType(addr statute.AddrSpec) byte {
	if addr.AddrType != 0 {
		return addr.AddrType
	}
	if addr.FQDN != "" {
		return statute.ATYPDomain
	}
	if addr.IP.To4() != nil {
		return statute.ATYPIPv4
	}
	if addr.IP.To16() != nil {
		return statute.ATYPIPv6
	}
	return 0
}

func associateTargetKey(addr statute.AddrSpec) (associateTargetKeyObj, error) {
	kind := associateTargetAddrType(addr)
	switch kind {
	case statute.ATYPDomain:
		host := strings.TrimSuffix(addr.FQDN, ".")
		if host == "" || !utf8.ValidString(host) {
			return associateTargetKeyObj{}, errAssociateInvalidTarget
		}
		return associateTargetKeyObj{
			kind: kind,
			host: strings.ToLower(host),
			port: addr.Port,
		}, nil
	case statute.ATYPIPv4, statute.ATYPIPv6:
		if len(addr.IP) == 0 {
			return associateTargetKeyObj{}, errAssociateInvalidTarget
		}
		return associateTargetKeyObj{
			kind: kind,
			host: addr.IP.String(),
			port: addr.Port,
		}, nil
	default:
		return associateTargetKeyObj{}, errAssociateInvalidTarget
	}
}

func associateTargetAddress(ctx context.Context, resolver socks5.NameResolver, addr statute.AddrSpec, key associateTargetKeyObj) (context.Context, string, error) {
	if key.kind != statute.ATYPDomain {
		return ctx, (&addr).String(), nil
	}
	name := key.host
	if resolver == nil {
		return ctx, net.JoinHostPort(name, strconv.Itoa(addr.Port)), nil
	}
	nextCtx, ip, err := resolver.Resolve(ctx, name)
	if err != nil {
		return nextCtx, "", err
	}
	if ip == nil {
		return nextCtx, "", fmt.Errorf("resolver returned no address for %s", name)
	}
	return nextCtx, net.JoinHostPort(ip.String(), strconv.Itoa(addr.Port)), nil
}

// //

func newAssociateSession(owner *Obj, ctx context.Context, network proxy.ContextDialer, resolver socks5.NameResolver, relay *net.UDPConn, request *socks5.Request) *associateSessionObj {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	config := owner.associateSessionConfig()
	return &associateSessionObj{
		owner:            owner,
		ctx:              sessionCtx,
		cancel:           cancel,
		network:          network,
		resolver:         resolver,
		relay:            relay,
		clientSpec:       request.DestAddr,
		serverLimiter:    config.serverLimiter,
		workerPool:       config.workerPool,
		isUnix:           config.isUnix,
		maxTargets:       config.maxTargets,
		maxPrincipal:     config.maxPrincipal,
		maxQueuedPackets: config.maxQueuedPackets,
		maxQueuedBytes:   config.maxQueuedBytes,
		dialTimeout:      config.dialTimeout,
		idleTimeout:      config.idleTimeout,
		targets:          make(map[associateTargetKeyObj]*associateTargetObj),
		pending:          make(map[associateTargetKeyObj]struct{}),
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
		if !s.acceptClient(src) {
			continue
		}
		packet, err := statute.ParseDatagram(buf[:n])
		if err != nil || packet.Frag != 0 {
			continue
		}
		s.refreshIdleDeadline()
		target, err := s.route(packet)
		if err != nil {
			continue
		}
		if target == nil {
			continue
		}
		target.enqueue(packet.Data)
	}
}

func cloneAssociateDatagram(packet statute.Datagram) statute.Datagram {
	clone := packet
	clone.Data = append([]byte(nil), packet.Data...)
	clone.DstAddr.IP = append(net.IP(nil), packet.DstAddr.IP...)
	return clone
}

func (s *associateSessionObj) route(packet statute.Datagram) (*associateTargetObj, error) {
	key, err := associateTargetKey(packet.DstAddr)
	if err != nil {
		return nil, err
	}
	s.targetMu.Lock()
	if target, ok := s.targets[key]; ok {
		s.targetMu.Unlock()
		return target, nil
	}
	if s.closed {
		s.targetMu.Unlock()
		return nil, net.ErrClosed
	}
	if _, ok := s.pending[key]; ok {
		s.targetMu.Unlock()
		if s.owner != nil {
			s.owner.associatePacketDrops.Add(1)
		}
		return nil, nil
	}
	if s.maxTargets >= 0 && len(s.targets)+len(s.pending) >= s.maxTargets {
		s.targetMu.Unlock()
		if s.owner != nil {
			s.owner.associateRejected.Add(1)
		}
		return nil, ErrAssociateTargetLimit
	}
	s.pending[key] = struct{}{}
	s.pendingWG.Add(1)
	if s.owner != nil {
		s.owner.associatePending.Add(1)
	}
	s.targetMu.Unlock()

	packet = cloneAssociateDatagram(packet)
	if !s.workerPool.submit(func() {
		defer s.pendingWG.Done()
		if s.owner != nil {
			defer s.owner.associatePending.Add(-1)
		}
		target, createErr := s.createTarget(packet, key)
		s.targetMu.Lock()
		delete(s.pending, key)
		s.targetMu.Unlock()
		if createErr != nil || target == nil {
			return
		}
		target.enqueue(packet.Data)
	}) {
		s.targetMu.Lock()
		delete(s.pending, key)
		s.targetMu.Unlock()
		s.pendingWG.Done()
		if s.owner != nil {
			s.owner.associatePending.Add(-1)
			s.owner.associateRejected.Add(1)
		}
		return nil, ErrAssociateTargetLimit
	}
	return nil, nil
}

func (s *associateSessionObj) acceptClient(addr *net.UDPAddr) bool {
	if !associateSpecAllowsClient(s.clientSpec, addr) {
		return false
	}
	if s.controlIP != nil && (addr == nil || !s.controlIP.Equal(addr.IP)) {
		return false
	}
	if s.clientUDP == nil {
		s.clientUDP = cloneUDPAddr(addr)
		return true
	}
	return udpAddrEqual(s.clientUDP, addr)
}

func (s *associateSessionObj) createTarget(packet statute.Datagram, key associateTargetKeyObj) (*associateTargetObj, error) {

	s.targetMu.Lock()
	if target, ok := s.targets[key]; ok {
		s.targetMu.Unlock()
		return target, nil
	}
	if s.closed {
		s.targetMu.Unlock()
		return nil, net.ErrClosed
	}
	if s.maxTargets >= 0 && len(s.targets) >= s.maxTargets {
		s.targetMu.Unlock()
		return nil, ErrAssociateTargetLimit
	}
	s.targetMu.Unlock()

	if !s.acquireTargetSlot() {
		return nil, ErrAssociateTargetLimit
	}

	targetCtx := s.ctx
	timeout := s.dialTimeout
	cancel := func() {}
	if timeout > 0 {
		targetCtx, cancel = context.WithTimeout(targetCtx, timeout)
	}
	defer cancel()

	targetCtx, address, err := associateTargetAddress(targetCtx, s.resolver, packet.DstAddr, key)
	if err != nil {
		s.releaseTargetSlot(s.principal)
		return nil, err
	}

	conn, err := s.network.DialContext(targetCtx, "udp", address)
	if err != nil {
		s.releaseTargetSlot(s.principal)
		return nil, err
	}

	target := &associateTargetObj{
		session:   s,
		key:       key,
		conn:      conn,
		header:    append([]byte(nil), packet.Header()...),
		principal: s.principal,
		out:       newAssociatePacketQueue(s.maxQueuedPackets, s.maxQueuedBytes),
	}

	s.targetMu.Lock()
	if s.closed {
		s.targetMu.Unlock()
		_ = conn.Close()
		s.releaseTargetSlot(s.principal)
		return nil, net.ErrClosed
	}
	if existing := s.targets[key]; existing != nil {
		s.targetMu.Unlock()
		_ = conn.Close()
		s.releaseTargetSlot(s.principal)
		return existing, nil
	}
	s.targetWG.Add(2)
	s.targets[key] = target
	s.targetMu.Unlock()

	go func() {
		defer s.targetWG.Done()
		target.forward()
	}()
	go func() {
		defer s.targetWG.Done()
		target.write()
	}()
	return target, nil
}

func (s *associateSessionObj) refreshIdleDeadline() {
	common.RefreshDeadline(time.Now(), s.idleTimeout, &s.relayReadDeadline, s.relay, true)
}

func (s *associateSessionObj) close() {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.relay.Close()
		s.targetMu.Lock()
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
	hlen := copy(buf, t.header)
	for {
		t.touch()
		n, err := t.conn.Read(buf[hlen:])
		if err != nil {
			return
		}
		if hlen+n > maxUDPDatagramPayloadSize {
			if t.session.owner != nil {
				t.session.owner.associatePacketDrops.Add(1)
			}
			continue
		}
		if _, err = t.session.relay.WriteToUDP(buf[:hlen+n], t.session.clientUDP); err != nil {
			return
		}
		t.session.refreshIdleDeadline()
	}
}

func (t *associateTargetObj) enqueue(payload []byte) {
	ok, full := t.out.enqueue(payload)
	if ok {
		return
	}
	if full && t.session.owner != nil {
		t.session.owner.associatePacketDrops.Add(1)
	}
}

func (t *associateTargetObj) write() {
	defer func() {
		t.close()
		t.session.deleteTarget(t)
	}()
	for {
		packet, ok := t.out.pop(t.session.ctx)
		if !ok {
			return
		}
		_ = t.conn.SetWriteDeadline(time.Now().Add(associateWriteTimeout))
		if _, err := t.conn.Write(packet); err != nil {
			return
		}
		t.touch()
	}
}

func (t *associateTargetObj) close() {
	t.closeOnce.Do(func() {
		t.out.close()
		_ = t.conn.Close()
		t.session.releaseTargetSlot(t.principal)
	})
}

// //

func (s *Obj) handleAssociate(ctx context.Context, writer io.Writer, request *socks5.Request, network proxy.ContextDialer, resolver socks5.NameResolver) error {
	if err := validateAssociateClientSpec(request.DestAddr); err != nil {
		if replyErr := socks5.SendReply(writer, statute.RepHostUnreachable, nil); replyErr != nil {
			return fmt.Errorf("failed to send SOCKS UDP ASSOCIATE client-domain failure reply: %w", replyErr)
		}
		return err
	}
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
	session.principal = associatePrincipal(request, session.controlIP, session.isUnix)
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
		session.pendingWG.Wait()
		session.targetWG.Wait()
		return err
	case err = <-udpDone:
		session.close()
		if associateControlClose(writer) {
			<-controlDone
		}
		session.pendingWG.Wait()
		session.targetWG.Wait()
		return err
	}
}
