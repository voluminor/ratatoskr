package ratatoskr

import (
	"fmt"
	"sync"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/resolver"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

// Obj — узел Yggdrasil для встраивания в приложения.
// Объединяет ядро (DialContext/Listen), резолвер (.pk.ygg) и SOCKS5.
// Все сетевые методы ядра доступны напрямую через встраивание интерфейса.
// Multicast и Admin доступны через core.Interface
type Obj struct {
	core.Interface
	socksServer socks.Interface
	peerMgr     *peermgr.Obj
	logger      yggcore.Logger
	done        chan struct{}
	closeOnce   sync.Once
}

// New создаёт и запускает узел.
// Если cfg.Peers задан, запускается менеджер пиров; cfg.Config.Peers при этом должен быть пустым.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.Logger == nil {
		cfg.Logger = noopLoggerObj{}
	}

	if cfg.Peers != nil && cfg.Config != nil && len(cfg.Config.Peers) > 0 {
		return nil, fmt.Errorf("cannot use Config.Peers and Peers manager simultaneously")
	}

	coreNode, err := core.New(core.ConfigObj{
		Config:          cfg.Config,
		Logger:          cfg.Logger,
		CoreStopTimeout: cfg.CoreStopTimeout,
	})
	if err != nil {
		return nil, err
	}

	obj := &Obj{
		Interface:   coreNode,
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

	// Автозавершение при отмене контекста
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

// EnableSOCKS запускает SOCKS5-прокси с указанными параметрами.
// Резолвер создаётся автоматически на основе cfg.Nameserver
func (o *Obj) EnableSOCKS(cfg SOCKSConfigObj) error {
	return o.socksServer.Enable(socks.EnableConfigObj{
		Addr:           cfg.Addr,
		Resolver:       resolver.New(o.Interface, cfg.Nameserver),
		Verbose:        cfg.Verbose,
		Logger:         o.logger,
		MaxConnections: cfg.MaxConnections,
	})
}

// DisableSOCKS останавливает SOCKS5-прокси
func (o *Obj) DisableSOCKS() error {
	return o.socksServer.Disable()
}

// //

// PeerManagerActive возвращает текущий список активных пиров менеджера.
// Возвращает nil если менеджер не используется.
func (o *Obj) PeerManagerActive() []string {
	if o.peerMgr == nil {
		return nil
	}
	return o.peerMgr.Active()
}

// PeerManagerOptimize запускает внеплановую перепроверку пиров.
// Возвращает ошибку если менеджер не используется.
func (o *Obj) PeerManagerOptimize() error {
	if o.peerMgr == nil {
		return fmt.Errorf("peer manager not enabled")
	}
	return o.peerMgr.Optimize()
}

// //

// RetryPeers немедленно инициирует переподключение ко всем отключённым пирам
func (o *Obj) RetryPeers() {
	if coreNode, ok := o.Interface.(*core.Obj); ok {
		coreNode.UnsafeCore().RetryPeersNow()
	}
}

// //

// Close корректно останавливает все компоненты и ядро; безопасен для повторного вызова
func (o *Obj) Close() error {
	o.closeOnce.Do(func() {
		close(o.done)
		if o.peerMgr != nil {
			o.peerMgr.Stop()
		}
		if err := o.socksServer.Disable(); err != nil {
			o.logger.Warnf("socks disable: %v", err)
		}
		if err := o.Interface.Close(); err != nil {
			o.logger.Warnf("core close: %v", err)
		}
	})
	return nil
}
