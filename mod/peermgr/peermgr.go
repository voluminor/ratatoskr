// Package peermgr maintains a bounded, latency-selected Yggdrasil peer set.
package peermgr

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/internal/common"
)

// // // // // // // // // //

const (
	defaultProbeTimeout          = 10 * time.Second
	defaultBatchSize             = 64
	maxBatchSize                 = 256
	maxProbeBackoff              = 10 * time.Minute
	defaultHealthInterval        = 10 * time.Second
	defaultMinPeersConfirmations = 3
	defaultReprobeInterval       = 30 * time.Minute
)

// //

// ConfigObj contains peer-manager dependencies, candidates, and scheduling.
type ConfigObj struct {
	// Node is the peer-capable node managed by this object.
	Node NodeInterface

	// Peers lists candidate Yggdrasil peer URIs.
	Peers []string

	// Peer connection wait timeout per optimize cycle. 0 → 10s; <0 is invalid.
	ProbeTimeout time.Duration

	// Scheduled re-evaluation interval. <=0 disables scheduled refresh; outage
	// recovery may still run independently.
	RefreshInterval time.Duration

	// Number of best peers per protocol:
	//   0 or 1 — one best per protocol (default)
	//   N > 1  — top-N per protocol
	MaxPerProto int

	// Maximum new candidate connections attempted in one optimize cycle:
	//   0 or 1 — default batch size
	//   N >= 2 — up to N candidates, capped internally
	//   N < 0   — invalid
	// Active peers are re-evaluated without consuming this connection budget.
	BatchSize int

	// Passive keeps every configured peer managed and skips latency selection.
	Passive bool

	// MinPeers triggers an early optimize after the active Up count stays at or
	// below this threshold for MinPeersConfirmations checks. Zero disables it.
	MinPeers int

	// HealthInterval controls outage and MinPeers checks. Zero uses 10 seconds;
	// a negative value disables health recovery.
	HealthInterval time.Duration

	// MinPeersConfirmations controls consecutive low-count checks; 0 uses 3;
	// a negative value is invalid.
	MinPeersConfirmations int

	// ReprobeInterval is the minimum delay before reconnecting a reachable peer
	// that lost selection. 0 uses 30 minutes; <0 disables the holdoff.
	ReprobeInterval time.Duration

	// NoReachablePeers receives a best-effort notification after an active
	// optimize finds no reachable peer. Sending never blocks; use a channel with
	// capacity one to coalesce notifications while the receiver is busy. The
	// caller must not close the channel before Close returns.
	NoReachablePeers chan<- struct{}

	// Logger; nil discards logs.
	Logger yggcore.Logger
}

// NodeInterface is the node contract required for peer selection.
type NodeInterface interface {
	AddPeer(uri string) error
	RemovePeer(uri string) error
	GetPeers() []yggcore.PeerInfo
}

// // // // // // // // // //

// Obj owns and maintains a selected peer set.
type Obj struct {
	cfg         ConfigObj
	peers       []PeerEntryObj
	probeState  map[string]probeStateObj
	tasks       *common.TaskGroupObj
	active      []string
	mu          sync.Mutex
	optimizeCh  chan struct{}
	probeCursor int
	closeMu     sync.Mutex
}

type probeStateObj struct {
	failures     int
	retryAfter   time.Time
	holdoffUntil time.Time
}

func effectiveProbeTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultProbeTimeout
	}
	return timeout
}

func effectiveReprobeInterval(interval time.Duration) time.Duration {
	if interval == 0 {
		return defaultReprobeInterval
	}
	return interval
}

func newObj(cfg ConfigObj) (*Obj, error) {
	if cfg.Node == nil {
		return nil, ErrNodeRequired
	}
	cfg.Logger = common.NormalizeLogger(cfg.Logger)
	if cfg.MaxPerProto < 0 {
		return nil, ErrInvalidMaxPerProto
	}
	if cfg.ProbeTimeout < 0 {
		return nil, ErrInvalidProbeTimeout
	}
	if cfg.BatchSize < 0 {
		return nil, ErrInvalidBatchSize
	}
	if cfg.MinPeers < 0 {
		return nil, ErrInvalidMinPeers
	}
	if cfg.MinPeersConfirmations < 0 {
		return nil, ErrInvalidMinPeersConfirmations
	}
	if cfg.MaxPerProto == 0 {
		cfg.MaxPerProto = 1
	}
	cfg.ProbeTimeout = effectiveProbeTimeout(cfg.ProbeTimeout)
	if cfg.HealthInterval == 0 {
		cfg.HealthInterval = defaultHealthInterval
	}
	if cfg.MinPeersConfirmations == 0 {
		cfg.MinPeersConfirmations = defaultMinPeersConfirmations
	}
	cfg.ReprobeInterval = effectiveReprobeInterval(cfg.ReprobeInterval)

	peers, errs := ValidatePeers(cfg.Peers)
	for _, err := range errs {
		cfg.Logger.Warnf("[peermgr] %v", err)
	}
	if len(peers) == 0 {
		return nil, ErrNoPeers
	}
	if cfg.Passive && cfg.MinPeers > 0 {
		cfg.Logger.Warnf("[peermgr] MinPeers ignored in passive mode")
		cfg.MinPeers = 0
	}
	if cfg.HealthInterval < 0 && cfg.MinPeers > 0 {
		cfg.Logger.Warnf("[peermgr] MinPeers ignored when health recovery is disabled")
		cfg.MinPeers = 0
	}
	if cfg.MinPeers >= maxSelectablePeers(peers, cfg.MaxPerProto) {
		return nil, ErrMinPeersTooHigh
	}

	mgr := &Obj{
		cfg:        cfg,
		peers:      peers,
		probeState: make(map[string]probeStateObj, len(peers)),
		tasks:      common.NewTaskGroup(context.Background()),
		optimizeCh: make(chan struct{}, 1),
	}
	return mgr, nil
}

// // // // // // // // // //

// New validates cfg and starts peer management asynchronously.
func New(cfg ConfigObj) (*Obj, error) {
	mgr, err := newObj(cfg)
	if err != nil {
		return nil, err
	}
	mgr.cfg.Logger.Infof("[peermgr] starting, %d candidates, MaxPerProto=%d, BatchSize=%d",
		len(mgr.peers), mgr.cfg.MaxPerProto, mgr.cfg.BatchSize)
	_ = mgr.tasks.Go(mgr.run)
	return mgr, nil
}

// Close stops the manager and removes owned peers. Failed removals remain owned
// for the next Close call.
func (m *Obj) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()

	m.tasks.Wait()

	m.mu.Lock()
	active := append([]string(nil), m.active...)
	m.mu.Unlock()

	remaining := make([]string, 0, len(active))
	var errs []error
	for _, uri := range active {
		if err := m.cfg.Node.RemovePeer(uri); err != nil {
			remaining = append(remaining, uri)
			errs = append(errs, fmt.Errorf("remove peer %s: %w", normalizePeerURI(uri), err))
		}
	}
	m.mu.Lock()
	m.active = remaining
	m.mu.Unlock()
	m.cfg.Logger.Infof("[peermgr] closed, removed %d peers, %d pending", len(active)-len(remaining), len(remaining))
	return errors.Join(errs...)
}

// Active returns a copy of the currently managed peer URIs.
func (m *Obj) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.active))
	copy(out, m.active)
	return out
}

// //

// Optimize runs one unscheduled selection cycle.
func (m *Obj) Optimize() error {
	err, ok := m.tasks.Do(m.optimizeLocked)
	if !ok {
		return ErrClosed
	}
	if errors.Is(err, context.Canceled) {
		return ErrClosed
	}
	return err
}

func maxSelectablePeers(peers []PeerEntryObj, maxPerProto int) int {
	perProtocol := make(map[string]int)
	for _, peer := range peers {
		perProtocol[peer.Scheme]++
	}
	total := 0
	for _, count := range perProtocol {
		if count > maxPerProto {
			count = maxPerProto
		}
		total += count
	}
	return total
}
