package ratatoskr

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils/sigil_core"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/resolver"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

// Obj — Yggdrasil node for embedding in applications.
// Combines core (DialContext/Listen), resolver (.pk.ygg), and SOCKS5.
// The primary networking and peer methods are exposed directly; the full node
// contract (multicast, admin, retry, stats) is reachable via Core().
type Obj struct {
	// core is assigned once in New and read-only afterwards; use Close() to stop.
	core core.Interface
	// socks is assigned once in New and read-only afterwards; safe to read lock-free.
	socks         *socks.Obj
	peerManager   *peermgr.Obj
	nodeInfo      *ninfo.Obj
	logger        yggcore.Logger
	closeTimeout  time.Duration
	done          chan struct{}
	closeOnce     sync.Once
	closeErr      error
	closeTimedOut atomic.Bool
}

const defaultCloseTimeout = 10 * time.Second

func effectiveCloseTimeout(timeout time.Duration) time.Duration {
	if timeout == 0 {
		return defaultCloseTimeout
	}
	return timeout
}

// cloneCallerConfig insulates New from the caller's config: sigils add top-level
// keys to NodeInfo and MulticastInterfaces is read after New (EnableMulticast), so
// both reference fields are copied. NodeInfo is cloned recursively because its
// nested maps and slices remain mutable after New. Other NodeConfig fields are
// consumed once at construction, so a shallow copy of the rest is sufficient.
func cloneCallerConfig(cfg *config.NodeConfig) (*config.NodeConfig, error) {
	if cfg == nil {
		return nil, nil
	}
	cloned := *cfg
	var err error
	cloned.NodeInfo, err = common.CloneNodeInfo(cfg.NodeInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidNodeInfo, err)
	}
	cloned.MulticastInterfaces = slices.Clone(cfg.MulticastInterfaces)
	return &cloned, nil
}

// New creates and starts the node.
// If cfg.Peers is set, starts the peer manager; cfg.Config.Peers must be empty.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.CloseTimeout < 0 {
		return nil, ErrInvalidCloseTimeout
	}
	if cfg.Ctx != nil {
		if err := cfg.Ctx.Err(); err != nil {
			return nil, err
		}
	}
	cfg.Logger = common.NormalizeLogger(cfg.Logger)

	if cfg.Peers != nil && cfg.Config != nil && len(cfg.Config.Peers) > 0 {
		return nil, ErrPeersConflict
	}

	if cfg.Config == nil {
		cfg.Config = config.GenerateConfig()
		cfg.Config.AdminListen = "none"
	} else {
		var err error
		cfg.Config, err = cloneCallerConfig(cfg.Config)
		if err != nil {
			return nil, err
		}
	}

	// Assemble NodeInfo from sigils
	var sigilsObj *sigil_core.Obj
	if cfg.Sigils != nil {
		var errs []error
		sigilsObj, errs = sigil_core.New(cfg.Config.NodeInfo, cfg.Sigils...)
		for _, e := range errs {
			cfg.Logger.Warnf("[ratatoskr] sigil: %v", e)
		}
		cfg.Config.NodeInfo = sigilsObj.NodeInfo()
	}

	coreNode, err := core.New(core.ConfigObj{
		Config:       cfg.Config,
		Logger:       cfg.Logger,
		RSTQueueSize: cfg.RSTQueueSize,
	})
	if err != nil {
		return nil, err
	}

	// ninfo — always created for Ask/AskAddr
	ni, err := ninfo.New(ninfo.ConfigObj{Source: coreNode})
	if err != nil {
		_ = coreNode.Close()
		return nil, fmt.Errorf("ninfo: %w", err)
	}
	if sigilsObj != nil {
		if err := ni.ImportSigils(sigilsObj); err != nil {
			cfg.Logger.Warnf("[ratatoskr] parse sigil: %v", err)
		}
	}

	obj := &Obj{
		core:         coreNode,
		socks:        socks.NewDisabled(),
		nodeInfo:     ni,
		logger:       cfg.Logger,
		closeTimeout: effectiveCloseTimeout(cfg.CloseTimeout),
		done:         make(chan struct{}),
	}

	if cfg.Peers != nil {
		pCfg := *cfg.Peers
		if pCfg.Logger == nil {
			pCfg.Logger = cfg.Logger
		}
		mgr, err := peermgr.New(coreNode, pCfg)
		if err != nil {
			_ = ni.Close()
			_ = coreNode.Close()
			return nil, fmt.Errorf("peer manager: %w", err)
		}
		if err := mgr.Start(); err != nil {
			_ = ni.Close()
			_ = mgr.Stop()
			_ = coreNode.Close()
			return nil, fmt.Errorf("peer manager: %w", err)
		}
		obj.peerManager = mgr
	}

	// Auto-shutdown on context cancellation
	if cfg.Ctx != nil {
		go func() {
			select {
			case <-cfg.Ctx.Done():
				if err := obj.Close(); err != nil {
					obj.logger.Errorf("[ratatoskr] close after context cancellation: %v", err)
				}
			case <-obj.done:
			}
		}()
		// A cfg.Ctx cancelled during construction has already armed the watchdog
		// above; surface the error instead of returning a live-looking node that is
		// concurrently closing. Close is idempotent, so racing the watchdog is safe.
		if err := cfg.Ctx.Err(); err != nil {
			_ = obj.Close()
			return nil, err
		}
	}

	return obj, nil
}

// // // // // // // // // //

// Core exposes the full underlying node contract (multicast, admin, retry peers,
// stats). The primary methods below are promoted directly, so the embeddable
// surface stays small and advanced controls live behind one accessor.
func (o *Obj) Core() core.Interface { return o.core }

// DialContext opens a connection to a Yggdrasil address; compatible with http.Transport.DialContext.
func (o *Obj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return o.core.DialContext(ctx, network, address)
}

// Listen creates a TCP listener; closed automatically on Close().
func (o *Obj) Listen(network, address string) (net.Listener, error) {
	return o.core.Listen(network, address)
}

// ListenPacket creates a UDP listener; closed automatically on Close().
func (o *Obj) ListenPacket(network, address string) (net.PacketConn, error) {
	return o.core.ListenPacket(network, address)
}

// Address — node IPv6 address in the 200::/7 range.
func (o *Obj) Address() net.IP { return o.core.Address() }

// Subnet — routable /64 subnet of the node in the 300::/7 range.
func (o *Obj) Subnet() net.IPNet { return o.core.Subnet() }

// PublicKey — ed25519 public key of the node.
func (o *Obj) PublicKey() ed25519.PublicKey { return o.core.PublicKey() }

// MTU — MTU of the node interface.
func (o *Obj) MTU() uint64 { return o.core.MTU() }

// AddPeer adds a peer; URI: "tcp://...", "quic://...", etc.
func (o *Obj) AddPeer(uri string) error { return o.core.AddPeer(uri) }

// RemovePeer removes a previously added peer.
func (o *Obj) RemovePeer(uri string) error { return o.core.RemovePeer(uri) }

// GetPeers returns all peers (connected and configured).
func (o *Obj) GetPeers() []yggcore.PeerInfo { return o.core.GetPeers() }

// //

func (o *Obj) isClosed() bool {
	select {
	case <-o.done:
		return true
	default:
		return false
	}
}

// //

// EnableSOCKS starts the SOCKS5 proxy with the given parameters.
// Resolver is created automatically based on cfg.Nameserver
func (o *Obj) EnableSOCKS(cfg SOCKSConfigObj) error {
	if o.isClosed() {
		return ErrClosed
	}
	server := o.socks
	network := o.core
	logger := o.logger

	if server.IsEnabled() {
		return fmt.Errorf("%w on %s", socks.ErrAlreadyEnabled, server.Addr())
	}
	resolverCfg := resolver.ConfigObj{
		Dialer:          network,
		Nameserver:      cfg.Nameserver,
		LookupTimeout:   cfg.NameserverLookupTimeout,
		CacheTTL:        cfg.NameserverCacheTTL,
		CacheMaxEntries: cfg.NameserverCacheMaxEntries,
	}
	nameResolver := resolver.New(resolverCfg)
	err := server.Start(socks.ConfigObj{
		Network:                       network,
		Addr:                          cfg.Addr,
		Resolver:                      nameResolver,
		OwnResolver:                   true,
		Verbose:                       cfg.Verbose,
		Logger:                        logger,
		MaxConnections:                cfg.MaxConnections,
		HandshakeTimeout:              cfg.HandshakeTimeout,
		DialTimeout:                   cfg.DialTimeout,
		TunnelIdleTimeout:             cfg.TunnelIdleTimeout,
		MaxAssociateTargetsPerSession: cfg.MaxAssociateTargetsPerSession,
		Credentials:                   cfg.Credentials,
	})
	if err != nil {
		return errors.Join(err, nameResolver.Close())
	}
	// Close may have run its single SOCKS teardown before Start bound the listener.
	// The closed signal precedes teardown, so a closed node here means we must
	// tear the late listener down ourselves; surface any close error.
	if o.isClosed() {
		return errors.Join(ErrClosed, server.Close())
	}
	return nil
}

func (o *Obj) DisableSOCKS() error {
	if o.isClosed() {
		return ErrClosed
	}
	return o.socks.Close()
}

// SetSOCKSMaxConnections adjusts the SOCKS5 connection limit at runtime.
func (o *Obj) SetSOCKSMaxConnections(n int) {
	if o.isClosed() {
		return
	}
	o.socks.SetMaxConnections(n)
}

// SOCKSMaxConnections reports the current SOCKS5 connection limit.
func (o *Obj) SOCKSMaxConnections() int {
	return o.socks.MaxConnections()
}

// //

// PeerManagerActive returns the current list of active peers; nil if the manager is not used
func (o *Obj) PeerManagerActive() []string {
	if o.peerManager == nil {
		return nil
	}
	return o.peerManager.Active()
}

// PeerManagerOptimize triggers an unscheduled peer re-evaluation
func (o *Obj) PeerManagerOptimize() error {
	if o.peerManager == nil {
		return ErrPeerManagerNotEnabled
	}
	return o.peerManager.Optimize()
}

// Ask queries a remote node's NodeInfo by public key.
// Returns parsed metadata, build info, and measured RTT
func (o *Obj) Ask(ctx context.Context, key ed25519.PublicKey) (*ninfo.AskResultObj, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	result, err := o.nodeInfo.Ask(ctx, key)
	if errors.Is(err, ninfo.ErrClosed) {
		return nil, ErrClosed
	}
	return result, err
}

// AskAddr queries a remote node's NodeInfo by address string.
// Supported formats: "<hex>.pk.ygg", "[ip6]:port", "ip6", raw 64-char hex
func (o *Obj) AskAddr(ctx context.Context, addr string) (*ninfo.AskResultObj, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	result, err := o.nodeInfo.AskAddr(ctx, addr)
	if errors.Is(err, ninfo.ErrClosed) {
		return nil, ErrClosed
	}
	return result, err
}

// //

type closeResultObj struct {
	name string
	err  error
}

// Close stops all components; safe to call multiple times. The total wait is
// bounded by ConfigObj.CloseTimeout. A component that does not return before the
// deadline continues best-effort in a detached goroutine and cannot hold the
// application's shutdown path.
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		// Raise the closed signal before teardown. EnableSOCKS synchronizes with
		// SOCKS through the socks-layer mutex, so a listener bound concurrently
		// with Close is guaranteed to be observed and torn down (no leak).
		close(o.done)

		dependents := []struct {
			name string
			fn   func() error
		}{
			{name: "ninfo", fn: o.nodeInfo.Close},
			{name: "socks", fn: o.socks.Close},
		}
		if o.peerManager != nil {
			dependents = append(dependents, struct {
				name string
				fn   func() error
			}{name: "peermgr", fn: o.peerManager.Stop})
		}

		results := make(chan closeResultObj, len(dependents))
		for _, closer := range dependents {
			go func() {
				results <- closeResultObj{name: closer.name, err: closer.fn()}
			}()
		}

		timer := time.NewTimer(effectiveCloseTimeout(o.closeTimeout))
		defer timer.Stop()
		errs := make([]error, 0, len(dependents)+2)
		startCore := func() <-chan closeResultObj {
			result := make(chan closeResultObj, 1)
			go func() {
				result <- closeResultObj{name: "core", err: o.core.Close()}
			}()
			return result
		}
		timeout := func() {
			o.closeTimedOut.Store(true)
			errs = append(errs, fmt.Errorf("%w after %s", ErrCloseTimedOut, effectiveCloseTimeout(o.closeTimeout)))
			o.closeErr = errors.Join(errs...)
		}

		for range dependents {
			select {
			case result := <-results:
				if result.err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", result.name, result.err))
				}
			case <-timer.C:
				// The graceful dependency order exhausted its budget. Still attempt the
				// upstream teardown so a broken dependent cannot prevent best-effort
				// resource release after Close returns.
				_ = startCore()
				timeout()
				return
			}
		}

		select {
		case result := <-startCore():
			if result.err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", result.name, result.err))
			}
			o.closeErr = errors.Join(errs...)
		case <-timer.C:
			timeout()
		}
	})
	return o.closeErr
}
