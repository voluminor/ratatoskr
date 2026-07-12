package forward

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

const (
	udpSessionQueueBytes      = 64 * 1024
	udpSessionQueueMaxPackets = 64
)

// //

type udpSessionObj struct {
	ctx          context.Context
	connMu       sync.RWMutex
	conn         net.Conn
	out          chan []byte
	lastActivity atomic.Int64
	cancel       context.CancelFunc
	closeOnce    sync.Once
	counter      *atomic.Int64
}

type udpBufferPoolObj struct {
	size int
	pool sync.Pool
}

type packetWriterObj struct {
	conn net.PacketConn
}

func newUDPBufferPool(size int) *udpBufferPoolObj {
	if size <= 0 {
		size = maxUDPDatagramSize
	}
	p := &udpBufferPoolObj{size: size}
	p.pool.New = func() any {
		buf := make([]byte, p.size)
		return &buf
	}
	return p
}

func (p *udpBufferPoolObj) get(n int) []byte {
	// Pooled buffers are always exactly p.size and n never exceeds it.
	buf := p.pool.Get().(*[]byte)
	return (*buf)[:n]
}

func (p *udpBufferPoolObj) put(buf []byte) {
	if cap(buf) != p.size {
		return
	}
	buf = buf[:p.size]
	p.pool.Put(&buf)
}

func newPacketWriter(conn net.PacketConn) *packetWriterObj {
	return &packetWriterObj{conn: conn}
}

// writeTo writes one datagram. net.PacketConn.WriteTo is safe for concurrent use,
// so every session shares one writer without a lock; a UDP write reaches the
// kernel send buffer and effectively never blocks, so it carries no deadline.
func (w *packetWriterObj) writeTo(packet []byte, addr net.Addr) error {
	_, err := w.conn.WriteTo(packet, addr)
	return err
}

func (s *udpSessionObj) close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.connMu.RLock()
		conn := s.conn
		s.connMu.RUnlock()
		if conn != nil {
			_ = conn.Close()
		}
		if s.counter != nil {
			s.counter.Add(-1)
		}
	})
}

func (s *udpSessionObj) setConn(conn net.Conn) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.conn = conn
	return true
}

func (s *udpSessionObj) getConn() net.Conn {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.conn
}

type udpStartObj struct {
	mapping     UDPMappingObj
	conn        net.PacketConn
	logMapping  func(UDPMappingObj)
	acceptLabel string
	dial        func(UDPMappingObj, context.Context, net.Addr) (net.Conn, error)
}

// udpSessionMapObj is the NAT table keyed by source addr:port. Keying a typed map
// by the comparable netip.AddrPort keeps the read-loop hot path allocation-free,
// unlike sync.Map, which would box the key into interface{} on every datagram.
type udpSessionMapObj struct {
	mu sync.RWMutex
	m  map[netip.AddrPort]*udpSessionObj
}

type udpSessionEntryObj struct {
	key     netip.AddrPort
	session *udpSessionObj
}

func newUDPSessionMap() *udpSessionMapObj {
	return &udpSessionMapObj{m: make(map[netip.AddrPort]*udpSessionObj)}
}

func (t *udpSessionMapObj) load(key netip.AddrPort) (*udpSessionObj, bool) {
	t.mu.RLock()
	session, ok := t.m[key]
	t.mu.RUnlock()
	return session, ok
}

func (t *udpSessionMapObj) store(key netip.AddrPort, session *udpSessionObj) {
	t.mu.Lock()
	t.m[key] = session
	t.mu.Unlock()
}

// compareAndDelete removes key only while it still maps to session, mirroring
// sync.Map.CompareAndDelete so a replacement session installed after a stale
// close is never dropped.
func (t *udpSessionMapObj) compareAndDelete(key netip.AddrPort, session *udpSessionObj) {
	t.mu.Lock()
	if t.m[key] == session {
		delete(t.m, key)
	}
	t.mu.Unlock()
}

// snapshot copies the live entries so cleanup can close sessions without holding
// the map lock (session.close() runs a conn Close syscall and must not block it).
func (t *udpSessionMapObj) snapshot() []udpSessionEntryObj {
	t.mu.RLock()
	out := make([]udpSessionEntryObj, 0, len(t.m))
	for key, session := range t.m {
		out = append(out, udpSessionEntryObj{key: key, session: session})
	}
	t.mu.RUnlock()
	return out
}

func closeUDPSession(sessions *udpSessionMapObj, key netip.AddrPort, session *udpSessionObj) {
	sessions.compareAndDelete(key, session)
	session.close()
}

func udpQueueSize(maxPacketSize int) int {
	maxPacketSize = clampUDPMaxPacketSize(maxPacketSize)
	n := udpSessionQueueBytes / maxPacketSize
	if n < 1 {
		return 1
	}
	if n > udpSessionQueueMaxPackets {
		return udpSessionQueueMaxPackets
	}
	return n
}

func (cfg UDPLoopConfigObj) sessionTimeout() time.Duration {
	return cfg.Timeout
}

func (cfg UDPLoopConfigObj) sessionLimit() int {
	return cfg.MaxSessions
}

func (cfg UDPLoopConfigObj) dialTimeout() time.Duration {
	return cfg.DialTimeout
}

func enqueueUDPPacket(session *udpSessionObj, pool *udpBufferPoolObj, packet []byte) bool {
	buf := pool.get(len(packet))
	copy(buf, packet)
	select {
	case session.out <- buf:
		return true
	case <-session.ctx.Done():
		pool.put(buf)
		return false
	default:
		pool.put(buf)
		return false
	}
}

func drainUDPPackets(ch <-chan []byte, pool *udpBufferPoolObj) {
	for {
		select {
		case buf := <-ch:
			pool.put(buf)
		default:
			return
		}
	}
}

// udpSessionKey derives the comparable NAT-table key from a datagram source.
// A UDP PacketConn always yields *net.UDPAddr, so ok is false only for an
// impossible address shape, in which case the read loop drops the datagram.
func udpSessionKey(addr net.Addr) (netip.AddrPort, bool) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok || udpAddr.Port < 0 || udpAddr.Port > 65535 {
		return netip.AddrPort{}, false
	}
	ip, ok := netip.AddrFromSlice(udpAddr.IP)
	if !ok {
		return netip.AddrPort{}, false
	}
	ip = ip.Unmap()
	if udpAddr.Zone != "" {
		ip = ip.WithZone(udpAddr.Zone)
	}
	return netip.AddrPortFrom(ip, uint16(udpAddr.Port)), true
}

func (m *ManagerObj) prepareUDP(
	mappings []UDPMappingObj,
	listen func(UDPMappingObj) (net.PacketConn, string, error),
	logMapping func(UDPMappingObj),
	acceptLabel string,
	dial func(UDPMappingObj, context.Context, net.Addr) (net.Conn, error),
) ([]udpStartObj, error) {
	starts := make([]udpStartObj, 0, len(mappings))
	for _, mapping := range mappings {
		if err := validateUDPMapping(mapping); err != nil {
			closeUDPStarts(starts)
			return nil, err
		}
		conn, listenAddr, err := listen(mapping)
		if err != nil {
			closeUDPStarts(starts)
			return nil, fmt.Errorf("listen on %s UDP %s: %w", acceptLabel, listenAddr, err)
		}
		starts = append(starts, udpStartObj{
			mapping:     mapping,
			conn:        conn,
			logMapping:  logMapping,
			acceptLabel: acceptLabel,
			dial:        dial,
		})
	}
	return starts, nil
}

func validateUDPMapping(mapping UDPMappingObj) error {
	if mapping.Listen == nil {
		return fmt.Errorf("%w: UDP listen address is nil", ErrInvalidMapping)
	}
	if mapping.Mapped == nil {
		return fmt.Errorf("%w: UDP mapped address is nil", ErrInvalidMapping)
	}
	return nil
}

func closeUDPStarts(starts []udpStartObj) {
	for _, start := range starts {
		_ = start.conn.Close()
	}
}

func (m *ManagerObj) runUDPStarts(ctx context.Context, starts []udpStartObj) {
	for _, start := range starts {
		m.wg.Add(1)
		go func(st udpStartObj) {
			defer m.wg.Done()
			defer func() { _ = st.conn.Close() }()
			st.logMapping(st.mapping)

			runUDPLoopWithWait(ctx, UDPLoopConfigObj{
				Logger:     m.log,
				ListenConn: st.conn,
				Dial: func(ctx context.Context, addr net.Addr) (net.Conn, error) {
					return st.dial(st.mapping, ctx, addr)
				},
				DialTimeout:   m.dialTimeout,
				MaxPacketSize: m.effectiveUDPMaxPacketSize(),
				Timeout:       m.timeout,
				MaxSessions:   m.maxUDPSessions,
			}, &m.wg)
		}(start)
	}
}

// //

func (m *ManagerObj) prepareLocalUDP() ([]udpStartObj, error) {
	return m.prepareUDP(m.localUDPs,
		func(mp UDPMappingObj) (net.PacketConn, string, error) {
			conn, err := net.ListenUDP("udp", mp.Listen)
			return conn, mp.Listen.String(), err
		},
		func(mp UDPMappingObj) {
			m.log.Infof("[forward] mapping local UDP port %d to Yggdrasil %s", mp.Listen.Port, mp.Mapped)
		},
		"local",
		func(mp UDPMappingObj, ctx context.Context, _ net.Addr) (net.Conn, error) {
			return m.node.DialContext(ctx, "udp", fmt.Sprintf("[%s]:%d", mp.Mapped.IP, mp.Mapped.Port))
		},
	)
}

func (m *ManagerObj) prepareRemoteUDP() ([]udpStartObj, error) {
	return m.prepareUDP(m.remoteUDPs,
		func(mp UDPMappingObj) (net.PacketConn, string, error) {
			addr := fmt.Sprintf("[%s]:%d", m.node.Address(), mp.Listen.Port)
			conn, err := m.node.ListenPacket("udp", addr)
			return conn, addr, err
		},
		func(mp UDPMappingObj) {
			m.log.Infof("[forward] mapping Yggdrasil UDP port %d to %s", mp.Listen.Port, mp.Mapped)
		},
		"remote",
		func(mp UDPMappingObj, _ context.Context, _ net.Addr) (net.Conn, error) {
			return net.DialUDP("udp", nil, mp.Mapped)
		},
	)
}

// //

// RunUDPLoop reads packets, routes them to sessions, and cleans up inactive ones.
// Cancelling ctx closes cfg.ListenConn to unblock reads and then waits for session workers.
func RunUDPLoop(ctx context.Context, cfg UDPLoopConfigObj) {
	var wg sync.WaitGroup
	runUDPLoopWithWait(ctx, cfg, &wg)
	wg.Wait()
}

func trackUDPWorker(wg *sync.WaitGroup, fn func()) {
	if wg != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
		return
	}
	go fn()
}

func runUDPWriter(ctx context.Context, session *udpSessionObj, pool *udpBufferPoolObj) error {
	defer drainUDPPackets(session.out, pool)
	conn := session.getConn()
	if conn == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case packet := <-session.out:
			_, err := conn.Write(packet)
			pool.put(packet)
			if err != nil {
				return err
			}
		}
	}
}

func startUDPSessionWorker(ctx context.Context, cfg UDPLoopConfigObj, sessions *udpSessionMapObj, key netip.AddrPort, remoteAddr net.Addr, session *udpSessionObj, pool *udpBufferPoolObj, writer *packetWriterObj, maxPacketSize int, wg *sync.WaitGroup, log yggcore.Logger) {
	trackUDPWorker(wg, func() {
		dialCtx, cancel := dialTimeoutContext(session.ctx, cfg.dialTimeout())
		fwdConn, err := cfg.Dial(dialCtx, remoteAddr)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				log.Errorf("[forward] failed to dial upstream: %s", err)
			}
			drainUDPPackets(session.out, pool)
			closeUDPSession(sessions, key, session)
			return
		}
		if !session.setConn(fwdConn) {
			_ = fwdConn.Close()
			drainUDPPackets(session.out, pool)
			closeUDPSession(sessions, key, session)
			return
		}
		trackUDPWorker(wg, func() {
			reverseProxyUDP(session.ctx, UDPReverseConfigObj{
				Dst:           cfg.ListenConn,
				DstAddr:       remoteAddr,
				Src:           fwdConn,
				MaxPacketSize: maxPacketSize,
				Activity: func() {
					session.lastActivity.Store(time.Now().UnixMilli())
				},
				writer: writer,
			})
			closeUDPSession(sessions, key, session)
		})
		if err := runUDPWriter(session.ctx, session, pool); err != nil && session.ctx.Err() == nil {
			log.Debugf("[forward] session write error: %s", err)
		}
		closeUDPSession(sessions, key, session)
	})
}

func runUDPLoopWithWait(ctx context.Context, cfg UDPLoopConfigObj, wg *sync.WaitGroup) {
	if ctx == nil {
		ctx = context.Background()
	}
	log := common.NormalizeLogger(cfg.Logger)
	if cfg.sessionTimeout() <= 0 {
		log.Errorf("[forward] invalid UDP session timeout: %s", cfg.sessionTimeout())
		return
	}
	if cfg.ListenConn == nil {
		log.Errorf("[forward] UDP listen connection is required")
		return
	}
	if cfg.Dial == nil {
		log.Errorf("[forward] UDP dial function is required")
		return
	}
	// Apply safe defaults so the standalone entrypoint matches the manager: a zero
	// limit means "safe default", not "unlimited". An explicit negative keeps its
	// unlimited meaning — the caller's deliberate choice.
	cfg.MaxSessions = effectiveMaxConnections(cfg.MaxSessions, DefaultMaxUDPSessions)
	var sessionCount atomic.Int64
	sessions := newUDPSessionMap()
	limitLog := intervalLogObj{}
	readErrorLog := intervalLogObj{}
	oversizeLog := intervalLogObj{}
	queueLog := intervalLogObj{}
	maxPacketSize := clampUDPMaxPacketSize(cfg.MaxPacketSize)
	packetPool := newUDPBufferPool(maxPacketSize)
	packetWriter := newPacketWriter(cfg.ListenConn)
	sessionQueueSize := udpQueueSize(maxPacketSize)
	readDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = cfg.ListenConn.Close()
		case <-readDone:
		}
	}()
	defer close(readDone)

	// Clean up inactive sessions
	trackUDPWorker(wg, func() {
		for {
			timeout := cfg.sessionTimeout()
			if timeout <= 0 {
				timeout = time.Millisecond
			}
			timer := time.NewTimer(udpCleanupInterval(timeout))
			select {
			case <-ctx.Done():
				timer.Stop()
				for _, e := range sessions.snapshot() {
					e.session.close()
				}
				return
			case <-timer.C:
				now := time.Now().UnixMilli()
				timeout = cfg.sessionTimeout()
				for _, e := range sessions.snapshot() {
					if timeout > 0 && now-e.session.lastActivity.Load() > timeout.Milliseconds() {
						log.Debugf("[forward] cleaning up inactive UDP session %v", e.key)
						closeUDPSession(sessions, e.key, e.session)
					}
				}
			}
		}
	})

	buf := make([]byte, udpReadBufferSize(maxPacketSize))
	backoff := time.Duration(0)
	for {
		n, remoteAddr, err := cfg.ListenConn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if readErrorLog.allow(limitLogInterval) {
				log.Debugf("[forward] UDP read error: %v", err)
			}
			backoff = nextBackoff(backoff)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}
		backoff = 0
		if n == 0 {
			continue
		}
		if n > maxPacketSize {
			if oversizeLog.allow(limitLogInterval) {
				log.Warnf("[forward] UDP packet from %s exceeds max packet size %d, dropping", remoteAddr, maxPacketSize)
			}
			continue
		}

		key, keyOK := udpSessionKey(remoteAddr)
		if !keyOK {
			continue
		}
		session, ok := sessions.load(key)
		created := false
		if !ok {
			maxSessions := cfg.sessionLimit()
			if maxSessions > 0 && sessionCount.Load() >= int64(maxSessions) {
				if limitLog.allow(limitLogInterval) {
					log.Warnf("[forward] UDP session limit reached (%d), dropping packet from %s", maxSessions, remoteAddr)
				}
				continue
			}
			sessCtx, sessCancel := context.WithCancel(ctx)
			session = &udpSessionObj{
				ctx:     sessCtx,
				cancel:  sessCancel,
				out:     make(chan []byte, sessionQueueSize),
				counter: &sessionCount,
			}
			session.lastActivity.Store(time.Now().UnixMilli())
			sessionCount.Add(1)
			sessions.store(key, session)
			startUDPSessionWorker(ctx, cfg, sessions, key, remoteAddr, session, packetPool, packetWriter, maxPacketSize, wg, log)
			created = true
		}

		session.lastActivity.Store(time.Now().UnixMilli())
		if !enqueueUDPPacket(session, packetPool, buf[:n]) && queueLog.allow(limitLogInterval) {
			if created {
				log.Warnf("[forward] UDP session queue full before dial, dropping first packet from %s", remoteAddr)
			} else {
				log.Warnf("[forward] UDP session queue full, dropping packet from %s", remoteAddr)
			}
		}
		if session.ctx.Err() != nil {
			closeUDPSession(sessions, key, session)
		}
	}
}

// ReverseProxyUDP — reverse channel: src → dst to dstAddr
func ReverseProxyUDP(ctx context.Context, cfg UDPReverseConfigObj) {
	reverseProxyUDP(ctx, cfg)
}

func reverseProxyUDP(ctx context.Context, cfg UDPReverseConfigObj) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Src == nil || cfg.Dst == nil || cfg.DstAddr == nil {
		return
	}
	watchDone := make(chan struct{})
	defer close(watchDone)
	defer func() { _ = cfg.Src.Close() }()
	go func() {
		select {
		case <-ctx.Done():
			_ = cfg.Src.SetReadDeadline(time.Now())
		case <-watchDone:
		}
	}()

	writer := cfg.writer
	if writer == nil {
		writer = newPacketWriter(cfg.Dst)
	}
	maxPacketSize := clampUDPMaxPacketSize(cfg.MaxPacketSize)
	buf := make([]byte, udpReadBufferSize(maxPacketSize))
	for {
		n, err := cfg.Src.Read(buf)
		if err != nil {
			return
		}
		if n > maxPacketSize {
			continue
		}
		if n > 0 {
			if cfg.Activity != nil {
				cfg.Activity()
			}
			if err = writer.writeTo(buf[:n], cfg.DstAddr); err != nil {
				return
			}
		}
	}
}
