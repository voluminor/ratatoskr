package core

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	golog "github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
)

// // // // // // // // // //

var _ Interface = (*Obj)(nil)

// Obj — Yggdrasil node with a userspace TCP/UDP stack.
// Provides standard Go networking methods: DialContext, Listen, ListenPacket
type Obj struct {
	corePtr      atomic.Pointer[yggcore.Core]
	nodeCfg      *config.NodeConfig
	netstackPtr  atomic.Pointer[netstackObj]
	logger       yggcore.Logger
	multicast    componentObj
	adminSocket  componentObj
	handlersMu   sync.Mutex
	closeOnce    sync.Once
	closers      []io.Closer
	closersMu    sync.Mutex
	coreTimeout  time.Duration
	rstQueueSize int
	closeErr     error
}

// New creates and starts the Yggdrasil node.
// For proper shutdown, the caller must call Close()
func New(cfg ConfigObj) (*Obj, error) {
	log := cfg.Logger

	nodeCfg := cfg.Config
	if nodeCfg == nil {
		nodeCfg = config.GenerateConfig()
		nodeCfg.AdminListen = "none"
	}

	rstQueueSize := cfg.RSTQueueSize
	if rstQueueSize <= 0 {
		rstQueueSize = 100
	}

	obj := &Obj{
		nodeCfg:      nodeCfg,
		logger:       log,
		coreTimeout:  cfg.CoreStopTimeout,
		rstQueueSize: rstQueueSize,
		multicast:    componentObj{name: "multicast"},
		adminSocket:  componentObj{name: "admin"},
	}

	// Yggdrasil core
	c, err := yggcore.New(nodeCfg.Certificate, log, buildCoreOptions(nodeCfg, log)...)
	if err != nil {
		return nil, fmt.Errorf("core.New: %w", err)
	}
	obj.corePtr.Store(c)

	// Network stack
	ns, err := newNetstack(c, log, rstQueueSize)
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

// Close stops the node; safe to call multiple times.
// CoreStopTimeout limits the entire shutdown process, not just core.Stop()
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		if o.coreTimeout > 0 {
			done := make(chan struct{})
			go func() {
				o.closeErr = o.closeSequence()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(o.coreTimeout):
				o.logger.Warnf("[core] close timed out after %s", o.coreTimeout)
				o.netstackPtr.Store(nil)
				o.corePtr.Store(nil)
				o.closeErr = fmt.Errorf("%w after %s", ErrCloseTimedOut, o.coreTimeout)
			}
		} else {
			o.closeErr = o.closeSequence()
		}
	})
	return o.closeErr
}

// closeSequence — sequential shutdown of all components
func (o *Obj) closeSequence() error {
	var errs []error

	// Components — before closing core
	if err := o.multicast.disable(); err != nil {
		errs = append(errs, fmt.Errorf("multicast: %w", err))
	}
	if err := o.adminSocket.disable(); err != nil {
		errs = append(errs, fmt.Errorf("admin: %w", err))
	}

	// Registered resources (listeners, etc.)
	o.closersMu.Lock()
	for _, c := range o.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	o.closers = nil
	o.closersMu.Unlock()

	// Core stops before netstack: ipv6rwc.Read() unblocks
	// only after core.Stop()
	if c := o.corePtr.Swap(nil); c != nil {
		c.Stop()
	}

	if ns := o.netstackPtr.Swap(nil); ns != nil {
		ns.close()
	}

	return errors.Join(errs...)
}

// //

// DialContext opens a connection to a Yggdrasil address; compatible with http.Transport.DialContext
func (o *Obj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	return ns.DialContext(ctx, network, address)
}

// Listen creates a TCP listener; ":port" or "[ipv6]:port".
// Closed automatically on Close()
func (o *Obj) Listen(network, address string) (net.Listener, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	ln, err := ns.Listen(network, address)
	if err != nil {
		return nil, err
	}
	o.addCloser(ln)
	return ln, nil
}

// ListenPacket creates a UDP listener; ":port" or "[ipv6]:port".
// Closed automatically on Close()
func (o *Obj) ListenPacket(network, address string) (net.PacketConn, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	pc, err := ns.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	o.addCloser(pc)
	return pc, nil
}

// //

// UnsafeCore — direct access to the core; unstable API
func (o *Obj) UnsafeCore() *yggcore.Core {
	return o.corePtr.Load()
}

// Address — node IPv6 address in the 200::/7 range
func (o *Obj) Address() net.IP {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	addr := c.Address()
	return net.IP(addr[:])
}

// Subnet — routable /64 subnet of the node in the 300::/7 range
func (o *Obj) Subnet() net.IPNet {
	c := o.corePtr.Load()
	if c == nil {
		return net.IPNet{}
	}
	return c.Subnet()
}

// PublicKey — ed25519 public key of the node (32 bytes)
func (o *Obj) PublicKey() ed25519.PublicKey {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.PublicKey()
}

// MTU returns the MTU of the NIC interface
func (o *Obj) MTU() uint64 {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return 0
	}
	return ns.MTU()
}

// //

// RSTDropped — count of RST packets dropped on queue overflow
func (o *Obj) RSTDropped() int64 {
	ns := o.netstackPtr.Load()
	if ns == nil || ns.nic == nil {
		return 0
	}
	return ns.nic.rstDropped.Load()
}

// AddPeer adds a peer; URI: "tcp://...", "quic://...", etc.
func (o *Obj) AddPeer(uri string) error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("url.Parse: %w", err)
	}
	return c.AddPeer(u, "")
}

func (o *Obj) RemovePeer(uri string) error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("url.Parse: %w", err)
	}
	return c.RemovePeer(u, "")
}

// GetPeers — all peers (connected and configured)
func (o *Obj) GetPeers() []yggcore.PeerInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetPeers()
}

// //

// EnableMulticast enables mDNS discovery on the local network.
// Interfaces are taken from NodeConfig.MulticastInterfaces
func (o *Obj) EnableMulticast(logger *golog.Logger) error {
	err := o.multicast.enable(func() (any, func() error, error) {
		c := o.corePtr.Load()
		if c == nil {
			return nil, nil, ErrNotAvailable
		}
		options := make([]multicast.SetupOption, 0, len(o.nodeCfg.MulticastInterfaces))
		for _, intf := range o.nodeCfg.MulticastInterfaces {
			re, err := regexp.Compile(intf.Regex)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid multicast regex %q: %w", intf.Regex, err)
			}
			options = append(options, multicast.MulticastInterface{
				Regex:    re,
				Beacon:   intf.Beacon,
				Listen:   intf.Listen,
				Port:     intf.Port,
				Priority: uint8(intf.Priority),
				Password: intf.Password,
			})
		}
		mc, err := multicast.New(c, logger, options...)
		if err != nil {
			return nil, nil, fmt.Errorf("multicast.New: %w", err)
		}
		return mc, mc.Stop, nil
	})
	if err != nil {
		return err
	}
	o.registerAdminHandlers()
	return nil
}

func (o *Obj) DisableMulticast() error {
	return o.multicast.disable()
}

// //

// EnableAdmin starts the admin socket; "unix:///path" or "tcp://host:port"
func (o *Obj) EnableAdmin(addr string) error {
	err := o.adminSocket.enable(func() (any, func() error, error) {
		c := o.corePtr.Load()
		if c == nil {
			return nil, nil, ErrNotAvailable
		}
		as, err := admin.New(c, o.logger, admin.ListenAddress(addr))
		if err != nil {
			return nil, nil, fmt.Errorf("admin.New: %w", err)
		}
		if as == nil {
			return nil, nil, fmt.Errorf("%w for address %q", ErrAdminDisabled, addr)
		}
		as.SetupAdminHandlers()
		return as, as.Stop, nil
	})
	if err != nil {
		return err
	}
	o.registerAdminHandlers()
	return nil
}

func (o *Obj) DisableAdmin() error {
	return o.adminSocket.disable()
}

// //

// registerAdminHandlers wires admin and multicast together if both are active
func (o *Obj) registerAdminHandlers() {
	o.handlersMu.Lock()
	defer o.handlersMu.Unlock()

	as, _ := o.adminSocket.get().(*admin.AdminSocket)
	mc, _ := o.multicast.get().(*multicast.Multicast)
	if as != nil && mc != nil {
		mc.SetupAdminHandlers(as)
	}
}

func (o *Obj) addCloser(c io.Closer) {
	o.closersMu.Lock()
	o.closers = append(o.closers, c)
	o.closersMu.Unlock()
}

func buildCoreOptions(cfg *config.NodeConfig, log yggcore.Logger) []yggcore.SetupOption {
	n := 2 + len(cfg.Listen) + len(cfg.Peers) + len(cfg.AllowedPublicKeys)
	for _, peers := range cfg.InterfacePeers {
		n += len(peers)
	}
	opts := make([]yggcore.SetupOption, 0, n)
	opts = append(opts, yggcore.NodeInfo(cfg.NodeInfo))
	opts = append(opts, yggcore.NodeInfoPrivacy(cfg.NodeInfoPrivacy))
	for _, addr := range cfg.Listen {
		opts = append(opts, yggcore.ListenAddress(addr))
	}
	for _, peer := range cfg.Peers {
		opts = append(opts, yggcore.Peer{URI: peer})
	}
	for intf, peers := range cfg.InterfacePeers {
		for _, peer := range peers {
			opts = append(opts, yggcore.Peer{URI: peer, SourceInterface: intf})
		}
	}
	for _, allowed := range cfg.AllowedPublicKeys {
		k, err := hex.DecodeString(allowed)
		if err != nil {
			log.Debugf("[core] skipping invalid AllowedPublicKey %q: %v", allowed, err)
			continue
		}
		opts = append(opts, yggcore.AllowedPublicKey(k[:]))
	}
	return opts
}
