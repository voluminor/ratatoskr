package ratatoskr

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/crypto/ed25519"

	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/resolver"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

// Obj — Yggdrasil node for embedding in applications.
// Combines core (DialContext/Listen), resolver (.pk.ygg), and SOCKS5.
// All core networking methods are available directly via interface embedding.
// Multicast and Admin are accessible via core.Interface
type Obj struct {
	core.Interface
	socksServer socks.Interface
	peerMgr     *peermgr.Obj
	nodeInfo    *ninfo.Obj
	logger      yggcore.Logger
	done        chan struct{}
	closeOnce   sync.Once
}

// New creates and starts the node.
// If cfg.Peers is set, starts the peer manager; cfg.Config.Peers must be empty.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.Logger == nil {
		cfg.Logger = noopLoggerObj{}
	}

	if cfg.Peers != nil && cfg.Config != nil && len(cfg.Config.Peers) > 0 {
		return nil, ErrPeersConflict
	}

	// Assemble NodeInfo from sigils
	var sigilsObj *ninfo.SigilsObj
	if cfg.Sigils != nil {
		if cfg.Config == nil {
			cfg.Config = config.GenerateConfig()
			cfg.Config.AdminListen = "none"
		}
		var errs []error
		sigilsObj, errs = ninfo.Sigils(cfg.Config.NodeInfo, cfg.Sigils...)
		for _, e := range errs {
			cfg.Logger.Warnf("[ratatoskr] sigil: %v", e)
		}
		cfg.Config.NodeInfo = sigilsObj.NodeInfo()
	}

	coreNode, err := core.New(core.ConfigObj{
		Config:          cfg.Config,
		Logger:          cfg.Logger,
		CoreStopTimeout: cfg.CoreStopTimeout,
	})
	if err != nil {
		return nil, err
	}

	// ninfo — always created for Ask/AskAddr
	ni, err := ninfo.New(coreNode.UnsafeCore(), cfg.Logger)
	if err != nil {
		_ = coreNode.Close()
		return nil, fmt.Errorf("ninfo: %w", err)
	}
	if sigilsObj != nil {
		for _, e := range ni.ImportSigils(sigilsObj, ninfo.ImportAppend) {
			cfg.Logger.Warnf("[ratatoskr] parse sigil: %v", e)
		}
	}

	obj := &Obj{
		Interface:   coreNode,
		nodeInfo:    ni,
		socksServer: socks.New(coreNode),
		logger:      cfg.Logger,
		done:        make(chan struct{}),
	}

	if cfg.Peers != nil {
		pCfg := *cfg.Peers
		if pCfg.Logger == nil {
			pCfg.Logger = cfg.Logger
		}
		mgr, err := peermgr.New(coreNode, pCfg)
		if err != nil {
			_ = coreNode.Close()
			return nil, fmt.Errorf("peer manager: %w", err)
		}
		if err := mgr.Start(); err != nil {
			_ = coreNode.Close()
			return nil, fmt.Errorf("peer manager: %w", err)
		}
		obj.peerMgr = mgr
	}

	// Auto-shutdown on context cancellation
	if cfg.Ctx != nil {
		go func() {
			select {
			case <-cfg.Ctx.Done():
				obj.Close()
			case <-obj.done:
			}
		}()
	}

	return obj, nil
}

// //

// EnableSOCKS starts the SOCKS5 proxy with the given parameters.
// Resolver is created automatically based on cfg.Nameserver
func (o *Obj) EnableSOCKS(cfg SOCKSConfigObj) error {
	return o.socksServer.Enable(socks.EnableConfigObj{
		Addr:           cfg.Addr,
		Resolver:       resolver.New(o.Interface, cfg.Nameserver),
		Verbose:        cfg.Verbose,
		Logger:         o.logger,
		MaxConnections: cfg.MaxConnections,
	})
}

func (o *Obj) DisableSOCKS() error {
	return o.socksServer.Disable()
}

// //

// PeerManagerActive returns the current list of active peers; nil if the manager is not used
func (o *Obj) PeerManagerActive() []string {
	if o.peerMgr == nil {
		return nil
	}
	return o.peerMgr.Active()
}

// PeerManagerOptimize triggers an unscheduled peer re-evaluation
func (o *Obj) PeerManagerOptimize() error {
	if o.peerMgr == nil {
		return ErrPeerManagerNotEnabled
	}
	return o.peerMgr.Optimize()
}

// RetryPeers initiates immediate reconnection of disconnected peers
func (o *Obj) RetryPeers() {
	if coreNode, ok := o.Interface.(*core.Obj); ok {
		coreNode.UnsafeCore().RetryPeersNow()
	}
}

// Ask queries a remote node's NodeInfo by public key.
// Returns parsed metadata, build info, and measured RTT
func (o *Obj) Ask(ctx context.Context, key ed25519.PublicKey) (*ninfo.AskResultObj, error) {
	return o.nodeInfo.Ask(ctx, key)
}

// AskAddr queries a remote node's NodeInfo by address string.
// Supported formats: "<hex>.pk.ygg", "[ip6]:port", "ip6", raw 64-char hex
func (o *Obj) AskAddr(ctx context.Context, addr string) (*ninfo.AskResultObj, error) {
	return o.nodeInfo.AskAddr(ctx, addr)
}

// //

// Close stops all components; safe to call multiple times
func (o *Obj) Close() error {
	var closeErr error
	o.closeOnce.Do(func() {
		close(o.done)
		if o.peerMgr != nil {
			o.peerMgr.Stop()
		}
		closeErr = errors.Join(
			o.socksServer.Disable(),
			o.nodeInfo.Close(),
			o.Interface.Close(),
		)
	})
	return closeErr
}
