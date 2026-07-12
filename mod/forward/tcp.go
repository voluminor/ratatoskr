package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
)

// // // // // // // // // //

var tcpCopyBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

type tcpLimitObj struct {
	max    int64
	active atomic.Int64
}

type tcpStartObj struct {
	mapping     TCPMappingObj
	listener    net.Listener
	logMapping  func(TCPMappingObj)
	acceptLabel string
	dial        func(TCPMappingObj, context.Context) (net.Conn, error)
	target      func(TCPMappingObj) string
}

func (l *tcpLimitObj) acquire() bool {
	if l == nil {
		return true
	}
	if l.max <= 0 {
		l.active.Add(1)
		return true
	}
	for {
		active := l.active.Load()
		if active >= l.max {
			return false
		}
		if l.active.CompareAndSwap(active, active+1) {
			return true
		}
	}
}

func (l *tcpLimitObj) release() {
	if l != nil {
		l.active.Add(-1)
	}
}

// ProxyTCPContext proxies TCP and unblocks both directions when ctx is cancelled.
func ProxyTCPContext(ctx context.Context, c1, c2 net.Conn, closeTimeout time.Duration) {
	proxyTCPContext(ctx, c1, c2, closeTimeout, -1)
}

func proxyTCPContext(ctx context.Context, c1, c2 net.Conn, closeTimeout, idleTimeout time.Duration) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c1.Close()
			_ = c2.Close()
		case <-done:
		}
	}()
	defer close(done)

	proxyTCP(c1, c2, closeTimeout, idleTimeout)
}

func ProxyTCP(c1, c2 net.Conn, closeTimeout time.Duration) {
	proxyTCP(c1, c2, closeTimeout, -1)
}

type closeWriterInterface interface {
	CloseWrite() error
}

type tcpCopyResultObj struct {
	dst net.Conn
	err error
}

func copyTCP(dst, src net.Conn, errCh chan<- tcpCopyResultObj, activity func()) {
	buf := tcpCopyBufferPool.Get().(*[]byte)
	_, err := copyTCPBuffer(dst, src, *buf, activity)
	tcpCopyBufferPool.Put(buf)
	errCh <- tcpCopyResultObj{dst: dst, err: err}
}

func copyTCPBuffer(dst, src net.Conn, buf []byte, activity func()) (int64, error) {
	var written int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			activity()
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}

func closeTCPWrite(conn net.Conn) bool {
	closeWriter, ok := conn.(closeWriterInterface)
	if !ok {
		return false
	}
	return closeWriter.CloseWrite() == nil
}

func resetTCPIdleTimer(timer *time.Timer, timeout time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(timeout)
}

func waitTCPHalfClose(errCh <-chan tcpCopyResultObj, activityCh <-chan struct{}, c1, c2 net.Conn, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-errCh:
			return
		case <-activityCh:
			resetTCPIdleTimer(timer, timeout)
		case <-timer.C:
			_ = c1.Close()
			_ = c2.Close()
			return
		}
	}
}

func waitTCPFinalClose(errCh <-chan tcpCopyResultObj, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-errCh:
	case <-timer.C:
	}
}

func proxyTCP(c1, c2 net.Conn, closeTimeout, idleTimeout time.Duration) {
	errCh := make(chan tcpCopyResultObj, 2)
	var halfCloseArmed atomic.Bool
	halfCloseActivity := make(chan struct{}, 1)
	// Per-conn last-armed deadline; the shared gate re-arms only past the
	// half-interval, cutting SetDeadline syscalls on the hot copy path.
	var c1Deadline, c2Deadline atomic.Int64
	activity := func() {
		if idleTimeout > 0 {
			now := time.Now()
			common.RefreshDeadline(now, idleTimeout, &c1Deadline, c1, false)
			common.RefreshDeadline(now, idleTimeout, &c2Deadline, c2, false)
		}
		if halfCloseArmed.Load() {
			select {
			case halfCloseActivity <- struct{}{}:
			default:
			}
		}
	}
	activity()
	go copyTCP(c1, c2, errCh, activity)
	go copyTCP(c2, c1, errCh, activity)

	first := <-errCh
	if first.err == nil && closeTCPWrite(first.dst) {
		halfCloseArmed.Store(true)
		activity()
		waitTCPHalfClose(errCh, halfCloseActivity, c1, c2, closeTimeout)
		_ = c1.Close()
		_ = c2.Close()
		return
	}

	_ = c1.Close()
	_ = c2.Close()
	waitTCPFinalClose(errCh, closeTimeout)
}

func (m *ManagerObj) startTCPProxy(ctx context.Context, client net.Conn, limiter *tcpLimitObj, dial func(context.Context) (net.Conn, error), target string) {
	defer m.wg.Done()
	defer limiter.release()

	dialCtx, cancel := dialTimeoutContext(ctx, m.dialTimeout)
	remote, err := dial(dialCtx)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			m.log.Errorf("[forward] failed to dial %s: %s", target, err)
		}
		_ = client.Close()
		return
	}
	proxyTCPContext(ctx, client, remote, DefaultTCPCloseTimeout, m.tcpIdleTimeout)
}

func (m *ManagerObj) prepareTCP(
	mappings []TCPMappingObj,
	listen func(TCPMappingObj) (net.Listener, string, error),
	logMapping func(TCPMappingObj),
	acceptLabel string,
	dial func(TCPMappingObj, context.Context) (net.Conn, error),
	target func(TCPMappingObj) string,
) ([]tcpStartObj, error) {
	starts := make([]tcpStartObj, 0, len(mappings))
	for _, mapping := range mappings {
		if err := validateTCPMapping(mapping); err != nil {
			closeTCPStarts(starts)
			return nil, err
		}
		listener, listenAddr, err := listen(mapping)
		if err != nil {
			closeTCPStarts(starts)
			return nil, fmt.Errorf("listen on %s TCP %s: %w", acceptLabel, listenAddr, err)
		}
		starts = append(starts, tcpStartObj{
			mapping:     mapping,
			listener:    listener,
			logMapping:  logMapping,
			acceptLabel: acceptLabel,
			dial:        dial,
			target:      target,
		})
	}
	return starts, nil
}

func closeTCPStarts(starts []tcpStartObj) {
	for _, start := range starts {
		_ = start.listener.Close()
	}
}

func validateTCPMapping(mapping TCPMappingObj) error {
	if mapping.Listen == nil {
		return fmt.Errorf("%w: TCP listen address is nil", ErrInvalidMapping)
	}
	if mapping.Mapped == nil {
		return fmt.Errorf("%w: TCP mapped address is nil", ErrInvalidMapping)
	}
	return nil
}

func (m *ManagerObj) runTCPStarts(ctx context.Context, starts []tcpStartObj) {
	for _, start := range starts {
		m.wg.Add(1)
		go func(st tcpStartObj) {
			defer m.wg.Done()
			defer func() { _ = st.listener.Close() }()
			st.logMapping(st.mapping)

			limiter := m.newTCPLimit()
			limitLog := intervalLogObj{}
			backoff := time.Duration(0)

			acceptCtx, acceptCancel := context.WithCancel(ctx)
			defer acceptCancel()
			go func() {
				<-acceptCtx.Done()
				_ = st.listener.Close()
			}()

			for {
				c, err := st.listener.Accept()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					m.log.Errorf("[forward] %s TCP accept error: %s", st.acceptLabel, err)
					backoff = nextBackoff(backoff)
					if !sleepContext(ctx, backoff) {
						return
					}
					continue
				}
				backoff = 0
				if !limiter.acquire() {
					if limitLog.allow(limitLogInterval) {
						m.log.Warnf("[forward] TCP connection limit reached (%d), dropping %s", limiter.max, c.RemoteAddr())
					}
					_ = c.Close()
					continue
				}
				m.wg.Add(1)
				go m.startTCPProxy(ctx, c, limiter, func(dialCtx context.Context) (net.Conn, error) {
					return st.dial(st.mapping, dialCtx)
				}, st.target(st.mapping))
			}
		}(start)
	}
}

// //

func (m *ManagerObj) prepareLocalTCP() ([]tcpStartObj, error) {
	return m.prepareTCP(m.localTCPs,
		func(mp TCPMappingObj) (net.Listener, string, error) {
			listener, err := net.ListenTCP("tcp", mp.Listen)
			return listener, mp.Listen.String(), err
		},
		func(mp TCPMappingObj) {
			m.log.Infof("[forward] mapping local TCP port %d to Yggdrasil %s", mp.Listen.Port, mp.Mapped)
		},
		"local",
		func(mp TCPMappingObj, dialCtx context.Context) (net.Conn, error) {
			mappedAddr := fmt.Sprintf("[%s]:%d", mp.Mapped.IP, mp.Mapped.Port)
			return m.node.DialContext(dialCtx, "tcp", mappedAddr)
		},
		func(mp TCPMappingObj) string {
			return mp.Mapped.String()
		},
	)
}

func (m *ManagerObj) prepareRemoteTCP() ([]tcpStartObj, error) {
	return m.prepareTCP(m.remoteTCPs,
		func(mp TCPMappingObj) (net.Listener, string, error) {
			addr := fmt.Sprintf("[%s]:%d", m.node.Address(), mp.Listen.Port)
			listener, err := m.node.Listen("tcp", addr)
			return listener, addr, err
		},
		func(mp TCPMappingObj) {
			m.log.Infof("[forward] mapping Yggdrasil TCP port %d to %s", mp.Listen.Port, mp.Mapped)
		},
		"remote",
		func(mp TCPMappingObj, dialCtx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(dialCtx, "tcp", mp.Mapped.String())
		},
		func(mp TCPMappingObj) string {
			return mp.Mapped.String()
		},
	)
}
