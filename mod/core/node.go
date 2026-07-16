// Package core embeds a Yggdrasil node behind standard Go networking APIs.
package core

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/core/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
)

// // // // // // // // // //

var _ Interface = (*Obj)(nil)

const (
	minimumMTU = 1280
)

func normalizeMTU(requested, max uint64) uint64 {
	mtu := requested
	if mtu == 0 {
		mtu = max
	}
	if mtu < minimumMTU {
		mtu = minimumMTU
	}
	if mtu > max {
		mtu = max
	}
	return mtu
}

// Obj is a Yggdrasil node with a userspace TCP/IP stack.
type Obj struct {
	corePtr     atomic.Pointer[yggcore.Core]
	nodeCfg     *config.NodeConfig
	netstackPtr atomic.Pointer[netstackObj]
	logger      yggcore.Logger
	multicast   componentObj[*multicast.Multicast]
	adminSocket componentObj[*admin.Obj]
	adminMu     sync.Mutex
	closeOnce   sync.Once
	closeErr    error
}

// New creates and starts a node. The caller must close the returned object.
func New(cfg ConfigObj) (*Obj, error) {
	log := common.NormalizeLogger(cfg.Logger)

	nodeCfg := cfg.Config
	if nodeCfg == nil {
		nodeCfg = config.GenerateConfig()
		nodeCfg.AdminListen = "none"
	} else {
		cloned := *nodeCfg
		cloned.MulticastInterfaces = slices.Clone(nodeCfg.MulticastInterfaces)
		var err error
		cloned.NodeInfo, err = common.CloneNodeInfo(nodeCfg.NodeInfo)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidNodeInfo, err)
		}
		nodeCfg = &cloned
	}

	obj := &Obj{
		nodeCfg:     nodeCfg,
		logger:      log,
		multicast:   componentObj[*multicast.Multicast]{name: "multicast"},
		adminSocket: componentObj[*admin.Obj]{name: "admin"},
	}

	opts, err := buildCoreOptions(nodeCfg)
	if err != nil {
		return nil, err
	}
	c, err := yggcore.New(nodeCfg.Certificate, log, opts...)
	if err != nil {
		return nil, fmt.Errorf("core.New: %w", err)
	}
	obj.corePtr.Store(c)

	ns, err := newNetstack(c, log, nodeCfg.IfMTU)
	if err != nil {
		c.Stop()
		return nil, fmt.Errorf("netstack: %w", err)
	}
	obj.netstackPtr.Store(ns)

	log.Infof("[core] address: %s", obj.Address())
	log.Infof("[core] subnet: %s", obj.Subnet())
	log.Infof("[core] public key: %s", hex.EncodeToString(c.PublicKey()))

	return obj, nil
}

// //

// Close stops the node and all components it owns. Repeated calls return the
// same result.
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		c := o.corePtr.Swap(nil)
		ns := o.netstackPtr.Swap(nil)
		errs := o.closeOwned()
		o.teardown(c, ns)
		o.closeErr = errors.Join(errs...)
	})
	return o.closeErr
}

func (o *Obj) closeOwned() []error {
	var errs []error
	if err := o.multicast.disable(); err != nil {
		errs = append(errs, fmt.Errorf("multicast: %w", err))
	}
	if err := o.adminSocket.disable(); err != nil {
		errs = append(errs, fmt.Errorf("admin: %w", err))
	}
	return errs
}

func (o *Obj) teardown(c *yggcore.Core, ns *netstackObj) {
	if ns != nil {
		ns.close()
		return
	}
	if c != nil {
		c.Stop()
	}
}

// //

// DialContext opens a TCP or UDP connection to a Yggdrasil address.
func (o *Obj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	return ns.DialContext(ctx, network, address)
}

// Listen creates a TCP listener that is aborted when the node closes.
func (o *Obj) Listen(network, address string) (net.Listener, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	return ns.Listen(network, address)
}

// ListenPacket creates a UDP endpoint that is aborted when the node closes.
func (o *Obj) ListenPacket(network, address string) (net.PacketConn, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	return ns.ListenPacket(network, address)
}

// //

// Address returns the node IPv6 address in 200::/7.
func (o *Obj) Address() net.IP {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	addr := c.Address()
	return net.IP(addr[:])
}

// Subnet returns the node's routable /64 subnet in 300::/7.
func (o *Obj) Subnet() net.IPNet {
	c := o.corePtr.Load()
	if c == nil {
		return net.IPNet{}
	}
	return c.Subnet()
}

// PublicKey returns an owned copy of the node's Ed25519 public key.
func (o *Obj) PublicKey() ed25519.PublicKey {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return slices.Clone(c.PublicKey())
}

// MTU returns the current virtual interface MTU.
func (o *Obj) MTU() uint64 {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return 0
	}
	return ns.MTU()
}

// //

// SetAdmin exposes Yggdrasil's low-level handler-registration hook. It is unsafe
// to call concurrently with another SetAdmin, EnableAdmin, or EnableMulticast:
// upstream mutates handler registries without synchronization, and a callback
// can observe privileged debug handlers. Never pass an untrusted implementation.
func (o *Obj) SetAdmin(a yggcore.AddHandler) error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	return c.SetAdmin(a)
}

// SendLookup requests a path lookup for key when the node is available.
func (o *Obj) SendLookup(key ed25519.PublicKey) {
	if c := o.corePtr.Load(); c != nil {
		c.SendLookup(key)
	}
}

// GetSelf returns the node's current routing information.
func (o *Obj) GetSelf() yggcore.SelfInfo {
	c := o.corePtr.Load()
	if c == nil {
		return yggcore.SelfInfo{}
	}
	return c.GetSelf()
}

// GetSessions returns the node's current encrypted sessions.
func (o *Obj) GetSessions() []yggcore.SessionInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetSessions()
}

// GetTree returns the node's current spanning-tree entries.
func (o *Obj) GetTree() []yggcore.TreeEntryInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetTree()
}

// GetPaths returns the node's current path-cache entries.
func (o *Obj) GetPaths() []yggcore.PathEntryInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetPaths()
}
