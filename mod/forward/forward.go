// Package forward proxies TCP and UDP between host and Yggdrasil networks.
package forward

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// NetworkInterface is the five-method network contract forwarding consumes.
type NetworkInterface interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	Listen(string, string) (net.Listener, error)
	ListenPacket(string, string) (net.PacketConn, error)
	Address() net.IP
	MTU() uint64
}

// TCPMappingObj maps one TCP listener to one destination.
type TCPMappingObj struct {
	// Listen is the address on the source network.
	Listen *net.TCPAddr
	// Mapped is the destination on the other network.
	Mapped *net.TCPAddr
}

// UDPMappingObj maps one UDP listener to one destination.
type UDPMappingObj struct {
	// Listen is the address on the source network.
	Listen *net.UDPAddr
	// Mapped is the destination on the other network.
	Mapped *net.UDPAddr
}

// UDPLoopConfigObj contains all inputs for a UDP session loop.
type UDPLoopConfigObj struct {
	// Logger receives loop events. Nil discards them.
	Logger yggcore.Logger
	// ListenConn receives client datagrams.
	ListenConn net.PacketConn
	// Dial creates one upstream connection per source address.
	Dial func(context.Context, net.Addr) (net.Conn, error)
	// DialTimeout bounds Dial. Zero uses 10 seconds; negative disables it.
	DialTimeout time.Duration
	// WriteTimeout bounds reverse writes. Zero uses 5 seconds; negative disables it.
	WriteTimeout time.Duration
	// MaxPacketSize bounds UDP payload bytes.
	MaxPacketSize int
	// Timeout closes inactive sessions and must be positive.
	Timeout time.Duration
	// MaxSessions bounds active sessions. Zero is unlimited; negative is invalid.
	MaxSessions int
}

// UDPReverseConfigObj contains all inputs for a UDP reverse proxy worker.
type UDPReverseConfigObj struct {
	// Dst receives reverse datagrams.
	Dst net.PacketConn
	// DstAddr is the reverse datagram destination.
	DstAddr net.Addr
	// Src is the upstream connected socket.
	Src net.Conn
	// WriteTimeout bounds each destination write.
	WriteTimeout time.Duration
	// MaxPacketSize bounds upstream payload bytes.
	MaxPacketSize int
	// Activity records successful traffic when non-nil.
	Activity func()
}

// ConfigObj contains all forwarding dependencies and limits.
type ConfigObj struct {
	// Logger receives forwarding events.
	Logger yggcore.Logger

	// Node owns Yggdrasil Dial/Listen operations used by forwarding rules.
	Node NetworkInterface

	// LocalTCP forwards local TCP listeners to Yggdrasil destinations.
	LocalTCP []TCPMappingObj

	// RemoteTCP forwards Yggdrasil TCP listeners to local destinations.
	RemoteTCP []TCPMappingObj

	// LocalUDP forwards local UDP listeners to Yggdrasil destinations.
	LocalUDP []UDPMappingObj

	// RemoteUDP forwards Yggdrasil UDP listeners to local destinations.
	RemoteUDP []UDPMappingObj

	// UDPTimeout closes idle UDP sessions; must be positive.
	UDPTimeout time.Duration

	// Backend dial timeout; 0 -> safe default, <0 -> disabled.
	DialTimeout time.Duration

	// TCP proxy idle timeout; 0 -> safe default, <0 -> disabled.
	TCPIdleTimeout time.Duration

	// Active TCP proxy sessions across this object; 0 means unlimited, <0 is invalid.
	MaxTCPConnections int

	// Active UDP sessions across this object; 0 means unlimited, <0 is invalid.
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

// Obj owns an immutable set of active forwarding rules.
type Obj struct {
	log              yggcore.Logger
	node             NetworkInterface
	timeout          time.Duration
	dialTimeout      time.Duration
	tcpIdleTimeout   time.Duration
	udpMaxPacketSize int
	udpWriteTimeout  time.Duration
	tcpLimit         admissionLimitObj
	udpLimit         admissionLimitObj
	wg               sync.WaitGroup
	cancel           context.CancelFunc
	closeOnce        sync.Once
	stats            statsObj

	localTCPs  []TCPMappingObj
	remoteTCPs []TCPMappingObj
	localUDPs  []UDPMappingObj
	remoteUDPs []UDPMappingObj
}

type statsObj struct {
	sessionUDPDrops atomic.Uint64
	reverseUDPDrops atomic.Uint64
	terminalErrors  atomic.Uint64
}

// SnapshotObj is a lock-free point-in-time view of forwarding load and drops.
type SnapshotObj struct {
	// ActiveTCP is the current object-wide TCP session count.
	ActiveTCP int64
	// ActiveUDP is the current object-wide UDP session count.
	ActiveUDP int64
	// SessionUDPDrops counts packets rejected by session queues or churn.
	SessionUDPDrops uint64
	// ReverseUDPDrops counts reverse queue and write failures.
	ReverseUDPDrops uint64
	// TerminalErrors counts listener loops stopped by repeated terminal errors.
	TerminalErrors uint64
}

// Snapshot returns current counters without resetting them.
func (m *Obj) Snapshot() SnapshotObj {
	return SnapshotObj{
		ActiveTCP:       m.tcpLimit.active.Load(),
		ActiveUDP:       m.udpLimit.active.Load(),
		SessionUDPDrops: m.stats.sessionUDPDrops.Load(),
		ReverseUDPDrops: m.stats.reverseUDPDrops.Load(),
		TerminalErrors:  m.stats.terminalErrors.Load(),
	}
}

type intervalLogObj struct {
	next atomic.Int64
}

type admissionLimitObj struct {
	max    int64
	active atomic.Int64
}

func (l *admissionLimitObj) acquire() bool {
	if l.max == 0 {
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

func (l *admissionLimitObj) release() {
	l.active.Add(-1)
}

type ioErrorStreakObj struct {
	message string
	count   int
}

func (s *ioErrorStreakObj) terminal(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
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

func (m *Obj) effectiveUDPMaxPacketSize() int {
	if m.udpMaxPacketSize > 0 {
		return m.udpMaxPacketSize
	}
	return udpMaxPacketSizeFromMTU(m.node.MTU())
}

func (m *Obj) hasUDPMappings() bool {
	return len(m.localUDPs) > 0 || len(m.remoteUDPs) > 0
}

func effectiveUDPWriteTimeout(d time.Duration) time.Duration {
	if d == 0 {
		return DefaultUDPWriteTimeout
	}
	return d
}

// //

func (m *Obj) applyConfig(cfg ConfigObj) {
	m.log = common.NormalizeLogger(cfg.Logger)
	m.node = cfg.Node
	m.timeout = cfg.UDPTimeout
	m.dialTimeout = cfg.DialTimeout
	m.tcpIdleTimeout = effectiveTCPIdleTimeout(cfg.TCPIdleTimeout)
	m.tcpLimit.max = int64(cfg.MaxTCPConnections)
	m.udpLimit.max = int64(cfg.MaxUDPSessions)
	m.udpWriteTimeout = effectiveUDPWriteTimeout(cfg.UDPWriteTimeout)
	if cfg.UDPMaxPacketSize < 0 {
		m.udpMaxPacketSize = maxUDPDatagramSize
	} else if cfg.UDPMaxPacketSize > 0 {
		m.udpMaxPacketSize = clampUDPMaxPacketSize(cfg.UDPMaxPacketSize)
	}
}

func (m *Obj) validateMappings() error {
	for _, mapping := range m.localTCPs {
		if err := validateTCPMapping(mapping); err != nil {
			return err
		}
	}
	for _, mapping := range m.remoteTCPs {
		if err := validateTCPMapping(mapping); err != nil {
			return err
		}
	}
	for _, mapping := range m.localUDPs {
		if err := validateUDPMapping(mapping); err != nil {
			return err
		}
	}
	for _, mapping := range m.remoteUDPs {
		if err := validateUDPMapping(mapping); err != nil {
			return err
		}
	}
	return nil
}

func cloneTCPMappings(mappings []TCPMappingObj) []TCPMappingObj {
	if len(mappings) == 0 {
		return nil
	}
	cloned := make([]TCPMappingObj, len(mappings))
	for i, mapping := range mappings {
		cloned[i] = cloneTCPMapping(mapping)
	}
	return cloned
}

func cloneUDPMappings(mappings []UDPMappingObj) []UDPMappingObj {
	if len(mappings) == 0 {
		return nil
	}
	cloned := make([]UDPMappingObj, len(mappings))
	for i, mapping := range mappings {
		cloned[i] = cloneUDPMapping(mapping)
	}
	return cloned
}

func cloneTCPMapping(mapping TCPMappingObj) TCPMappingObj {
	return TCPMappingObj{Listen: cloneTCPAddr(mapping.Listen), Mapped: cloneTCPAddr(mapping.Mapped)}
}

func cloneUDPMapping(mapping UDPMappingObj) UDPMappingObj {
	return UDPMappingObj{Listen: cloneUDPAddr(mapping.Listen), Mapped: cloneUDPAddr(mapping.Mapped)}
}

func cloneTCPAddr(address *net.TCPAddr) *net.TCPAddr {
	if address == nil {
		return nil
	}
	cloned := *address
	cloned.IP = slices.Clone(address.IP)
	return &cloned
}

func cloneUDPAddr(address *net.UDPAddr) *net.UDPAddr {
	if address == nil {
		return nil
	}
	cloned := *address
	cloned.IP = slices.Clone(address.IP)
	return &cloned
}

// //

// New validates, binds, and starts an immutable forwarding rule set.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.Node == nil {
		return nil, ErrNodeRequired
	}
	if cfg.MaxTCPConnections < 0 || cfg.MaxUDPSessions < 0 {
		return nil, ErrInvalidLimit
	}
	m := &Obj{}
	m.applyConfig(cfg)
	m.localTCPs = cloneTCPMappings(cfg.LocalTCP)
	m.remoteTCPs = cloneTCPMappings(cfg.RemoteTCP)
	m.localUDPs = cloneUDPMappings(cfg.LocalUDP)
	m.remoteUDPs = cloneUDPMappings(cfg.RemoteUDP)
	if m.hasUDPMappings() && m.timeout <= 0 {
		return nil, ErrInvalidSessionTimeout
	}
	if err := m.validateMappings(); err != nil {
		return nil, err
	}

	var localTCP, remoteTCP []tcpStartObj
	var localUDP, remoteUDP []udpStartObj
	prepared := false
	defer func() {
		if prepared {
			return
		}
		closeTCPStarts(localTCP)
		closeTCPStarts(remoteTCP)
		closeUDPStarts(localUDP)
		closeUDPStarts(remoteUDP)
	}()

	var err error
	localTCP, err = m.prepareLocalTCP()
	if err != nil {
		return nil, err
	}
	remoteTCP, err = m.prepareRemoteTCP()
	if err != nil {
		return nil, err
	}
	localUDP, err = m.prepareLocalUDP()
	if err != nil {
		return nil, err
	}
	remoteUDP, err = m.prepareRemoteUDP()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	prepared = true

	m.runTCPStarts(runCtx, localTCP)
	m.runTCPStarts(runCtx, remoteTCP)
	m.runUDPStarts(runCtx, localUDP)
	m.runUDPStarts(runCtx, remoteUDP)
	return m, nil
}

// //

// Close cancels all forwarding work and waits for the standalone module to stop.
func (m *Obj) Close() error {
	m.closeOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
	})
	m.wg.Wait()
	return nil
}
