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
	udpReverseQueueBytes      = 256 * 1024
	udpReverseQueueMaxPackets = 256
)

type udpEnqueueResult uint8

const (
	udpEnqueueQueued udpEnqueueResult = iota
	udpEnqueueCanceled
	udpEnqueueFull
)

// //

type udpSessionObj struct {
	ctx          context.Context
	connMu       sync.RWMutex
	conn         net.Conn
	out          chan *udpPacketObj
	lastActivity atomic.Int64
	cancel       context.CancelFunc
	stopOnce     sync.Once
	finishOnce   sync.Once
	limit        *admissionLimitObj
}

type udpPacketObj struct {
	buf []byte
}

type udpBufferPoolObj struct {
	size int
	pool sync.Pool
}

type udpReversePacketObj struct {
	packet *udpPacketObj
	addr   net.Addr
}

// udpReverseWriterObj is shared by all sessions of one mapping. Session readers
// only enqueue, so a slow or broken destination can retain at most one blocked
// writer and a bounded amount of queued memory per mapping.
type udpReverseWriterObj struct {
	ctx          context.Context
	dst          net.PacketConn
	writeTimeout time.Duration
	pool         *udpBufferPoolObj
	out          chan udpReversePacketObj
	drops        *atomic.Uint64
}

func newUDPReverseWriter(ctx context.Context, dst net.PacketConn, writeTimeout time.Duration, pool *udpBufferPoolObj, maxPacketSize int, drops *atomic.Uint64) *udpReverseWriterObj {
	return &udpReverseWriterObj{
		ctx:          ctx,
		dst:          dst,
		writeTimeout: writeTimeout,
		pool:         pool,
		out:          make(chan udpReversePacketObj, boundedUDPQueueSize(maxPacketSize, udpReverseQueueBytes, udpReverseQueueMaxPackets)),
		drops:        drops,
	}
}

func (w *udpReverseWriterObj) enqueue(addr net.Addr, payload []byte) bool {
	packet := w.pool.get(len(payload))
	copy(packet.buf, payload)
	select {
	case w.out <- udpReversePacketObj{packet: packet, addr: addr}:
		return true
	case <-w.ctx.Done():
		w.pool.put(packet)
		return false
	default:
		w.pool.put(packet)
		if w.drops != nil {
			w.drops.Add(1)
		}
		return false
	}
}

func (w *udpReverseWriterObj) run() {
	defer func() {
		for {
			select {
			case packet := <-w.out:
				w.pool.put(packet.packet)
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-w.ctx.Done():
			return
		case packet := <-w.out:
			if w.writeTimeout > 0 {
				_ = w.dst.SetWriteDeadline(time.Now().Add(w.writeTimeout))
			}
			_, err := w.dst.WriteTo(packet.packet.buf, packet.addr)
			w.pool.put(packet.packet)
			if err != nil {
				if w.ctx.Err() != nil {
					return
				}
				if w.drops != nil {
					w.drops.Add(1)
				}
			}
		}
	}
}

func newUDPBufferPool(size int) *udpBufferPoolObj {
	if size <= 0 {
		size = maxUDPDatagramSize
	}
	p := &udpBufferPoolObj{size: size}
	p.pool.New = func() any {
		return &udpPacketObj{buf: make([]byte, p.size)}
	}
	return p
}

func (p *udpBufferPoolObj) get(n int) *udpPacketObj {
	// Pooled buffers are always exactly p.size and n never exceeds it.
	packet := p.pool.Get().(*udpPacketObj)
	packet.buf = packet.buf[:n]
	return packet
}

func (p *udpBufferPoolObj) put(packet *udpPacketObj) {
	if packet == nil || cap(packet.buf) != p.size {
		return
	}
	packet.buf = packet.buf[:p.size]
	p.pool.Put(packet)
}

func (s *udpSessionObj) stop() {
	s.stopOnce.Do(func() {
		s.cancel()
		s.connMu.RLock()
		conn := s.conn
		s.connMu.RUnlock()
		if conn != nil {
			_ = conn.Close()
		}
	})
}

func (s *udpSessionObj) finish() {
	s.finishOnce.Do(func() {
		s.stop()
		s.limit.release()
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
// the map lock (session.stop() runs a conn Close syscall and must not block it).
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
	session.stop()
}

func boundedUDPQueueSize(maxPacketSize, byteBudget, maxPackets int) int {
	maxPacketSize = clampUDPMaxPacketSize(maxPacketSize)
	n := byteBudget / maxPacketSize
	if n < 1 {
		return 1
	}
	if n > maxPackets {
		return maxPackets
	}
	return n
}

func enqueueUDPPacket(session *udpSessionObj, pool *udpBufferPoolObj, packet []byte, drops *atomic.Uint64) udpEnqueueResult {
	buf := pool.get(len(packet))
	copy(buf.buf, packet)
	select {
	case session.out <- buf:
		return udpEnqueueQueued
	case <-session.ctx.Done():
		pool.put(buf)
		return udpEnqueueCanceled
	default:
		pool.put(buf)
		if drops != nil {
			drops.Add(1)
		}
		return udpEnqueueFull
	}
}

func recordUDPChurnDrop(loopCtx context.Context, drops *atomic.Uint64) bool {
	if loopCtx.Err() != nil {
		return false
	}
	if drops != nil {
		drops.Add(1)
	}
	return true
}

func drainUDPPackets(ch <-chan *udpPacketObj, pool *udpBufferPoolObj) {
	for {
		select {
		case packet := <-ch:
			pool.put(packet)
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

func (m *Obj) prepareUDP(
	mappings []UDPMappingObj,
	listen func(UDPMappingObj) (net.PacketConn, string, error),
	logMapping func(UDPMappingObj),
	acceptLabel string,
	dial func(UDPMappingObj, context.Context, net.Addr) (net.Conn, error),
) ([]udpStartObj, error) {
	starts := make([]udpStartObj, 0, len(mappings))
	for _, mapping := range mappings {
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

func (m *Obj) runUDPStarts(ctx context.Context, starts []udpStartObj) {
	for _, start := range starts {
		m.wg.Add(1)
		go func(st udpStartObj) {
			defer m.wg.Done()
			defer func() { _ = st.conn.Close() }()
			st.logMapping(st.mapping)

			err := runUDPLoopWithWait(ctx, UDPLoopConfigObj{
				Logger:     m.log,
				ListenConn: st.conn,
				Dial: func(ctx context.Context, addr net.Addr) (net.Conn, error) {
					return st.dial(st.mapping, ctx, addr)
				},
				DialTimeout:   m.dialTimeout,
				WriteTimeout:  m.udpWriteTimeout,
				MaxPacketSize: m.effectiveUDPMaxPacketSize(),
				Timeout:       m.timeout,
			}, &m.wg, &m.udpLimit, &m.stats)
			if err != nil && ctx.Err() == nil {
				m.log.Errorf("[forward] UDP mapping stopped: %v", err)
			}
		}(start)
	}
}

// //

func (m *Obj) prepareLocalUDP() ([]udpStartObj, error) {
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

func (m *Obj) prepareRemoteUDP() ([]udpStartObj, error) {
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
// Cancelling ctx closes cfg.ListenConn to unblock reads and then waits for session
// workers. Configuration and terminal read failures are returned to the caller.
func RunUDPLoop(ctx context.Context, cfg UDPLoopConfigObj) error {
	var wg sync.WaitGroup
	err := runUDPLoopWithWait(ctx, cfg, &wg, nil, nil)
	wg.Wait()
	return err
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
			_, err := conn.Write(packet.buf)
			pool.put(packet)
			if err != nil {
				return err
			}
		}
	}
}

func startUDPSessionWorker(ctx context.Context, cfg UDPLoopConfigObj, sessions *udpSessionMapObj, key netip.AddrPort, remoteAddr net.Addr, session *udpSessionObj, pool *udpBufferPoolObj, reverseWriter *udpReverseWriterObj, maxPacketSize int, wg *sync.WaitGroup, log yggcore.Logger) {
	trackUDPWorker(wg, func() {
		defer func() {
			sessions.compareAndDelete(key, session)
			session.finish()
		}()
		dialCtx, cancel := dialTimeoutContext(session.ctx, cfg.DialTimeout)
		fwdConn, err := cfg.Dial(dialCtx, remoteAddr)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				log.Errorf("[forward] failed to dial upstream: %s", err)
			}
			drainUDPPackets(session.out, pool)
			return
		}
		if !session.setConn(fwdConn) {
			_ = fwdConn.Close()
			drainUDPPackets(session.out, pool)
			return
		}
		reverseDone := make(chan struct{})
		go func() {
			defer close(reverseDone)
			reverseReadUDP(session.ctx, fwdConn, maxPacketSize, func(payload []byte) bool {
				session.lastActivity.Store(time.Now().UnixMilli())
				if reverseWriter.enqueue(remoteAddr, payload) {
					return true
				}
				return session.ctx.Err() == nil
			})
			session.stop()
		}()
		if err := runUDPWriter(session.ctx, session, pool); err != nil && session.ctx.Err() == nil {
			log.Debugf("[forward] session write error: %s", err)
		}
		session.stop()
		<-reverseDone
	})
}

func runUDPLoopWithWait(ctx context.Context, cfg UDPLoopConfigObj, wg *sync.WaitGroup, sharedLimit *admissionLimitObj, stats *statsObj) error {
	if ctx == nil {
		ctx = context.Background()
	}
	log := common.NormalizeLogger(cfg.Logger)
	if cfg.Timeout <= 0 {
		return ErrInvalidSessionTimeout
	}
	if cfg.ListenConn == nil {
		return fmt.Errorf("%w: UDP listen connection is nil", ErrInvalidMapping)
	}
	if cfg.Dial == nil {
		return fmt.Errorf("%w: UDP dial function is nil", ErrInvalidMapping)
	}
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	limit := sharedLimit
	if limit == nil {
		if cfg.MaxSessions < 0 {
			return ErrInvalidLimit
		}
		limit = &admissionLimitObj{max: int64(cfg.MaxSessions)}
	}
	sessions := newUDPSessionMap()
	limitLog := intervalLogObj{}
	readErrorLog := intervalLogObj{}
	oversizeLog := intervalLogObj{}
	queueLog := intervalLogObj{}
	maxPacketSize := clampUDPMaxPacketSize(cfg.MaxPacketSize)
	packetPool := newUDPBufferPool(maxPacketSize)
	var reverseDrops *atomic.Uint64
	var sessionDrops *atomic.Uint64
	if stats != nil {
		reverseDrops = &stats.reverseUDPDrops
		sessionDrops = &stats.sessionUDPDrops
	}
	reverseWriter := newUDPReverseWriter(loopCtx, cfg.ListenConn, effectiveUDPWriteTimeout(cfg.WriteTimeout), packetPool, maxPacketSize, reverseDrops)
	trackUDPWorker(wg, reverseWriter.run)
	sessionQueueSize := boundedUDPQueueSize(maxPacketSize, udpSessionQueueBytes, udpSessionQueueMaxPackets)
	readStop := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		select {
		case <-loopCtx.Done():
			_ = cfg.ListenConn.Close()
		case <-readStop:
		}
	}()
	defer func() {
		close(readStop)
		<-readDone
	}()

	// Clean up inactive sessions
	trackUDPWorker(wg, func() {
		ticker := time.NewTicker(udpCleanupInterval(cfg.Timeout))
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				for _, e := range sessions.snapshot() {
					e.session.stop()
				}
				return
			case <-ticker.C:
				now := time.Now().UnixMilli()
				for _, e := range sessions.snapshot() {
					if now-e.session.lastActivity.Load() > cfg.Timeout.Milliseconds() {
						log.Debugf("[forward] cleaning up inactive UDP session %v", e.key)
						closeUDPSession(sessions, e.key, e.session)
					}
				}
			}
		}
	})

	buf := make([]byte, udpReadBufferSize(maxPacketSize))
	backoff := time.Duration(0)
	errorStreak := ioErrorStreakObj{}
	for {
		n, remoteAddr, err := cfg.ListenConn.ReadFrom(buf)
		if err != nil {
			if loopCtx.Err() != nil {
				return nil
			}
			if errorStreak.terminal(err) {
				if stats != nil {
					stats.terminalErrors.Add(1)
				}
				return fmt.Errorf("forward: UDP read: %w", err)
			}
			if readErrorLog.allow(limitLogInterval) {
				log.Debugf("[forward] UDP read error: %v", err)
			}
			backoff = nextBackoff(backoff)
			if !sleepContext(loopCtx, backoff) {
				return nil
			}
			continue
		}
		errorStreak.reset()
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
			if !limit.acquire() {
				if limitLog.allow(limitLogInterval) {
					log.Warnf("[forward] UDP session limit reached (%d), dropping packet from %s", limit.max, remoteAddr)
				}
				continue
			}
			sessCtx, sessCancel := context.WithCancel(loopCtx)
			session = &udpSessionObj{
				ctx:    sessCtx,
				cancel: sessCancel,
				out:    make(chan *udpPacketObj, sessionQueueSize),
				limit:  limit,
			}
			session.lastActivity.Store(time.Now().UnixMilli())
			sessions.store(key, session)
			startUDPSessionWorker(loopCtx, cfg, sessions, key, remoteAddr, session, packetPool, reverseWriter, maxPacketSize, wg, log)
			created = true
		}

		session.lastActivity.Store(time.Now().UnixMilli())
		switch enqueueUDPPacket(session, packetPool, buf[:n], sessionDrops) {
		case udpEnqueueQueued:
		case udpEnqueueFull:
			if queueLog.allow(limitLogInterval) {
				if created {
					log.Warnf("[forward] UDP session queue full before dial, dropping first packet from %s", remoteAddr)
				} else {
					log.Warnf("[forward] UDP session queue full, dropping packet from %s", remoteAddr)
				}
			}
		case udpEnqueueCanceled:
			// The read loop is still live, so this is session churn rather than
			// global shutdown. Account for the packet that hit the retiring entry.
			if recordUDPChurnDrop(loopCtx, sessionDrops) && queueLog.allow(limitLogInterval) {
				log.Warnf("[forward] UDP session is closing, dropping packet from %s", remoteAddr)
			}
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
	writeTimeout := effectiveUDPWriteTimeout(cfg.WriteTimeout)
	reverseReadUDP(ctx, cfg.Src, cfg.MaxPacketSize, func(payload []byte) bool {
		if cfg.Activity != nil {
			cfg.Activity()
		}
		if writeTimeout > 0 {
			_ = cfg.Dst.SetWriteDeadline(time.Now().Add(writeTimeout))
		}
		_, err := cfg.Dst.WriteTo(payload, cfg.DstAddr)
		return err == nil
	})
}

func reverseReadUDP(ctx context.Context, src net.Conn, maxPacketSize int, consume func([]byte) bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if src == nil || consume == nil {
		return
	}
	watchStop := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = src.SetReadDeadline(time.Now())
		case <-watchStop:
		}
	}()
	defer func() {
		close(watchStop)
		<-watchDone
	}()
	defer func() { _ = src.Close() }()

	maxPacketSize = clampUDPMaxPacketSize(maxPacketSize)
	buf := make([]byte, udpReadBufferSize(maxPacketSize))
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if n > maxPacketSize {
			continue
		}
		if n > 0 {
			if !consume(buf[:n]) {
				return
			}
		}
	}
}
