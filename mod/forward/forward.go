package forward

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
)

// // // // // // // // // //

// ErrInvalidSessionTimeout is returned by Start() when the UDP session timeout is not positive.
var ErrInvalidSessionTimeout = errors.New("forward: session timeout must be > 0")

// ErrNodeRequired is returned by Start() when the forwarding network is missing.
var ErrNodeRequired = errors.New("forward: node is required")

// ErrInvalidMapping is returned by Start() when a forwarding mapping is incomplete.
var ErrInvalidMapping = errors.New("forward: invalid mapping")

// ErrAlreadyStarted is returned when a manager is started or changed after Start.
var ErrAlreadyStarted = errors.New("forward: manager already started")

// ErrClosed is returned when a closed manager is started or changed.
var ErrClosed = errors.New("forward: manager is closed")

// TCPMappingObj — TCP mapping: local address ↔ remote
type TCPMappingObj struct {
	Listen *net.TCPAddr
	Mapped *net.TCPAddr
}

// UDPMappingObj — UDP mapping: local address ↔ remote
type UDPMappingObj struct {
	Listen *net.UDPAddr
	Mapped *net.UDPAddr
}

// UDPLoopConfigObj contains all inputs for a UDP session loop.
type UDPLoopConfigObj struct {
	Logger        yggcore.Logger
	ListenConn    net.PacketConn
	Dial          func(context.Context, net.Addr) (net.Conn, error)
	DialTimeout   time.Duration
	WriteTimeout  time.Duration
	MaxPacketSize int
	Timeout       time.Duration
	MaxSessions   int
	stats         *statsObj
}

// UDPReverseConfigObj contains all inputs for a UDP reverse proxy worker.
type UDPReverseConfigObj struct {
	Dst           net.PacketConn
	DstAddr       net.Addr
	Src           net.Conn
	WriteTimeout  time.Duration
	MaxPacketSize int
	Activity      func()
}

// ConfigObj contains all forwarding dependencies and limits.
type ConfigObj struct {
	// Logger receives forwarding events.
	Logger yggcore.Logger

	// Node owns Yggdrasil Dial/Listen operations used by forwarding rules.
	Node core.NetworkInterface

	// UDPTimeout closes idle UDP sessions; must be positive.
	UDPTimeout time.Duration

	// Backend dial timeout; 0 -> safe default, <0 -> disabled.
	DialTimeout time.Duration

	// TCP proxy idle timeout; 0 -> safe default, <0 -> disabled.
	TCPIdleTimeout time.Duration

	// Active TCP proxy sessions per mapping; 0 -> safe default, <0 -> unlimited.
	MaxTCPConnections int

	// Active UDP sessions per mapping; 0 -> safe default, <0 -> unlimited.
	MaxUDPSessions int

	// UDPMaxPacketSize bounds UDP payload bytes per datagram; 0 -> node MTU, <0 -> max datagram size.
	UDPMaxPacketSize int

	// UDPWriteTimeout bounds a write from a reverse UDP writer; 0 -> safe default, <0 -> disabled.
	UDPWriteTimeout time.Duration
}

// //

const (
	// DefaultTCPCloseTimeout waits for the TCP peer after one side closes.
	DefaultTCPCloseTimeout = 30 * time.Second

	// DefaultDialTimeout bounds backend dials after accepting TCP or UDP traffic.
	DefaultDialTimeout = 10 * time.Second

	// DefaultMaxTCPConnections bounds active TCP sessions per mapping.
	DefaultMaxTCPConnections = 1024

	// DefaultMaxUDPSessions bounds active UDP sessions per mapping.
	DefaultMaxUDPSessions = 1024

	// DefaultTCPIdleTimeout bounds established TCP sessions with no traffic.
	DefaultTCPIdleTimeout = 5 * time.Minute

	// DefaultUDPWriteTimeout bounds reverse writes to a mapping listener.
	DefaultUDPWriteTimeout = 5 * time.Second

	maxUDPDatagramSize = 65535
	acceptBackoffMin   = 10 * time.Millisecond
	acceptBackoffMax   = time.Second
	limitLogInterval   = time.Second
	terminalErrorLimit = 8
)

// ManagerObj — forwarding rule manager: New → Add* → Start
type ManagerObj struct {
	log               yggcore.Logger
	node              core.NetworkInterface
	timeout           time.Duration
	dialTimeout       time.Duration
	tcpIdleTimeout    time.Duration
	maxTCPConnections int
	maxUDPSessions    int
	udpMaxPacketSize  int
	udpWriteTimeout   time.Duration
	wg                sync.WaitGroup
	stateMu           sync.Mutex
	started           bool
	closed            bool
	cancel            context.CancelFunc
	closeOnce         sync.Once
	stats             statsObj

	localTCPs  []TCPMappingObj
	remoteTCPs []TCPMappingObj
	localUDPs  []UDPMappingObj
	remoteUDPs []UDPMappingObj
}

type statsObj struct {
	activeTCP       atomic.Int64
	activeUDP       atomic.Int64
	reverseUDPDrops atomic.Int64
	terminalErrors  atomic.Int64
}

// SnapshotObj is a lock-free point-in-time view of forwarding load and drops.
type SnapshotObj struct {
	ActiveTCP       int64
	ActiveUDP       int64
	ReverseUDPDrops int64
	TerminalErrors  int64
}

// Snapshot returns current counters without resetting them.
func (m *ManagerObj) Snapshot() SnapshotObj {
	return SnapshotObj{
		ActiveTCP:       m.stats.activeTCP.Load(),
		ActiveUDP:       m.stats.activeUDP.Load(),
		ReverseUDPDrops: m.stats.reverseUDPDrops.Load(),
		TerminalErrors:  m.stats.terminalErrors.Load(),
	}
}

type intervalLogObj struct {
	next atomic.Int64
}

// ioErrorStreakObj prevents a broken Listener/PacketConn implementation from
// spinning forever on the same immediate error while still allowing transient
// failures to recover with backoff.
type ioErrorStreakObj struct {
	message string
	count   int
}

func (s *ioErrorStreakObj) terminal(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	// Resource exhaustion and aborted accepts are recoverable once pressure drops.
	// Keep the bounded backoff in the caller, but never permanently kill a mapping
	// merely because the kernel returned the same transient errno repeatedly.
	if retryableIOError(err) {
		s.reset()
		return false
	}
	message := err.Error()
	if message != s.message {
		s.message = message
		s.count = 1
		return false
	}
	s.count++
	return s.count >= terminalErrorLimit
}

func retryableIOError(err error) bool {
	return errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ENOBUFS) ||
		errors.Is(err, syscall.ECONNABORTED)
}

func (s *ioErrorStreakObj) reset() {
	s.message = ""
	s.count = 0
}

func (l *intervalLogObj) allow(interval time.Duration) bool {
	now := time.Now().UnixNano()
	next := l.next.Load()
	if now < next {
		return false
	}
	return l.next.CompareAndSwap(next, now+int64(interval))
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return acceptBackoffMin
	}
	d *= 2
	if d > acceptBackoffMax {
		return acceptBackoffMax
	}
	return d
}

func clampUDPMaxPacketSize(size int) int {
	if size <= 0 || size > maxUDPDatagramSize {
		return maxUDPDatagramSize
	}
	return size
}

func udpMaxPacketSizeFromMTU(mtu uint64) int {
	if mtu == 0 || mtu > maxUDPDatagramSize {
		return maxUDPDatagramSize
	}
	return int(mtu)
}

func udpReadBufferSize(maxPacketSize int) int {
	maxPacketSize = clampUDPMaxPacketSize(maxPacketSize)
	if maxPacketSize >= maxUDPDatagramSize {
		return maxUDPDatagramSize
	}
	return maxPacketSize + 1
}

func udpCleanupInterval(timeout time.Duration) time.Duration {
	interval := timeout / 4
	if interval < time.Millisecond {
		return time.Millisecond
	}
	return interval
}

func dialTimeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout < 0 {
		return ctx, func() {}
	}
	if timeout == 0 {
		timeout = DefaultDialTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func effectiveTCPIdleTimeout(d time.Duration) time.Duration {
	if d < 0 {
		return -1
	}
	if d == 0 {
		return DefaultTCPIdleTimeout
	}
	return d
}

func effectiveMaxConnections(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

func (m *ManagerObj) effectiveUDPMaxPacketSize() int {
	if m.udpMaxPacketSize > 0 {
		return m.udpMaxPacketSize
	}
	return udpMaxPacketSizeFromMTU(m.node.MTU())
}

func (m *ManagerObj) hasUDPMappings() bool {
	return len(m.localUDPs) > 0 || len(m.remoteUDPs) > 0
}

func effectiveUDPWriteTimeout(d time.Duration) time.Duration {
	if d == 0 {
		return DefaultUDPWriteTimeout
	}
	return d
}

// //

func (m *ManagerObj) newTCPLimit() *tcpLimitObj {
	return &tcpLimitObj{max: int64(m.maxTCPConnections)}
}

// applyConfig sets all immutable tunables once through the effective* helpers.
func (m *ManagerObj) applyConfig(cfg ConfigObj) {
	m.log = common.NormalizeLogger(cfg.Logger)
	m.node = cfg.Node
	m.timeout = cfg.UDPTimeout
	m.dialTimeout = cfg.DialTimeout
	m.tcpIdleTimeout = effectiveTCPIdleTimeout(cfg.TCPIdleTimeout)
	m.maxTCPConnections = effectiveMaxConnections(cfg.MaxTCPConnections, DefaultMaxTCPConnections)
	m.maxUDPSessions = effectiveMaxConnections(cfg.MaxUDPSessions, DefaultMaxUDPSessions)
	m.udpWriteTimeout = effectiveUDPWriteTimeout(cfg.UDPWriteTimeout)
	if cfg.UDPMaxPacketSize < 0 {
		m.udpMaxPacketSize = maxUDPDatagramSize
	} else if cfg.UDPMaxPacketSize > 0 {
		m.udpMaxPacketSize = clampUDPMaxPacketSize(cfg.UDPMaxPacketSize)
	}
}

// New creates a manager; UDPTimeout controls UDP inactivity before closing a session.
func New(cfg ConfigObj) *ManagerObj {
	m := &ManagerObj{}
	m.applyConfig(cfg)
	return m
}

// //

func (m *ManagerObj) mutable() error {
	if m.closed {
		return ErrClosed
	}
	if m.started {
		return ErrAlreadyStarted
	}
	return nil
}

func (m *ManagerObj) AddLocalTCP(mappings ...TCPMappingObj) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err := m.mutable(); err != nil {
		return err
	}
	m.localTCPs = append(m.localTCPs, mappings...)
	return nil
}

func (m *ManagerObj) AddRemoteTCP(mappings ...TCPMappingObj) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err := m.mutable(); err != nil {
		return err
	}
	m.remoteTCPs = append(m.remoteTCPs, mappings...)
	return nil
}

func (m *ManagerObj) AddLocalUDP(mappings ...UDPMappingObj) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err := m.mutable(); err != nil {
		return err
	}
	m.localUDPs = append(m.localUDPs, mappings...)
	return nil
}

func (m *ManagerObj) AddRemoteUDP(mappings ...UDPMappingObj) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err := m.mutable(); err != nil {
		return err
	}
	m.remoteUDPs = append(m.remoteUDPs, mappings...)
	return nil
}

// ClearLocal clears local mappings. Before Start()
func (m *ManagerObj) ClearLocal() error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err := m.mutable(); err != nil {
		return err
	}
	m.localTCPs = nil
	m.localUDPs = nil
	return nil
}

// ClearRemote clears remote mappings. Before Start()
func (m *ManagerObj) ClearRemote() error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err := m.mutable(); err != nil {
		return err
	}
	m.remoteTCPs = nil
	m.remoteUDPs = nil
	return nil
}

// //

// Start launches goroutines for all mappings. A manager is single-use: after the
// first call, mappings cannot be changed and another Start returns ErrAlreadyStarted.
func (m *ManagerObj) Start(ctx context.Context) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.started {
		return ErrAlreadyStarted
	}
	m.started = true
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	if m.hasUDPMappings() && m.timeout <= 0 {
		cancel()
		return ErrInvalidSessionTimeout
	}
	if m.node == nil {
		cancel()
		return ErrNodeRequired
	}

	localTCP, err := m.prepareLocalTCP()
	if err != nil {
		cancel()
		return err
	}
	remoteTCP, err := m.prepareRemoteTCP()
	if err != nil {
		cancel()
		closeTCPStarts(localTCP)
		return err
	}
	localUDP, err := m.prepareLocalUDP()
	if err != nil {
		cancel()
		closeTCPStarts(localTCP)
		closeTCPStarts(remoteTCP)
		return err
	}
	remoteUDP, err := m.prepareRemoteUDP()
	if err != nil {
		cancel()
		closeTCPStarts(localTCP)
		closeTCPStarts(remoteTCP)
		closeUDPStarts(localUDP)
		return err
	}

	m.runTCPStarts(runCtx, localTCP)
	m.runTCPStarts(runCtx, remoteTCP)
	m.runUDPStarts(runCtx, localUDP)
	m.runUDPStarts(runCtx, remoteUDP)
	return nil
}

// Wait blocks until all goroutines finish. It does not initiate shutdown.
func (m *ManagerObj) Wait() {
	m.wg.Wait()
}

// Close cancels all forwarding work and waits for the standalone module to stop.
func (m *ManagerObj) Close() error {
	m.closeOnce.Do(func() {
		m.stateMu.Lock()
		m.closed = true
		cancel := m.cancel
		m.stateMu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	m.Wait()
	return nil
}
