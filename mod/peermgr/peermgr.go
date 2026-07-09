package peermgr

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/core"
)

// // // // // // // // // //

const (
	defaultProbeTimeout          = 10 * time.Second
	defaultWatchInterval         = 10 * time.Second
	minRefreshInterval           = 100 * time.Millisecond
	defaultMinPeersConfirmations = 3
	defaultBatchSize             = 32
	maxBatchSize                 = 256
	maxProbeBackoff              = 10 * time.Minute
)

// //

// ConfigObj — peer manager parameters
type ConfigObj struct {
	// List of candidate URIs ("tls://host:port", "tcp://...", "quic://...", etc.)
	Peers []string

	// Peer connection wait timeout during probing. 0 → 10s.
	// Applied per batch separately. Ignored when MaxPerProto == -1.
	ProbeTimeout time.Duration

	// Auto re-evaluation interval. 0 → only at startup.
	// Positive values below the safety floor are clamped.
	RefreshInterval time.Duration

	// Number of best peers per protocol:
	//   0 or 1  — one best per protocol (default)
	//   N > 1    — top-N per protocol
	//  -1        — passive mode: add all without selection;
	//              when RefreshInterval > 0 missing configured peers are re-added
	MaxPerProto int

	// Batch size during probing:
	//   0 or 1 — bounded default batch size
	//   N >= 2  — sliding tournament: N new candidates per full probe window, capped internally
	BatchSize int

	// Minimum active peer count before triggering unscheduled re-evaluation.
	// When active Up peers drop to this value the manager re-optimizes off-schedule.
	//   0     — disabled (default)
	//   N > 0 — threshold
	// Ignored in passive mode (MaxPerProto == -1).
	MinPeers uint8

	// Logger; required
	Logger yggcore.Logger

	// OnNoReachablePeers is called after probing if no peer responded.
	// Dispatched fire-and-forget with panic recovery; Stop does not wait for it.
	// A single-flight guard prevents overlapping invocations.
	OnNoReachablePeers func()

	// OnActiveChange is fired when the managed active-peer set changes;
	// called non-blocking, must return quickly; copy the slice if retained.
	OnActiveChange func(active []string)
}

// // // // // // // // // //

// Obj — peer manager
type Obj struct {
	cfg            ConfigObj
	node           core.PeerInterface
	peers          []peerEntryObj
	probeState     map[string]probeStateObj
	ctx            context.Context
	cancel         context.CancelFunc
	active         []string
	runID          uint64
	mu             sync.Mutex
	activeChangeMu sync.Mutex
	optimizeCh     chan struct{}
	optimizing     atomic.Int64
	callbackActive atomic.Bool
	activeChangeOn bool
	activePending  []string
	stopMu         sync.Mutex
	wg             sync.WaitGroup
	watchInterval  time.Duration
	watchNeed      int
	stopping       bool
}

type probeStateObj struct {
	failures int
	nextTry  time.Time
}

func effectiveProbeTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultProbeTimeout
	}
	return timeout
}

func effectiveRefreshInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	if interval < minRefreshInterval {
		return minRefreshInterval
	}
	return interval
}

// New creates the manager; peers are not added until Start()
func New(node core.PeerInterface, cfg ConfigObj) (*Obj, error) {
	if node == nil {
		return nil, ErrNodeRequired
	}
	cfg.Logger = common.NormalizeLogger(cfg.Logger)
	if cfg.MaxPerProto < -1 {
		return nil, ErrInvalidMaxPerProto
	}
	if cfg.MaxPerProto == 0 {
		cfg.MaxPerProto = 1
	}
	cfg.ProbeTimeout = effectiveProbeTimeout(cfg.ProbeTimeout)
	cfg.RefreshInterval = effectiveRefreshInterval(cfg.RefreshInterval)

	peers, errs := ValidatePeers(cfg.Peers)
	for _, err := range errs {
		cfg.Logger.Warnf("[peermgr] %v", err)
	}
	if len(peers) == 0 {
		return nil, ErrNoPeers
	}

	// Passive mode adds all peers without selection, so a MinPeers threshold is
	// meaningless there; a nonsensical threshold otherwise is the caller's problem.
	if cfg.MinPeers > 0 && cfg.MaxPerProto == -1 {
		cfg.Logger.Warnf("[peermgr] MinPeers ignored in passive mode (MaxPerProto == -1)")
		cfg.MinPeers = 0
	}

	mgr := &Obj{
		cfg:           cfg,
		node:          node,
		peers:         peers,
		probeState:    make(map[string]probeStateObj, len(peers)),
		optimizeCh:    make(chan struct{}, 1),
		watchInterval: defaultWatchInterval,
		watchNeed:     defaultMinPeersConfirmations,
	}
	return mgr, nil
}

// // // // // // // // // //

// Start launches the manager asynchronously; calling again without Stop is an error
func (m *Obj) Start() error {
	m.mu.Lock()
	if m.cancel != nil || m.stopping {
		m.mu.Unlock()
		return ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runID++
	m.ctx = ctx
	m.cancel = cancel
	m.wg.Add(1)
	m.mu.Unlock()

	m.cfg.Logger.Infof("[peermgr] starting, %d candidates, MaxPerProto=%d, BatchSize=%d",
		len(m.peers), m.cfg.MaxPerProto, m.cfg.BatchSize)

	go func() {
		defer m.wg.Done()
		m.run(ctx)
	}()
	return nil
}

// Stop cancels the context, removes managed peers; safe to call multiple times
func (m *Obj) Stop() {
	m.stopMu.Lock()
	defer m.stopMu.Unlock()

	m.mu.Lock()
	cancel := m.cancel
	runID := m.runID
	if cancel != nil {
		m.stopping = true
		m.cancel = nil
		m.ctx = nil
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Only the manager goroutine is awaited; user feedback callbacks are
	// fire-and-forget so shutdown latency never depends on arbitrary user code
	// and a callback that re-enters Stop/Optimize cannot deadlock.
	m.wg.Wait()

	m.mu.Lock()
	active := append([]string(nil), m.active...)
	m.active = nil
	m.mu.Unlock()

	for _, uri := range active {
		_ = m.node.RemovePeer(uri)
	}
	m.mu.Lock()
	if m.runID == runID {
		m.stopping = false
	}
	m.mu.Unlock()
	m.cfg.Logger.Infof("[peermgr] stopped, removed %d peers", len(active))
}

// Active — copy of the current active peer list
func (m *Obj) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.active))
	copy(out, m.active)
	return out
}

// //

// Optimize — unscheduled re-evaluation; blocks until done
func (m *Obj) Optimize() error {
	m.mu.Lock()
	ctx := m.ctx
	stopping := m.stopping
	if ctx != nil && !stopping {
		m.wg.Add(1)
	}
	m.mu.Unlock()
	if ctx == nil || stopping {
		return ErrNotRunning
	}
	defer m.wg.Done()
	err := m.optimizeLocked(ctx)
	if errors.Is(err, context.Canceled) {
		m.mu.Lock()
		stopped := m.ctx == nil || m.stopping
		m.mu.Unlock()
		if stopped {
			return ErrNotRunning
		}
	}
	return err
}
