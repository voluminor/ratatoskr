package socks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

const (
	defaultMaxConnections   = 256
	defaultHandshakeTimeout = 10 * time.Second
	defaultDialTimeout      = 10 * time.Second
	defaultTunnelIdleTime   = 5 * time.Minute
	defaultUnixSocketMode   = 0o600
	acceptRetryMinDelay     = 10 * time.Millisecond
	acceptRetryMaxDelay     = time.Second

	defaultMaxAssociateTargets = 1024
)

func effectiveMaxConnections(n int) int {
	switch {
	case n == 0:
		return defaultMaxConnections
	case n < 0:
		return -1
	default:
		return n
	}
}

// effectiveDuration resolves a caller timeout: 0 → def (safe default),
// <0 → 0 (disabled), else the value as given.
func effectiveDuration(d, def time.Duration) time.Duration {
	switch {
	case d == 0:
		return def
	case d < 0:
		return 0
	default:
		return d
	}
}

func finishHandshake(_ context.Context, writer io.Writer, request *socks5.Request) error {
	if conn, ok := writer.(*limitedConnObj); ok {
		if request != nil && request.Command == statute.CommandAssociate {
			conn.finishHandshakeWithoutTunnelIdle()
		} else {
			conn.finishHandshake()
		}
	}
	return nil
}

func retryableAcceptError(err error) bool {
	return errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ENOBUFS) ||
		errors.Is(err, syscall.ECONNABORTED)
}

// Obj — SOCKS5 proxy server over Yggdrasil
type Obj struct {
	listener          net.Listener
	addr              string
	isUnix            bool
	logger            yggcore.Logger
	maxConnections    atomic.Int64
	dialTimeout       time.Duration
	tunnelIdleTimeout time.Duration
	limiter           *common.DynamicLimitObj
	associateLimiter  *common.DynamicLimitObj
	mu                sync.Mutex
	serveWG           *sync.WaitGroup
}

// ConfigObj contains all SOCKS5 startup parameters.
type ConfigObj struct {
	// Network dials outbound connections through Yggdrasil.
	Network proxy.ContextDialer
	// Address: TCP "127.0.0.1:1080" or Unix "/tmp/ygg.sock"
	Addr string
	// Name resolver (.pk.ygg, DNS)
	Resolver socks5.NameResolver
	// Verbose logging for each connection
	Verbose bool
	// Logger; nil → no logging
	Logger yggcore.Logger
	// Maximum simultaneous connections; 0 → safe default, <0 → unlimited
	MaxConnections int
	// Handshake timeout; 0 → safe default, <0 → disabled
	HandshakeTimeout time.Duration
	// Outbound dial timeout; 0 -> safe default, <0 -> disabled
	DialTimeout time.Duration
	// Established tunnel idle timeout; 0 -> safe default, <0 -> disabled
	TunnelIdleTimeout time.Duration
	// Optional SOCKS5 username/password credentials
	Credentials CredentialsInterface
}

// New creates and starts a SOCKS server.
func New(cfg ConfigObj) (*Obj, error) {
	s := NewDisabled()
	if err := s.Start(cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// NewDisabled creates a SOCKS server handle without opening a listener.
func NewDisabled() *Obj {
	return &Obj{}
}

// //

func (s *Obj) newConnectionLimit() *common.DynamicLimitObj {
	limiter := common.NewDynamicLimit(int(s.maxConnections.Load()))
	s.limiter = limiter
	return limiter
}

func (s *Obj) MaxConnections() int {
	return int(s.maxConnections.Load())
}

func (s *Obj) SetMaxConnections(n int) {
	next := effectiveMaxConnections(n)
	s.maxConnections.Store(int64(next))
	s.mu.Lock()
	limiter := s.limiter
	s.mu.Unlock()
	if limiter != nil {
		limiter.Set(next)
	}
}

// ActiveConnections — current number of live tracked connections; 0 when disabled.
// Counts the tracked connection set, not the accept semaphore (which reserves a
// slot for the pending Accept and so never reads as the live-connection count).
func (s *Obj) ActiveConnections() int {
	s.mu.Lock()
	ln, _ := s.listener.(*limitedListenerObj)
	s.mu.Unlock()
	if ln == nil {
		return 0
	}
	ln.mu.Lock()
	n := len(ln.conns)
	ln.mu.Unlock()
	return n
}

// DialTimeout — immutable outbound dial timeout set at Start.
func (s *Obj) DialTimeout() time.Duration {
	return s.dialTimeout
}

// TunnelIdleTimeout — immutable tunnel idle timeout set at Start.
func (s *Obj) TunnelIdleTimeout() time.Duration {
	return s.tunnelIdleTimeout
}

// //

// Start opens the listener and launches the server goroutine.
func (s *Obj) Start(cfg ConfigObj) error {
	if cfg.Network == nil {
		return ErrNetworkRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return fmt.Errorf("%w on %s", ErrAlreadyEnabled, s.addr)
	}

	log := common.NormalizeLogger(cfg.Logger)
	s.maxConnections.Store(int64(effectiveMaxConnections(cfg.MaxConnections)))
	s.dialTimeout = effectiveDuration(cfg.DialTimeout, defaultDialTimeout)
	s.tunnelIdleTimeout = effectiveDuration(cfg.TunnelIdleTimeout, defaultTunnelIdleTime)
	s.associateLimiter = common.NewDynamicLimit(defaultMaxAssociateTargets)
	associateResolver := cfg.Resolver
	if associateResolver == nil {
		associateResolver = socks5.DNSResolver{}
	}
	opts := []socks5.Option{
		socks5.WithDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
			timeout := s.dialTimeout
			if timeout <= 0 {
				return cfg.Network.DialContext(ctx, network, addr)
			}
			if ctx == nil {
				ctx = context.Background()
			}
			dialCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return cfg.Network.DialContext(dialCtx, network, addr)
		}),
		socks5.WithConnectMiddleware(finishHandshake),
		socks5.WithBindMiddleware(finishHandshake),
		socks5.WithAssociateMiddleware(finishHandshake),
		socks5.WithAssociateHandle(func(ctx context.Context, writer io.Writer, request *socks5.Request) error {
			return s.handleAssociate(ctx, writer, request, cfg.Network, associateResolver)
		}),
	}
	if cfg.Resolver != nil {
		opts = append(opts, socks5.WithResolver(cfg.Resolver))
	}
	if cfg.Credentials != nil {
		opts = append(opts, socks5.WithCredential(cfg.Credentials))
	}
	if cfg.Verbose {
		opts = append(opts, socks5.WithLogger(log))
	}
	server := socks5.NewServer(opts...)

	s.logger = log

	// Filesystem path → Unix socket, otherwise TCP
	isUnix := strings.HasPrefix(cfg.Addr, "/") || strings.HasPrefix(cfg.Addr, ".")
	var (
		ln  net.Listener
		err error
	)
	if isUnix {
		ln, err = listenUnix(cfg.Addr, defaultUnixSocketMode)
	} else {
		ln, err = net.Listen("tcp", cfg.Addr)
	}
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}
	s.listener = ln
	s.addr = cfg.Addr
	s.isUnix = isUnix

	s.listener = newLimitedListener(
		s.listener,
		s.newConnectionLimit(),
		effectiveDuration(cfg.HandshakeTimeout, defaultHandshakeTimeout),
		s.tunnelIdleTimeout,
	)

	s.logger.Infof("[socks] started on %s", cfg.Addr)

	serveLn := s.listener
	// Per-Enable WaitGroup: a fresh instance each session so a later Enable's
	// Add never races the previous Disable's Wait on a reused WaitGroup.
	wg := &sync.WaitGroup{}
	s.serveWG = wg
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.finishServe(serveLn, server.Serve(serveLn))
	}()

	return nil
}

func (s *Obj) Close() error {
	s.mu.Lock()
	if s.listener == nil {
		s.mu.Unlock()
		return nil
	}
	ln := s.listener
	wg := s.serveWG
	s.mu.Unlock()

	err := ln.Close()
	if wg != nil {
		wg.Wait()
	}
	return err
}

// Addr — listen address; empty if not started
func (s *Obj) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Obj) IsUnix() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isUnix
}

func (s *Obj) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener != nil
}

func (s *Obj) finishServe(ln net.Listener, err error) {
	limited, _ := ln.(*limitedListenerObj)
	_ = ln.Close()
	if limited != nil {
		limited.wait()
	}

	s.mu.Lock()
	if s.listener != ln {
		s.mu.Unlock()
		return
	}
	addr := s.addr
	isUnix := s.isUnix
	if err != nil && !errors.Is(err, net.ErrClosed) {
		if s.logger != nil {
			s.logger.Errorf("[socks] server stopped: %v", err)
		}
	}
	s.listener = nil
	s.addr = ""
	s.isUnix = false
	if limited != nil && s.limiter == limited.limit {
		s.limiter = nil
	}
	s.serveWG = nil
	logger := s.logger
	s.mu.Unlock()

	if isUnix {
		_ = os.Remove(addr)
	}
	if logger != nil {
		logger.Infof("[socks] stopped on %s", addr)
	}
}

// //

// limitedListenerObj — semaphore limiting simultaneous connections
type limitedListenerObj struct {
	net.Listener
	limit             *common.DynamicLimitObj
	done              chan struct{}
	closeOnce         sync.Once
	closeErr          error
	handshakeTimeout  time.Duration
	tunnelIdleTimeout time.Duration
	mu                sync.Mutex
	conns             map[*limitedConnObj]struct{}
	wg                sync.WaitGroup
}

func newLimitedListener(inner net.Listener, limit *common.DynamicLimitObj, handshakeTimeout time.Duration, tunnelIdleTimeout time.Duration) *limitedListenerObj {
	return &limitedListenerObj{
		Listener:          inner,
		limit:             limit,
		done:              make(chan struct{}),
		handshakeTimeout:  handshakeTimeout,
		tunnelIdleTimeout: tunnelIdleTimeout,
		conns:             make(map[*limitedConnObj]struct{}),
	}
}

func (l *limitedListenerObj) Accept() (net.Conn, error) {
	backoff := acceptRetryMinDelay
	for {
		if err := l.acquire(); err != nil {
			return nil, err
		}
		conn, err := l.Listener.Accept()
		if err != nil {
			l.release()
			if !retryableAcceptError(err) {
				return nil, err
			}
			timer := time.NewTimer(backoff)
			select {
			case <-l.done:
				if !timer.Stop() {
					<-timer.C
				}
				return nil, net.ErrClosed
			case <-timer.C:
			}
			if backoff < acceptRetryMaxDelay {
				backoff *= 2
				if backoff > acceptRetryMaxDelay {
					backoff = acceptRetryMaxDelay
				}
			}
			continue
		}
		lc := &limitedConnObj{Conn: conn, owner: l, tunnelIdleTimeout: l.tunnelIdleTimeout}
		if l.handshakeTimeout > 0 {
			// Full deadline bounds both handshake reads and server writes (slowloris-on-write).
			_ = lc.SetDeadline(time.Now().Add(l.handshakeTimeout))
		}
		if !l.track(lc) {
			_ = conn.Close()
			l.release()
			return nil, net.ErrClosed
		}
		return lc, nil
	}
}

func (l *limitedListenerObj) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
		l.closeErr = l.Listener.Close()
		l.closeActive()
	})
	return l.closeErr
}

func (l *limitedListenerObj) wait() {
	l.wg.Wait()
}

func (l *limitedListenerObj) acquire() error {
	for {
		select {
		case <-l.done:
			return net.ErrClosed
		default:
		}
		if l.limit == nil {
			return nil
		}
		acquired, ready := l.limit.AcquireOrReady()
		if acquired {
			return nil
		}
		select {
		case <-l.done:
			return net.ErrClosed
		case <-ready:
		}
	}
}

func (l *limitedListenerObj) release() {
	if l.limit != nil {
		l.limit.Release()
	}
}

func (l *limitedListenerObj) track(conn *limitedConnObj) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.done:
		return false
	default:
	}
	l.conns[conn] = struct{}{}
	l.wg.Add(1)
	return true
}

func (l *limitedListenerObj) untrack(conn *limitedConnObj) {
	l.mu.Lock()
	if _, ok := l.conns[conn]; ok {
		delete(l.conns, conn)
		l.wg.Done()
	}
	l.mu.Unlock()
}

func (l *limitedListenerObj) activeConns() []*limitedConnObj {
	l.mu.Lock()
	conns := make([]*limitedConnObj, 0, len(l.conns))
	for conn := range l.conns {
		conns = append(conns, conn)
	}
	l.mu.Unlock()
	return conns
}

func (l *limitedListenerObj) closeActive() {
	conns := l.activeConns()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

// limitedConnObj — connection that releases a semaphore slot on Close()
type limitedConnObj struct {
	net.Conn
	once              sync.Once
	owner             *limitedListenerObj
	tunnelIdleTimeout time.Duration
	tunnelDeadline    atomic.Int64
	tunnelStarted     atomic.Bool
}

func (c *limitedConnObj) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.tunnelStarted.Load() {
		c.refreshActivityDeadline()
	}
	return n, err
}

func (c *limitedConnObj) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 && c.tunnelStarted.Load() {
		c.refreshActivityDeadline()
	}
	return n, err
}

func (c *limitedConnObj) finishHandshake() {
	c.finishHandshakeState(true)
}

func (c *limitedConnObj) finishHandshakeWithoutTunnelIdle() {
	c.finishHandshakeState(false)
}

func (c *limitedConnObj) finishHandshakeState(trackTunnel bool) {
	// Clear the full handshake deadline (read + write) before entering tunnel mode
	// so the write deadline armed at Accept never leaks into a silent tunnel.
	_ = c.SetReadDeadline(time.Time{})
	_ = c.SetWriteDeadline(time.Time{})
	if trackTunnel {
		c.tunnelStarted.Store(true)
		c.refreshActivityDeadline()
	}
}

func (c *limitedConnObj) refreshActivityDeadline() {
	common.RefreshDeadline(time.Now(), c.tunnelIdleTimeout, &c.tunnelDeadline, c, false)
}

func (c *limitedConnObj) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.owner.untrack(c)
		c.owner.release()
	})
	return err
}
