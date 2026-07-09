package forward

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
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
	Logger               yggcore.Logger
	ListenConn           net.PacketConn
	Dial                 func(context.Context, net.Addr) (net.Conn, error)
	DialTimeout          time.Duration
	MaxPacketSize        int
	Timeout              time.Duration
	MaxSessions          int
	MaxSessionsPerSource int
	// activeCounter tracks manager-wide active UDP sessions; nil for standalone loops.
	activeCounter *atomic.Int64
}

// UDPReverseConfigObj contains all inputs for a UDP reverse proxy worker.
type UDPReverseConfigObj struct {
	Dst           net.PacketConn
	DstAddr       net.Addr
	Src           net.Conn
	MaxPacketSize int
	Activity      func()
	writer        *packetWriterObj
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

	// Active UDP sessions per source IP; 0 -> safe default, <0 -> disabled.
	MaxUDPSessionsPerSource int

	// UDPMaxPacketSize bounds UDP payload bytes per datagram; 0 -> node MTU, <0 -> max datagram size.
	UDPMaxPacketSize int
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

	// DefaultMaxUDPSessionsPerSource bounds active UDP sessions per source IP.
	DefaultMaxUDPSessionsPerSource = 64

	// DefaultTCPIdleTimeout bounds established TCP sessions with no traffic.
	DefaultTCPIdleTimeout = 5 * time.Minute

	maxUDPDatagramSize = 65535
	acceptBackoffMin   = 10 * time.Millisecond
	acceptBackoffMax   = time.Second
	limitLogInterval   = time.Second
)

// ManagerObj — forwarding rule manager: New → Add* → Start
type ManagerObj struct {
	log                     yggcore.Logger
	node                    core.NetworkInterface
	timeout                 time.Duration
	dialTimeout             time.Duration
	tcpIdleTimeout          time.Duration
	maxTCPConnections       int
	maxUDPSessions          int
	maxUDPSessionsPerSource int
	udpMaxPacketSize        int
	activeUDPSessions       atomic.Int64
	wg                      sync.WaitGroup
	tcpLimiters             sync.Map

	localTCPs  []TCPMappingObj
	remoteTCPs []TCPMappingObj
	localUDPs  []UDPMappingObj
	remoteUDPs []UDPMappingObj
}

type intervalLogObj struct {
	next atomic.Int64
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

func effectiveDialTimeout(d time.Duration) time.Duration {
	if d == 0 {
		return DefaultDialTimeout
	}
	return d
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

func effectiveSourceLimit(n int) int {
	if n < 0 {
		return 0
	}
	if n == 0 {
		return DefaultMaxUDPSessionsPerSource
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

// //

// ActiveTCPConnections sums live TCP sessions across all per-mapping limiters.
func (m *ManagerObj) ActiveTCPConnections() int {
	total := 0
	m.tcpLimiters.Range(func(k, _ any) bool {
		total += k.(*tcpLimitObj).activeCount()
		return true
	})
	return total
}

// ActiveUDPSessions returns the manager-wide count of live UDP sessions.
func (m *ManagerObj) ActiveUDPSessions() int {
	return int(m.activeUDPSessions.Load())
}

func (m *ManagerObj) newTCPLimit() *tcpLimitObj {
	limiter := &tcpLimitObj{max: int64(m.maxTCPConnections)}
	m.tcpLimiters.Store(limiter, struct{}{})
	return limiter
}

// applyConfig sets all immutable tunables once through the effective* helpers.
func (m *ManagerObj) applyConfig(cfg ConfigObj) {
	m.log = common.NormalizeLogger(cfg.Logger)
	m.node = cfg.Node
	m.timeout = cfg.UDPTimeout
	m.dialTimeout = effectiveDialTimeout(cfg.DialTimeout)
	m.tcpIdleTimeout = effectiveTCPIdleTimeout(cfg.TCPIdleTimeout)
	m.maxTCPConnections = effectiveMaxConnections(cfg.MaxTCPConnections, DefaultMaxTCPConnections)
	m.maxUDPSessions = effectiveMaxConnections(cfg.MaxUDPSessions, DefaultMaxUDPSessions)
	m.maxUDPSessionsPerSource = effectiveSourceLimit(cfg.MaxUDPSessionsPerSource)
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

func (m *ManagerObj) AddLocalTCP(mappings ...TCPMappingObj) {
	m.localTCPs = append(m.localTCPs, mappings...)
}

func (m *ManagerObj) AddRemoteTCP(mappings ...TCPMappingObj) {
	m.remoteTCPs = append(m.remoteTCPs, mappings...)
}

func (m *ManagerObj) AddLocalUDP(mappings ...UDPMappingObj) {
	m.localUDPs = append(m.localUDPs, mappings...)
}

func (m *ManagerObj) AddRemoteUDP(mappings ...UDPMappingObj) {
	m.remoteUDPs = append(m.remoteUDPs, mappings...)
}

// ClearLocal clears local mappings. Before Start()
func (m *ManagerObj) ClearLocal() {
	m.localTCPs = nil
	m.localUDPs = nil
}

// ClearRemote clears remote mappings. Before Start()
func (m *ManagerObj) ClearRemote() {
	m.remoteTCPs = nil
	m.remoteUDPs = nil
}

// //

// Start launches goroutines for all mappings; called once
func (m *ManagerObj) Start(ctx context.Context) error {
	if m.hasUDPMappings() && m.timeout <= 0 {
		return ErrInvalidSessionTimeout
	}
	if m.node == nil {
		return ErrNodeRequired
	}

	localTCP, err := m.prepareLocalTCP()
	if err != nil {
		return err
	}
	remoteTCP, err := m.prepareRemoteTCP()
	if err != nil {
		closeTCPStarts(localTCP)
		return err
	}
	localUDP, err := m.prepareLocalUDP()
	if err != nil {
		closeTCPStarts(localTCP)
		closeTCPStarts(remoteTCP)
		return err
	}
	remoteUDP, err := m.prepareRemoteUDP()
	if err != nil {
		closeTCPStarts(localTCP)
		closeTCPStarts(remoteTCP)
		closeUDPStarts(localUDP)
		return err
	}

	m.runTCPStarts(ctx, localTCP)
	m.runTCPStarts(ctx, remoteTCP)
	m.runUDPStarts(ctx, localUDP)
	m.runUDPStarts(ctx, remoteUDP)
	return nil
}

// Wait blocks until all goroutines finish
func (m *ManagerObj) Wait() {
	m.wg.Wait()
}
