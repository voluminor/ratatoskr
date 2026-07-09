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

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
)

// // // // // // // // // //

var _ Interface = (*Obj)(nil)

const (
	minimumMTU      = 1280
	defaultRSTQueue = 100
	maxRSTQueue     = 65536
)

func normalizeMTU(requested, max uint64) uint64 {
	mtu := requested
	if mtu == 0 {
		mtu = max
	}
	if mtu < minimumMTU {
		mtu = minimumMTU
	}
	// Clamp to max last so the result never exceeds it.
	if mtu > max {
		mtu = max
	}
	return mtu
}

// Obj — Yggdrasil node with a userspace TCP/UDP stack.
// Provides standard Go networking methods: DialContext, Listen, ListenPacket
type Obj struct {
	corePtr     atomic.Pointer[yggcore.Core]
	nodeCfg     *config.NodeConfig
	netstackPtr atomic.Pointer[netstackObj]
	logger      yggcore.Logger
	multicast   componentObj[*multicast.Multicast]
	adminSocket componentObj[*admin.AdminSocket]
	// adminMu is the single lock for admin/multicast handler transitions;
	// it guards adminAddr and handlersWired.
	adminMu       sync.Mutex
	adminAddr     string
	handlersWired bool
	closeOnce     sync.Once
	closers       map[io.Closer]struct{}
	closersMu     sync.Mutex
	closersDone   bool
	coreTimeout   time.Duration
	rstQueueSize  int
	closeErr      error
}

// New creates and starts the Yggdrasil node.
// For proper shutdown, the caller must call Close()
func New(cfg ConfigObj) (*Obj, error) {
	log := common.NormalizeLogger(cfg.Logger)

	nodeCfg := cfg.Config
	if nodeCfg == nil {
		nodeCfg = config.GenerateConfig()
		nodeCfg.AdminListen = "none"
	}

	rstQueueSize := cfg.RSTQueueSize
	if rstQueueSize <= 0 {
		rstQueueSize = defaultRSTQueue
	}
	if rstQueueSize > maxRSTQueue {
		return nil, fmt.Errorf("%w: got %d, max %d", ErrRSTQueueTooLarge, rstQueueSize, maxRSTQueue)
	}

	obj := &Obj{
		nodeCfg:      nodeCfg,
		logger:       log,
		coreTimeout:  cfg.CoreStopTimeout,
		rstQueueSize: rstQueueSize,
		multicast:    componentObj[*multicast.Multicast]{name: "multicast"},
		adminSocket:  componentObj[*admin.AdminSocket]{name: "admin"},
	}

	// Yggdrasil core
	opts, err := buildCoreOptions(nodeCfg)
	if err != nil {
		return nil, err
	}
	c, err := yggcore.New(nodeCfg.Certificate, log, opts...)
	if err != nil {
		return nil, fmt.Errorf("core.New: %w", err)
	}
	obj.corePtr.Store(c)

	// Network stack
	ns, err := newNetstack(c, log, rstQueueSize, nodeCfg.IfMTU)
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
// Owned components (multicast, admin) are released synchronously, before the
// bounded region. The upstream core stop, the netstack teardown, and the tracked
// listeners then run in that order, bounded by CoreStopTimeout (0 → no limit).
// On timeout Close returns ErrCloseTimedOut while a single background goroutine
// finishes; a genuinely hung core.Stop() keeps its transport sockets until it
// completes — the inherent limit of bounding an uncancellable upstream call.
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		c := o.corePtr.Swap(nil)
		ns := o.netstackPtr.Swap(nil)
		errs := o.closeOwned()
		errs = append(errs, o.stopCoreAndStack(c, ns)...)
		o.closeErr = errors.Join(errs...)
	})
	return o.closeErr
}

// closeOwned releases the components this module owns outright. They do not block
// on the upstream core, so they are torn down synchronously before it is stopped.
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

// stopCoreAndStack stops the core, destroys the netstack, then closes tracked
// listeners — in that order so ipv6rwc.Read unblocks and the reload-critical
// sockets are freed first. Only this pair can block unpredictably, so it is the
// sole part bounded by CoreStopTimeout: on timeout Close returns ErrCloseTimedOut
// while a single teardown goroutine finishes once the core does. All stops are
// idempotent.
func (o *Obj) stopCoreAndStack(c *yggcore.Core, ns *netstackObj) []error {
	if o.coreTimeout <= 0 {
		return o.teardown(c, ns)
	}
	done := make(chan []error, 1)
	go func() {
		done <- o.teardown(c, ns)
	}()
	timer := time.NewTimer(o.coreTimeout)
	defer timer.Stop()
	select {
	case errs := <-done:
		return errs
	case <-timer.C:
		o.logger.Warnf("[core] core stop timed out after %s", o.coreTimeout)
		return []error{fmt.Errorf("%w after %s", ErrCloseTimedOut, o.coreTimeout)}
	}
}

// teardown performs the ordered, blocking part of shutdown; its listener errors
// are dropped on the timeout path where ErrCloseTimedOut already dominates.
func (o *Obj) teardown(c *yggcore.Core, ns *netstackObj) []error {
	if c != nil {
		c.Stop()
	}
	if ns != nil {
		ns.close()
	}
	return o.closeTrackedResources()
}

// closeTrackedResources closes and clears every registered resource (listeners,
// etc.) outside the lock, because their Close() calls back into removeCloser.
func (o *Obj) closeTrackedResources() []error {
	o.closersMu.Lock()
	closers := o.closers
	o.closers = nil
	o.closersDone = true
	o.closersMu.Unlock()
	var errs []error
	for cl := range closers {
		if err := cl.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
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
	tracked := &trackedListenerObj{Listener: ln, owner: o}
	if !o.addCloser(tracked) {
		_ = ln.Close()
		return nil, ErrNotAvailable
	}
	return tracked, nil
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
	tracked := &trackedPacketConnObj{PacketConn: pc, owner: o}
	if !o.addCloser(tracked) {
		_ = pc.Close()
		return nil, ErrNotAvailable
	}
	return tracked, nil
}

// //

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
func (o *Obj) RSTDropped() uint64 {
	ns := o.netstackPtr.Load()
	if ns == nil || ns.nic == nil {
		return 0
	}
	return ns.nic.rstDropped.Load()
}

func (o *Obj) RetryPeers() error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	c.RetryPeersNow()
	return nil
}

func (o *Obj) SetAdmin(a yggcore.AddHandler) error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	return c.SetAdmin(a)
}

func (o *Obj) SendLookup(key ed25519.PublicKey) {
	if c := o.corePtr.Load(); c != nil {
		c.SendLookup(key)
	}
}

func (o *Obj) GetSelf() yggcore.SelfInfo {
	c := o.corePtr.Load()
	if c == nil {
		return yggcore.SelfInfo{}
	}
	return c.GetSelf()
}

func (o *Obj) GetSessions() []yggcore.SessionInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetSessions()
}

func (o *Obj) GetTree() []yggcore.TreeEntryInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetTree()
}

func (o *Obj) GetPaths() []yggcore.PathEntryInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetPaths()
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
func (o *Obj) EnableMulticast() error {
	err := o.multicast.enable(func() (*multicast.Multicast, func() error, error) {
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
		mc, err := multicast.New(c, o.logger, options...)
		if err != nil {
			return nil, nil, fmt.Errorf("multicast.New: %w", err)
		}
		return mc, mc.Stop, nil
	})
	if err != nil {
		return err
	}
	// Wire multicast handlers under the same lock as admin transitions.
	o.adminMu.Lock()
	o.syncAdminHandlers()
	o.adminMu.Unlock()
	return nil
}

func (o *Obj) DisableMulticast() error {
	if err := o.multicast.disable(); err != nil {
		return err
	}
	return o.restartAdminAfterMulticastChange()
}

// //

func (o *Obj) newAdminSocket(addr string) (*admin.AdminSocket, func() error, error) {
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
}

// EnableAdmin starts the admin socket; "unix:///path" or "tcp://host:port"
func (o *Obj) EnableAdmin(addr string) error {
	o.adminMu.Lock()
	defer o.adminMu.Unlock()

	err := o.adminSocket.enable(func() (*admin.AdminSocket, func() error, error) {
		return o.newAdminSocket(addr)
	})
	if err != nil {
		return err
	}
	o.adminAddr = addr
	o.syncAdminHandlers()
	return nil
}

func (o *Obj) DisableAdmin() error {
	o.adminMu.Lock()
	defer o.adminMu.Unlock()

	err := o.adminSocket.disable()
	o.adminAddr = ""
	o.handlersWired = false
	return err
}

// //

// restartAdminAfterMulticastChange rebuilds the admin socket after multicast is
// disabled, dropping the now-stale multicast handlers. It is a no-op unless
// those handlers were actually wired.
func (o *Obj) restartAdminAfterMulticastChange() error {
	o.adminMu.Lock()
	defer o.adminMu.Unlock()

	if o.adminAddr == "" || !o.handlersWired {
		return nil
	}
	addr := o.adminAddr
	// yggdrasil-go exposes no way to unregister handlers, so rebuild the socket.
	err := o.adminSocket.disable()
	o.handlersWired = false
	if err != nil {
		return err
	}
	if err := o.adminSocket.enable(func() (*admin.AdminSocket, func() error, error) {
		return o.newAdminSocket(addr)
	}); err != nil {
		return err
	}
	o.syncAdminHandlers()
	return nil
}

// syncAdminHandlers wires multicast handlers into the admin socket exactly once,
// when both components are active. Caller must hold adminMu.
func (o *Obj) syncAdminHandlers() {
	if o.handlersWired {
		return
	}
	as, adminActive := o.adminSocket.get()
	mc, multicastActive := o.multicast.get()
	if !adminActive || !multicastActive || as == nil || mc == nil {
		return
	}
	mc.SetupAdminHandlers(as)
	o.handlersWired = true
}

// //

// trackedListenerObj unregisters itself from the owner on Close,
// so caller-closed listeners do not accumulate until node Close()
type trackedListenerObj struct {
	net.Listener
	owner *Obj
}

func (t *trackedListenerObj) Close() error {
	t.owner.removeCloser(t)
	return t.Listener.Close()
}

// trackedPacketConnObj — same as trackedListenerObj for packet listeners
type trackedPacketConnObj struct {
	net.PacketConn
	owner *Obj
}

func (t *trackedPacketConnObj) Close() error {
	t.owner.removeCloser(t)
	return t.PacketConn.Close()
}

// addCloser registers a resource for Close(); returns false once shutdown has started
func (o *Obj) addCloser(c io.Closer) bool {
	o.closersMu.Lock()
	defer o.closersMu.Unlock()
	if o.closersDone {
		return false
	}
	if o.closers == nil {
		o.closers = make(map[io.Closer]struct{})
	}
	o.closers[c] = struct{}{}
	return true
}

func (o *Obj) removeCloser(c io.Closer) {
	o.closersMu.Lock()
	delete(o.closers, c)
	o.closersMu.Unlock()
}

func buildCoreOptions(cfg *config.NodeConfig) ([]yggcore.SetupOption, error) {
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
	// A malformed AllowedPublicKey is rejected rather than skipped: silently
	// dropping every invalid entry can leave the allowlist empty, which upstream
	// treats as "allow all inbound peering" — a typo must not open the node.
	for _, allowed := range cfg.AllowedPublicKeys {
		k, err := hex.DecodeString(allowed)
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", ErrInvalidAllowedPublicKey, allowed, err)
		}
		if len(k) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w %q: got %d bytes, expected %d", ErrInvalidAllowedPublicKey, allowed, len(k), ed25519.PublicKeySize)
		}
		opts = append(opts, yggcore.AllowedPublicKey(k))
	}
	return opts, nil
}
