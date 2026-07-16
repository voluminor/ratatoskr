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

type udpLoopObj struct {
	cfg              UDPLoopConfigObj
	ctx              context.Context
	cancel           context.CancelFunc
	wg               *sync.WaitGroup
	stats            *statsObj
	log              yggcore.Logger
	limit            *admissionLimitObj
	sessions         *udpSessionMapObj
	packetPool       *udpBufferPoolObj
	reverseWriter    *udpReverseWriterObj
	maxPacketSize    int
	sessionQueueSize int
	sessionDrops     *atomic.Uint64
	limitLog         intervalLogObj
	readErrorLog     intervalLogObj
	oversizeLog      intervalLogObj
	queueLog         intervalLogObj
}

// //

func newUDPLoop(ctx context.Context, cfg UDPLoopConfigObj, wg *sync.WaitGroup, sharedLimit *admissionLimitObj, stats *statsObj) (*udpLoopObj, error) {
	if cfg.Timeout <= 0 {
		return nil, ErrInvalidSessionTimeout
	}
	if cfg.ListenConn == nil {
		return nil, fmt.Errorf("%w: UDP listen connection is nil", ErrInvalidMapping)
	}
	if cfg.Dial == nil {
		return nil, fmt.Errorf("%w: UDP dial function is nil", ErrInvalidMapping)
	}
	limit := sharedLimit
	if limit == nil {
		if cfg.MaxSessions < 0 {
			return nil, ErrInvalidLimit
		}
		limit = &admissionLimitObj{max: int64(cfg.MaxSessions)}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loopCtx, loopCancel := context.WithCancel(ctx)
	maxPacketSize := clampUDPMaxPacketSize(cfg.MaxPacketSize)
	packetPool := newUDPBufferPool(maxPacketSize)

	loop := &udpLoopObj{
		cfg:              cfg,
		ctx:              loopCtx,
		cancel:           loopCancel,
		wg:               wg,
		stats:            stats,
		log:              common.NormalizeLogger(cfg.Logger),
		limit:            limit,
		sessions:         newUDPSessionMap(),
		packetPool:       packetPool,
		maxPacketSize:    maxPacketSize,
		sessionQueueSize: boundedUDPQueueSize(maxPacketSize, udpSessionQueueBytes, udpSessionQueueMaxPackets),
	}
	var reverseDrops *atomic.Uint64
	if stats != nil {
		reverseDrops = &stats.reverseUDPDrops
		loop.sessionDrops = &stats.sessionUDPDrops
	}
	loop.reverseWriter = newUDPReverseWriter(loopCtx, cfg.ListenConn, effectiveUDPWriteTimeout(cfg.WriteTimeout), packetPool, maxPacketSize, reverseDrops)
	return loop, nil
}

// // // // // // // // // //

func (l *udpLoopObj) run() error {
	trackUDPWorker(l.wg, l.reverseWriter.run)
	trackUDPWorker(l.wg, l.cleanupLoop)
	readStop := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		select {
		case <-l.ctx.Done():
			_ = l.cfg.ListenConn.Close()
		case <-readStop:
		}
	}()
	defer func() {
		close(readStop)
		<-readDone
		l.cancel()
	}()
	return l.readLoop()
}

func (l *udpLoopObj) cleanupLoop() {
	ticker := time.NewTicker(udpCleanupInterval(l.cfg.Timeout))
	defer ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			for _, entry := range l.sessions.snapshot() {
				entry.session.stop()
			}
			return
		case <-ticker.C:
			now := time.Now().UnixMilli()
			for _, entry := range l.sessions.snapshot() {
				if now-entry.session.lastActivity.Load() > l.cfg.Timeout.Milliseconds() {
					l.log.Debugf("[forward] cleaning up inactive UDP session %v", entry.key)
					closeUDPSession(l.sessions, entry.key, entry.session)
				}
			}
		}
	}
}

func (l *udpLoopObj) readLoop() error {
	buf := make([]byte, udpReadBufferSize(l.maxPacketSize))
	backoff := time.Duration(0)
	errorStreak := ioErrorStreakObj{}
	for {
		n, remoteAddr, err := l.cfg.ListenConn.ReadFrom(buf)
		if err != nil {
			if l.ctx.Err() != nil {
				return nil
			}
			if errorStreak.terminal(err) {
				if l.stats != nil {
					l.stats.terminalErrors.Add(1)
				}
				return fmt.Errorf("forward: UDP read: %w", err)
			}
			if l.readErrorLog.allow(limitLogInterval) {
				l.log.Debugf("[forward] UDP read error: %v", err)
			}
			backoff = nextBackoff(backoff)
			if !sleepContext(l.ctx, backoff) {
				return nil
			}
			continue
		}
		errorStreak.reset()
		backoff = 0
		if n == 0 {
			continue
		}
		if n > l.maxPacketSize {
			if l.oversizeLog.allow(limitLogInterval) {
				l.log.Warnf("[forward] UDP packet from %s exceeds max packet size %d, dropping", remoteAddr, l.maxPacketSize)
			}
			continue
		}
		l.dispatch(remoteAddr, buf[:n])
	}
}

func (l *udpLoopObj) dispatch(remoteAddr net.Addr, packet []byte) {
	key, ok := udpSessionKey(remoteAddr)
	if !ok {
		return
	}
	session, created := l.session(key, remoteAddr)
	if session == nil {
		return
	}

	switch enqueueUDPPacket(session, l.packetPool, packet, l.sessionDrops) {
	case udpEnqueueQueued:
	case udpEnqueueFull:
		if l.queueLog.allow(limitLogInterval) {
			if created {
				l.log.Warnf("[forward] UDP session queue full before dial, dropping first packet from %s", remoteAddr)
			} else {
				l.log.Warnf("[forward] UDP session queue full, dropping packet from %s", remoteAddr)
			}
		}
	case udpEnqueueCanceled:
		if recordUDPChurnDrop(l.ctx, l.sessionDrops) && l.queueLog.allow(limitLogInterval) {
			l.log.Warnf("[forward] UDP session is closing, dropping packet from %s", remoteAddr)
		}
		closeUDPSession(l.sessions, key, session)
	}
}

func (l *udpLoopObj) session(key netip.AddrPort, remoteAddr net.Addr) (*udpSessionObj, bool) {
	if session, ok := l.sessions.load(key); ok {
		return session, false
	}
	if !l.limit.acquire() {
		if l.limitLog.allow(limitLogInterval) {
			l.log.Warnf("[forward] UDP session limit reached (%d), dropping packet from %s", l.limit.max, remoteAddr)
		}
		return nil, false
	}
	sessionCtx, sessionCancel := context.WithCancel(l.ctx)
	session := &udpSessionObj{
		ctx:    sessionCtx,
		cancel: sessionCancel,
		out:    make(chan *udpPacketObj, l.sessionQueueSize),
		limit:  l.limit,
	}
	session.lastActivity.Store(time.Now().UnixMilli())
	l.sessions.store(key, session)
	l.startSessionWorker(key, remoteAddr, session)
	return session, true
}

func (l *udpLoopObj) startSessionWorker(key netip.AddrPort, remoteAddr net.Addr, session *udpSessionObj) {
	trackUDPWorker(l.wg, func() {
		defer func() {
			l.sessions.compareAndDelete(key, session)
			session.finish()
		}()
		dialCtx, cancel := dialTimeoutContext(session.ctx, l.cfg.DialTimeout)
		fwdConn, err := l.cfg.Dial(dialCtx, remoteAddr)
		cancel()
		if err != nil {
			if l.ctx.Err() == nil {
				l.log.Errorf("[forward] failed to dial upstream: %s", err)
			}
			drainUDPPackets(session.out, l.packetPool)
			return
		}
		if !session.setConn(fwdConn) {
			_ = fwdConn.Close()
			drainUDPPackets(session.out, l.packetPool)
			return
		}
		reverseDone := make(chan struct{})
		go func() {
			defer close(reverseDone)
			reverseReadUDP(session.ctx, fwdConn, l.maxPacketSize, func(payload []byte) bool {
				if enqueueUDPReversePacket(session, l.reverseWriter, remoteAddr, payload) {
					return true
				}
				return session.ctx.Err() == nil
			})
			session.stop()
		}()
		if err := runUDPWriter(session.ctx, session, l.packetPool); err != nil && session.ctx.Err() == nil {
			l.log.Debugf("[forward] session write error: %s", err)
		}
		session.stop()
		<-reverseDone
	})
}

// //

func runUDPLoopWithWait(ctx context.Context, cfg UDPLoopConfigObj, wg *sync.WaitGroup, sharedLimit *admissionLimitObj, stats *statsObj) error {
	loop, err := newUDPLoop(ctx, cfg, wg, sharedLimit, stats)
	if err != nil {
		return err
	}
	return loop.run()
}
