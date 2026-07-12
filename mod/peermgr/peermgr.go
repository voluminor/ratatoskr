package peermgr

import (
	"context"
	"errors"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/internal/common"
)

// // // // // // // // // //

const (
	defaultProbeTimeout = 10 * time.Second
	defaultBatchSize    = 64
	maxBatchSize        = 256
	maxProbeBackoff     = 10 * time.Minute
)

// //

// ConfigObj — peer manager parameters
type ConfigObj struct {
	// List of candidate URIs ("tls://host:port", "tcp://...", "quic://...", etc.)
	Peers []string

	// Peer connection wait timeout per probe window. 0 → 10s.
	ProbeTimeout time.Duration

	// Auto re-evaluation interval. 0 → only at startup; positive → used as-is.
	RefreshInterval time.Duration

	// Number of best peers per protocol:
	//   0 or 1 — one best per protocol (default)
	//   N > 1  — top-N per protocol
	MaxPerProto int

	// Maximum peers probed simultaneously per window:
	//   0 or 1 — default window size
	//   N >= 2 — up to N concurrent probes, capped internally
	// A peer list that fits one window is evaluated in a single ProbeTimeout.
	BatchSize int

	// Logger; required
	Logger yggcore.Logger
}

// NodeInterface is the minimal node contract peermgr needs: it manages a curated
// peer set and reads peer state, nothing more. Any node implementation can be
// supplied without pulling in the whole core surface.
type NodeInterface interface {
	AddPeer(uri string) error
	RemovePeer(uri string) error
	GetPeers() []yggcore.PeerInfo
}

// // // // // // // // // //

// Obj — peer manager
type Obj struct {
	cfg        ConfigObj
	node       NodeInterface
	peers      []peerEntryObj
	probeState map[string]probeStateObj
	ctx        context.Context
	cancel     context.CancelFunc
	active     []string
	mu         sync.Mutex
	optimizeCh chan struct{}
	stopMu     sync.Mutex
	wg         sync.WaitGroup
	stopping   bool
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
	return interval
}

// New creates the manager; peers are not added until Start()
func New(node NodeInterface, cfg ConfigObj) (*Obj, error) {
	if node == nil {
		return nil, ErrNodeRequired
	}
	cfg.Logger = common.NormalizeLogger(cfg.Logger)
	if cfg.MaxPerProto < 0 {
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

	mgr := &Obj{
		cfg:        cfg,
		node:       node,
		peers:      peers,
		probeState: make(map[string]probeStateObj, len(peers)),
		optimizeCh: make(chan struct{}, 1),
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
	if cancel != nil {
		m.stopping = true
		m.cancel = nil
		m.ctx = nil
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.wg.Wait()

	m.mu.Lock()
	active := append([]string(nil), m.active...)
	m.active = nil
	m.mu.Unlock()

	for _, uri := range active {
		_ = m.node.RemovePeer(uri)
	}
	// stopping stays set through teardown so a concurrent Start waits for a clean
	// stop; clear it only once every managed peer has been removed.
	m.mu.Lock()
	m.stopping = false
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
