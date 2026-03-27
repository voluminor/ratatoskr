package forward

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type udpSessionObj struct {
	conn         net.Conn
	lastActivity atomic.Int64
	cancel       context.CancelFunc
	closeOnce    sync.Once
	counter      *atomic.Int64
}

func (s *udpSessionObj) close() {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.conn.Close()
		if s.counter != nil {
			s.counter.Add(-1)
		}
	})
}

// //

func (m *ManagerObj) startLocalUDP(ctx context.Context) {
	for _, mapping := range m.localUDPs {
		m.wg.Add(1)
		go func(mp UDPMappingObj) {
			defer m.wg.Done()
			conn, err := net.ListenUDP("udp", mp.Listen)
			if err != nil {
				m.log.Errorf("[forward] failed to listen on local UDP %s: %s", mp.Listen, err)
				return
			}
			defer conn.Close()
			m.log.Infof("[forward] mapping local UDP port %d to Yggdrasil %s", mp.Listen.Port, mp.Mapped)

			acceptCtx, acceptCancel := context.WithCancel(ctx)
			defer acceptCancel()
			go func() {
				<-acceptCtx.Done()
				conn.Close()
			}()

			RunUDPLoop(ctx, m.log, m.node.MTU(), conn, func() (net.Conn, error) {
				return m.node.DialContext(ctx, "udp", fmt.Sprintf("[%s]:%d", mp.Mapped.IP, mp.Mapped.Port))
			}, m.timeout, m.maxUDPSessions)
		}(mapping)
	}
}

func (m *ManagerObj) startRemoteUDP(ctx context.Context) {
	for _, mapping := range m.remoteUDPs {
		m.wg.Add(1)
		go func(mp UDPMappingObj) {
			defer m.wg.Done()
			addr := fmt.Sprintf("[%s]:%d", m.node.Address(), mp.Listen.Port)
			conn, err := m.node.ListenPacket("udp", addr)
			if err != nil {
				m.log.Errorf("[forward] failed to listen on Yggdrasil UDP %s: %s", addr, err)
				return
			}
			defer conn.Close()
			m.log.Infof("[forward] mapping Yggdrasil UDP port %d to %s", mp.Listen.Port, mp.Mapped)

			acceptCtx, acceptCancel := context.WithCancel(ctx)
			defer acceptCancel()
			go func() {
				<-acceptCtx.Done()
				conn.Close()
			}()

			RunUDPLoop(ctx, m.log, m.node.MTU(), conn, func() (net.Conn, error) {
				return net.DialUDP("udp", nil, mp.Mapped)
			}, m.timeout, m.maxUDPSessions)
		}(mapping)
	}
}

// //

// RunUDPLoop reads packets, routes them to sessions via dialFn,
// and cleans up inactive ones by timeout. maxSessions: 0 = unlimited
func RunUDPLoop(ctx context.Context, log yggcore.Logger, mtu uint64, listenConn net.PacketConn, dialFn func() (net.Conn, error), timeout time.Duration, maxSessions int) {
	var sessionCount atomic.Int64
	sessions := sync.Map{}

	// Clean up inactive sessions
	go func() {
		ticker := time.NewTicker(timeout / 4)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				sessions.Range(func(_, v any) bool {
					v.(*udpSessionObj).close()
					return true
				})
				return
			case <-ticker.C:
				now := time.Now().UnixMilli()
				sessions.Range(func(k, v any) bool {
					s := v.(*udpSessionObj)
					if now-s.lastActivity.Load() > timeout.Milliseconds() {
						log.Debugf("[forward] cleaning up inactive UDP session %s", k)
						s.close()
						sessions.Delete(k)
					}
					return true
				})
			}
		}
	}()

	buf := make([]byte, mtu)
	for {
		n, remoteAddr, err := listenConn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Debugf("[forward] UDP read error: %v", err)
			continue
		}
		if n == 0 {
			continue
		}

		key := remoteAddr.String()
		val, ok := sessions.Load(key)
		if !ok {
			if maxSessions > 0 && sessionCount.Load() >= int64(maxSessions) {
				log.Warnf("[forward] UDP session limit reached (%d), dropping packet from %s", maxSessions, remoteAddr)
				continue
			}
			fwdConn, err := dialFn()
			if err != nil {
				log.Errorf("[forward] failed to dial upstream: %s", err)
				continue
			}
			sessCtx, sessCancel := context.WithCancel(ctx)
			session := &udpSessionObj{
				conn:    fwdConn,
				cancel:  sessCancel,
				counter: &sessionCount,
			}
			session.lastActivity.Store(time.Now().UnixMilli())
			sessionCount.Add(1)
			sessions.Store(key, session)
			go ReverseProxyUDP(sessCtx, mtu, listenConn, remoteAddr, fwdConn)
			val = session
		}

		session := val.(*udpSessionObj)
		session.lastActivity.Store(time.Now().UnixMilli())
		if _, err = session.conn.Write(buf[:n]); err != nil {
			log.Debugf("[forward] session write error: %s", err)
			session.close()
			sessions.Delete(key)
		}
	}
}

// ReverseProxyUDP — reverse channel: src → dst to dstAddr
func ReverseProxyUDP(ctx context.Context, mtu uint64, dst net.PacketConn, dstAddr net.Addr, src net.Conn) {
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = src.SetReadDeadline(time.Now())
		case <-watchDone:
		}
	}()

	buf := make([]byte, mtu)
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			if _, err = dst.WriteTo(buf[:n], dstAddr); err != nil {
				return
			}
		}
	}
}
