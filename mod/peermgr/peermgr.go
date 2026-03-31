package peermgr

import (
	"context"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
)

// // // // // // // // // //

const defaultProbeTimeout = 10 * time.Second

// WatchInterval controls how often watchPeers polls GetPeers
// to check active peer count against MinPeers.
var WatchInterval = 10 * time.Second

// MinPeersConfirmations is the number of consecutive ticks at or below
// MinPeers threshold required before triggering an unscheduled optimize.
var MinPeersConfirmations = 3

// //

// ConfigObj — peer manager parameters
type ConfigObj struct {
	// List of candidate URIs ("tls://host:port", "tcp://...", "quic://...", etc.)
	Peers []string

	// Peer connection wait timeout during probing. 0 → 10s.
	// Applied per batch separately. Ignored when MaxPerProto == -1.
	ProbeTimeout time.Duration

	// Auto re-evaluation interval. 0 → only at startup.
	RefreshInterval time.Duration

	// Number of best peers per protocol:
	//   0 or 1  — one best per protocol (default)
	//   N > 1    — top-N per protocol
	//  -1        — passive mode: add all without selection;
	//              when RefreshInterval > 0 the entire list is reconnected
	MaxPerProto int

	// Batch size during probing:
	//   0 or 1 — all candidates added in one batch (default)
	//   N >= 2  — sliding window: N candidates at a time, race to elimination
	BatchSize int

	// Minimum active peer count before triggering unscheduled re-evaluation.
	// When active Up peers drop to this value for MinPeersConfirmations
	// consecutive checks, optimize is triggered automatically.
	//   0     — disabled (default)
	//   N > 0 — threshold; must be < MaxPerProto (unless -1) and < len(Peers)
	// Ignored in passive mode (MaxPerProto == -1).
	MinPeers uint8

	// Logger; required
	Logger yggcore.Logger

	// OnNoReachablePeers is called after probing if no peer responded.
	// Called from the manager goroutine; must not block.
	OnNoReachablePeers func()
}

// // // // // // // // // //

// Obj — peer manager
type Obj struct {
	cfg        ConfigObj
	node       core.Interface
	peers      []peerEntryObj
	ctx        context.Context
	cancel     context.CancelFunc
	active     []string
	mu         sync.Mutex
	optimizeMu sync.Mutex
	wg         sync.WaitGroup
}

// New creates the manager; peers are not added until Start()
func New(node core.Interface, cfg ConfigObj) (*Obj, error) {
	if cfg.Logger == nil {
		return nil, ErrLoggerRequired
	}
	if cfg.MaxPerProto == 0 {
		cfg.MaxPerProto = 1
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = defaultProbeTimeout
	}

	peers, errs := ValidatePeers(cfg.Peers)
	for _, err := range errs {
		cfg.Logger.Warnf("[peermgr] %v", err)
	}
	if len(peers) == 0 {
		return nil, ErrNoPeers
	}

	if cfg.MinPeers > 0 {
		if cfg.MaxPerProto == -1 {
			cfg.Logger.Warnf("[peermgr] MinPeers ignored in passive mode (MaxPerProto == -1)")
			cfg.MinPeers = 0
		} else if cfg.MaxPerProto > 0 && cfg.MaxPerProto <= int(cfg.MinPeers) {
			return nil, ErrMinPeersTooHigh
		} else if len(peers) <= int(cfg.MinPeers) {
			return nil, ErrMinPeersTooMany
		}
	}

	return &Obj{cfg: cfg, node: node, peers: peers}, nil
}

// // // // // // // // // //

// Start launches the manager asynchronously; calling again without Stop is an error
func (m *Obj) Start() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.mu.Unlock()

	m.cfg.Logger.Infof("[peermgr] starting, %d candidates, MaxPerProto=%d, BatchSize=%d",
		len(m.peers), m.cfg.MaxPerProto, m.cfg.BatchSize)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx)
	}()
	return nil
}

// Stop cancels the context, removes managed peers; safe to call multiple times
func (m *Obj) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.ctx = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.wg.Wait()

	m.mu.Lock()
	active := m.active
	m.active = nil
	m.mu.Unlock()

	for _, uri := range active {
		_ = m.node.RemovePeer(uri)
	}
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

// Optimize — unscheduled re-evaluation; blocks until done
func (m *Obj) Optimize() error {
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx == nil {
		return ErrNotRunning
	}
	return m.optimizeLocked(ctx)
}
