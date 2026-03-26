package core

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
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

var ErrNotAvailable = fmt.Errorf("netstack is not available")

var _ Interface = (*Obj)(nil)

// Obj — узел Yggdrasil с userspace TCP/UDP стеком.
// Предоставляет стандартные Go-сетевые методы: DialContext, Listen, ListenPacket
type Obj struct {
	core         *yggcore.Core
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
}

// New создаёт и запускает узел Yggdrasil.
// Для корректного завершения вызывающий обязан вызвать Close()
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

	// Ядро Yggdrasil
	var err error
	obj.core, err = yggcore.New(nodeCfg.Certificate, log, buildCoreOptions(nodeCfg, log)...)
	if err != nil {
		return nil, fmt.Errorf("core.New: %w", err)
	}

	// Сетевой стек
	ns, err := newNetstack(obj.core, log, rstQueueSize)
	if err != nil {
		obj.core.Stop()
		return nil, fmt.Errorf("netstack: %w", err)
	}
	obj.netstackPtr.Store(ns)

	log.Infof("[core] address: %s", obj.Address())
	log.Infof("[core] subnet: %s", obj.Subnet())
	log.Infof("[core] public key: %s", hex.EncodeToString(obj.core.PublicKey()))

	return obj, nil
}

// //

// Close корректно останавливает узел; безопасен для повторного вызова.
// Если задан CoreStopTimeout, он ограничивает весь процесс завершения, а не только core.Stop()
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		if o.coreTimeout > 0 {
			done := make(chan struct{})
			go func() {
				o.closeSequence()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(o.coreTimeout):
				o.logger.Warnf("[core] close timed out after %s", o.coreTimeout)
				// Установить nil — дальнейшие вызовы не будут зависать
				o.netstackPtr.Store(nil)
				o.core = nil
			}
		} else {
			o.closeSequence()
		}
	})
	return nil
}

// closeSequence — последовательное завершение всех компонентов
func (o *Obj) closeSequence() {
	// Компоненты — до закрытия core
	if err := o.multicast.disable(); err != nil {
		o.logger.Warnf("[core] multicast disable: %v", err)
	}
	if err := o.adminSocket.disable(); err != nil {
		o.logger.Warnf("[core] admin disable: %v", err)
	}

	// Зарегистрированные ресурсы (listeners и т.д.)
	o.closersMu.Lock()
	for _, c := range o.closers {
		if err := c.Close(); err != nil {
			o.logger.Warnf("[core] closer: %v", err)
		}
	}
	o.closers = nil
	o.closersMu.Unlock()

	// Core останавливается до netstack: ipv6rwc.Read() разблокируется
	// только после core.Stop()
	if o.core != nil {
		o.core.Stop()
		o.core = nil
	}

	if ns := o.netstackPtr.Swap(nil); ns != nil {
		ns.close()
	}
}

// //

// DialContext открывает соединение к Yggdrasil-адресу.
// Совместим с http.Transport.DialContext для использования как HTTP-транспорт
func (o *Obj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return nil, ErrNotAvailable
	}
	return ns.DialContext(ctx, network, address)
}

// Listen создаёт TCP listener на Yggdrasil-адресе.
// Формат адреса: ":port" или "[ipv6]:port".
// Listener автоматически закрывается при Close()
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

// ListenPacket создаёт UDP listener на Yggdrasil-адресе.
// Формат адреса: ":port" или "[ipv6]:port".
// Listener автоматически закрывается при Close()
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

// UnsafeCore — прямой доступ к ядру Yggdrasil.
// Не является частью стабильного API; upstream может измениться без предупреждения
func (o *Obj) UnsafeCore() *yggcore.Core {
	return o.core
}

// Address — IPv6-адрес узла в диапазоне 200::/7
func (o *Obj) Address() net.IP {
	if o.core == nil {
		return nil
	}
	addr := o.core.Address()
	return net.IP(addr[:])
}

// Subnet — маршрутизируемая /64 подсеть узла в диапазоне 300::/7
func (o *Obj) Subnet() net.IPNet {
	if o.core == nil {
		return net.IPNet{}
	}
	return o.core.Subnet()
}

// PublicKey — ed25519 публичный ключ узла (32 байта)
func (o *Obj) PublicKey() ed25519.PublicKey {
	if o.core == nil {
		return nil
	}
	return o.core.PublicKey()
}

// MTU — MTU сетевого интерфейса
func (o *Obj) MTU() uint64 {
	ns := o.netstackPtr.Load()
	if ns == nil {
		return 0
	}
	return ns.MTU()
}

// //

// RSTDropped — количество отброшенных RST-пакетов из-за переполнения очереди
func (o *Obj) RSTDropped() int64 {
	ns := o.netstackPtr.Load()
	if ns == nil || ns.nic == nil {
		return 0
	}
	return ns.nic.rstDropped.Load()
}

// AddPeer добавляет пир в runtime. URI: "tcp://host:port", "quic://host:port"
func (o *Obj) AddPeer(uri string) error {
	if o.core == nil {
		return ErrNotAvailable
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("url.Parse: %w", err)
	}
	return o.core.AddPeer(u, "")
}

// RemovePeer удаляет пир в runtime
func (o *Obj) RemovePeer(uri string) error {
	if o.core == nil {
		return ErrNotAvailable
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("url.Parse: %w", err)
	}
	return o.core.RemovePeer(u, "")
}

// GetPeers возвращает информацию обо всех пирах (подключённых и настроенных)
func (o *Obj) GetPeers() []yggcore.PeerInfo {
	if o.core == nil {
		return nil
	}
	return o.core.GetPeers()
}

// //

// EnableMulticast включает mDNS-обнаружение пиров в локальной сети.
// Интерфейсы берутся из NodeConfig.MulticastInterfaces.
// logger — специфичный для multicast (upstream требует *golog.Logger)
func (o *Obj) EnableMulticast(logger *golog.Logger) error {
	err := o.multicast.enable(func() (any, func() error, error) {
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
		mc, err := multicast.New(o.core, logger, options...)
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

// DisableMulticast останавливает mDNS-обнаружение
func (o *Obj) DisableMulticast() error {
	return o.multicast.disable()
}

// //

// EnableAdmin запускает admin-сокет на указанном адресе.
// Формат: "unix:///path" или "tcp://host:port"
func (o *Obj) EnableAdmin(addr string) error {
	err := o.adminSocket.enable(func() (any, func() error, error) {
		as, err := admin.New(o.core, o.logger, admin.ListenAddress(addr))
		if err != nil {
			return nil, nil, fmt.Errorf("admin.New: %w", err)
		}
		if as == nil {
			return nil, nil, fmt.Errorf("admin socket disabled for address %q", addr)
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

// DisableAdmin останавливает admin-сокет
func (o *Obj) DisableAdmin() error {
	return o.adminSocket.disable()
}

// //

// registerAdminHandlers связывает admin и multicast если оба активны.
// Вызывается после enable(), когда componentObj.mu уже отпущен
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
