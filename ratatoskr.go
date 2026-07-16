// Package ratatoskr composes embeddable Yggdrasil networking modules.
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
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/sigil_core"
	"github.com/voluminor/ratatoskr/target"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/resolver"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

// Obj is an embeddable Yggdrasil node with NodeInfo and optional peer-manager
// and SOCKS services.
type Obj struct {
	core          core.Interface
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

func rollbackNewError(timeout time.Duration, cause error, before []common.NamedCloseObj, final common.NamedCloseObj) error {
	closeErr, timedOut := common.CloseWithDeadline(effectiveCloseTimeout(timeout), before, final)
	if timedOut {
		closeErr = errors.Join(closeErr, fmt.Errorf("%w during New rollback after %s", ErrCloseTimedOut, effectiveCloseTimeout(timeout)))
	}
	return errors.Join(cause, closeErr)
}

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

// New creates and starts a node. When cfg.Peers is set, cfg.Config.Peers must be
// empty.
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

	var customParsers []sigils.Interface
	if cfg.Sigils != nil {
		sigilsObj, sigilErrs := sigil_core.New(cfg.Config.NodeInfo, cfg.Sigils...)
		if err := errors.Join(sigilErrs...); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidSigils, err)
		}
		cfg.Config.NodeInfo = sigilsObj.NodeInfo()
		for _, parser := range cfg.Sigils {
			if parser == nil {
				continue
			}
			if _, builtIn := target.Parse(parser.GetName()); !builtIn {
				customParsers = append(customParsers, parser)
			}
		}
	}

	coreNode, err := core.New(core.ConfigObj{
		Config: cfg.Config,
		Logger: cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	niCfg := ninfo.ConfigObj{}
	if cfg.NodeInfo != nil {
		niCfg = *cfg.NodeInfo
	}
	niCfg.Source = coreNode
	niCfg.Sigils = append(slices.Clone(niCfg.Sigils), customParsers...)
	ni, err := ninfo.New(niCfg)
	if err != nil {
		cause := fmt.Errorf("ninfo: %w", err)
		if errors.Is(err, ninfo.ErrInvalidSigil) {
			cause = fmt.Errorf("%w: %w", ErrInvalidSigils, cause)
		}
		return nil, rollbackNewError(cfg.CloseTimeout, cause, nil, common.NamedCloseObj{Name: "core", Close: coreNode.Close})
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
		pCfg.Node = coreNode
		if pCfg.Logger == nil {
			pCfg.Logger = cfg.Logger
		}
		mgr, err := peermgr.New(pCfg)
		if err != nil {
			cause := fmt.Errorf("peer manager: %w", err)
			return nil, rollbackNewError(cfg.CloseTimeout, cause,
				[]common.NamedCloseObj{{Name: "ninfo", Close: ni.Close}},
				common.NamedCloseObj{Name: "core", Close: coreNode.Close})
		}
		obj.peerManager = mgr
	}

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
		if err := cfg.Ctx.Err(); err != nil {
			_ = obj.Close()
			return nil, err
		}
	}

	return obj, nil
}

// // // // // // // // // //

// Core returns the underlying node interface for multicast, admin, peer retry,
// and diagnostics operations.
func (o *Obj) Core() core.Interface { return o.core }

// DialContext opens a connection to a Yggdrasil address.
func (o *Obj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	connection, err := o.core.DialContext(ctx, network, address)
	if err != nil {
		return nil, o.remapClosed(err)
	}
	if o.isClosed() {
		return nil, errors.Join(ErrClosed, connection.Close())
	}
	return connection, nil
}

// Listen creates a TCP listener that is closed with the node.
func (o *Obj) Listen(network, address string) (net.Listener, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	listener, err := o.core.Listen(network, address)
	if err != nil {
		return nil, o.remapClosed(err)
	}
	if o.isClosed() {
		return nil, errors.Join(ErrClosed, listener.Close())
	}
	return listener, nil
}

// ListenPacket creates a packet listener that is closed with the node.
func (o *Obj) ListenPacket(network, address string) (net.PacketConn, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	connection, err := o.core.ListenPacket(network, address)
	if err != nil {
		return nil, o.remapClosed(err)
	}
	if o.isClosed() {
		return nil, errors.Join(ErrClosed, connection.Close())
	}
	return connection, nil
}

// Address returns the node's IPv6 address in the 200::/7 range.
func (o *Obj) Address() net.IP { return o.core.Address() }

// Subnet returns the node's routable /64 subnet in the 300::/7 range.
func (o *Obj) Subnet() net.IPNet { return o.core.Subnet() }

// PublicKey returns the node's Ed25519 public key.
func (o *Obj) PublicKey() ed25519.PublicKey { return o.core.PublicKey() }

// MTU returns the node interface MTU.
func (o *Obj) MTU() uint64 { return o.core.MTU() }

// AddPeer adds a peer URI.
func (o *Obj) AddPeer(uri string) error {
	if o.isClosed() {
		return ErrClosed
	}
	return o.remapClosed(o.core.AddPeer(uri))
}

// RemovePeer removes a previously added peer.
func (o *Obj) RemovePeer(uri string) error {
	if o.isClosed() {
		return ErrClosed
	}
	return o.remapClosed(o.core.RemovePeer(uri))
}

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

func (o *Obj) remapClosed(err error) error {
	if o.isClosed() {
		return errors.Join(ErrClosed, err)
	}
	return err
}

// //

// EnableSOCKS starts the SOCKS5 proxy and its configured resolver.
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
	nameResolver, err := resolver.New(resolverCfg)
	if err != nil {
		return err
	}
	err = server.Start(socks.ConfigObj{
		Network:                            network,
		Addr:                               cfg.Addr,
		Resolver:                           nameResolver,
		OwnResolver:                        true,
		Verbose:                            cfg.Verbose,
		Logger:                             logger,
		MaxConnections:                     cfg.MaxConnections,
		HandshakeTimeout:                   cfg.HandshakeTimeout,
		DialTimeout:                        cfg.DialTimeout,
		TunnelIdleTimeout:                  cfg.TunnelIdleTimeout,
		MaxAssociateTargetsPerSession:      cfg.MaxAssociateTargetsPerSession,
		MaxAssociateTargetsPerPrincipal:    cfg.MaxAssociateTargetsPerPrincipal,
		MaxAssociateQueuedPacketsPerTarget: cfg.MaxAssociateQueuedPacketsPerTarget,
		MaxAssociateQueuedBytesPerTarget:   cfg.MaxAssociateQueuedBytesPerTarget,
		Credentials:                        cfg.Credentials,
	})
	if err != nil {
		return errors.Join(err, nameResolver.Close())
	}
	if o.isClosed() {
		return errors.Join(ErrClosed, server.Close())
	}
	return nil
}

// DisableSOCKS stops the SOCKS5 proxy.
func (o *Obj) DisableSOCKS() error {
	if o.isClosed() {
		return ErrClosed
	}
	err := o.socks.Close()
	if o.isClosed() {
		return errors.Join(ErrClosed, err)
	}
	return err
}

// SetSOCKSMaxConnections adjusts the SOCKS5 connection limit at runtime. If
// Close races the update, the limit may change before ErrClosed is returned.
func (o *Obj) SetSOCKSMaxConnections(n int) error {
	if o.isClosed() {
		return ErrClosed
	}
	o.socks.SetMaxConnections(n)
	if o.isClosed() {
		return ErrClosed
	}
	return nil
}

// SOCKSMaxConnections reports the current SOCKS5 connection limit.
func (o *Obj) SOCKSMaxConnections() int {
	return o.socks.MaxConnections()
}

// //

// PeerManagerActive returns the active managed peers, or nil when the peer
// manager is disabled.
func (o *Obj) PeerManagerActive() []string {
	if o.peerManager == nil {
		return nil
	}
	return o.peerManager.Active()
}

// PeerManagerOptimize triggers an unscheduled managed-peer evaluation.
func (o *Obj) PeerManagerOptimize() error {
	if o.isClosed() {
		return ErrClosed
	}
	if o.peerManager == nil {
		return ErrPeerManagerNotEnabled
	}
	return o.remapClosed(o.peerManager.Optimize())
}

// Ask queries a remote node's NodeInfo by public key.
// Returns parsed metadata, build info, and measured RTT. A partial result may
// accompany an error, including ErrClosed when shutdown races completion.
func (o *Obj) Ask(ctx context.Context, key ed25519.PublicKey) (*ninfo.AskResultObj, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	result, err := o.nodeInfo.Ask(ctx, key)
	if errors.Is(err, ninfo.ErrClosed) || o.isClosed() {
		return result, errors.Join(ErrClosed, err)
	}
	return result, err
}

// AskAddr queries remote NodeInfo by .pk.ygg name, IPv6 address, address with
// port, or hexadecimal public key. A partial result may accompany an error.
func (o *Obj) AskAddr(ctx context.Context, addr string) (*ninfo.AskResultObj, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	result, err := o.nodeInfo.AskAddr(ctx, addr)
	if errors.Is(err, ninfo.ErrClosed) || o.isClosed() {
		return result, errors.Join(ErrClosed, err)
	}
	return result, err
}

// //

// Close stops all components; safe to call multiple times. The total wait is
// bounded by ConfigObj.CloseTimeout. A component that does not return before the
// deadline continues best-effort in a detached goroutine and cannot hold the
// application's shutdown path.
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		close(o.done)

		dependents := []common.NamedCloseObj{
			{Name: "ninfo", Close: o.nodeInfo.Close},
			{Name: "socks", Close: o.socks.Close},
		}
		if o.peerManager != nil {
			dependents = append(dependents, common.NamedCloseObj{Name: "peermgr", Close: o.peerManager.Close})
		}
		closeErr, timedOut := common.CloseWithDeadline(
			effectiveCloseTimeout(o.closeTimeout),
			dependents,
			common.NamedCloseObj{Name: "core", Close: o.core.Close},
		)
		if timedOut {
			o.closeTimedOut.Store(true)
			o.closeErr = errors.Join(closeErr, fmt.Errorf("%w after %s", ErrCloseTimedOut, effectiveCloseTimeout(o.closeTimeout)))
			return
		}
		o.closeErr = closeErr
	})
	return o.closeErr
}
