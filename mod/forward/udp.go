package forward

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
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

func enqueueUDPReversePacket(session *udpSessionObj, writer *udpReverseWriterObj, addr net.Addr, payload []byte) bool {
	if !writer.enqueue(addr, payload) {
		return false
	}
	session.lastActivity.Store(time.Now().UnixMilli())
	return true
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

func (t *udpSessionMapObj) compareAndDelete(key netip.AddrPort, session *udpSessionObj) {
	t.mu.Lock()
	if t.m[key] == session {
		delete(t.m, key)
	}
	t.mu.Unlock()
}

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
		session.lastActivity.Store(time.Now().UnixMilli())
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

// RunUDPLoop routes datagrams through per-source connected sessions until ctx ends.
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

// ReverseProxyUDP relays connected upstream datagrams to one packet destination.
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
