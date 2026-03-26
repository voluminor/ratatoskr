package peermgr

import (
	"context"
	"fmt"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
)

const defaultProbeTimeout = 10 * time.Second

// // // // // // // // // //

// ConfigObj — параметры менеджера пиров
type ConfigObj struct {
	// Список URI-кандидатов ("tls://host:port", "tcp://...", "quic://...", etc.)
	Peers []string

	// Таймаут ожидания подключения пира при пробинге. 0 → 30s.
	// Игнорируется при MaxPerProto == -1.
	ProbeTimeout time.Duration

	// Интервал автоматической перепроверки. 0 → только при запуске.
	RefreshInterval time.Duration

	// Количество лучших пиров на протокол:
	//   0 или 1  — один лучший на протокол (default)
	//   N > 1    — топ-N на протокол
	//  -1        — пассивный режим: добавить всех без выбора;
	//              при RefreshInterval > 0 список переподключается целиком
	MaxPerProto int

	// Логгер; обязателен
	Logger yggcore.Logger

	// OnNoReachablePeers вызывается после пробинга, если ни один пир не ответил.
	// Вызывается из горутины менеджера; не должен блокировать.
	OnNoReachablePeers func()
}

// // // // // // // // // //

// Obj — менеджер пиров
type Obj struct {
	cfg    ConfigObj
	node   core.Interface
	ctx    context.Context    // активен пока менеджер работает
	cancel context.CancelFunc //
	active []string           // текущий выбранный список URI
	mu     sync.Mutex
	wg     sync.WaitGroup
}

// New создаёт менеджер; пиры не добавляются до Start()
func New(node core.Interface, cfg ConfigObj) (*Obj, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("peermgr: logger is required")
	}
	if cfg.MaxPerProto == 0 {
		cfg.MaxPerProto = 1
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = defaultProbeTimeout
	}
	return &Obj{cfg: cfg, node: node}, nil
}

// // // // // // // // // //

// Start запускает менеджер: первичный прогон и тикер работают асинхронно.
// Повторный Start без Stop возвращает ошибку.
func (m *Obj) Start() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return fmt.Errorf("peermgr: already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.mu.Unlock()

	m.cfg.Logger.Infof("[peermgr] starting, %d candidates, MaxPerProto=%d", len(m.cfg.Peers), m.cfg.MaxPerProto)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx)
	}()
	return nil
}

// Stop отменяет контекст, ждёт завершения горутин и удаляет все управляемые пиры.
// Безопасен при повторном вызове.
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

// Active возвращает копию текущего списка активных пиров
func (m *Obj) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.active))
	copy(out, m.active)
	return out
}

// Optimize запускает внеплановую перепроверку пиров.
// Блокирует до завершения. Возвращает ошибку если менеджер не запущен.
func (m *Obj) Optimize() error {
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("peermgr: not running")
	}
	return m.optimize(ctx)
}

// // // // // // // // // //

func (m *Obj) run(ctx context.Context) {
	_ = m.optimize(ctx)

	if m.cfg.RefreshInterval <= 0 {
		return
	}
	ticker := time.NewTicker(m.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = m.optimize(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (m *Obj) optimize(ctx context.Context) error {
	if m.cfg.MaxPerProto == -1 {
		return m.optimizePassive()
	}
	return m.optimizeActive(ctx)
}

// optimizeActive: добавить всех кандидатов → ждать ProbeTimeout →
// выбрать лучших по протоколу → убрать остальных.
func (m *Obj) optimizeActive(ctx context.Context) error {
	for _, uri := range m.cfg.Peers {
		if err := m.node.AddPeer(uri); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", uri, err)
		}
	}

	select {
	case <-time.After(m.cfg.ProbeTimeout):
	case <-ctx.Done():
		return ctx.Err()
	}

	results := buildResults(m.cfg.Peers, m.node.GetPeers())
	selected := selectBest(results, m.cfg.MaxPerProto)

	selectedSet := make(map[string]bool, len(selected))
	for _, uri := range selected {
		selectedSet[uri] = true
	}

	for _, uri := range m.cfg.Peers {
		if !selectedSet[uri] {
			if err := m.node.RemovePeer(uri); err != nil {
				m.cfg.Logger.Debugf("[peermgr] RemovePeer %s: %v", uri, err)
			}
		}
	}

	m.mu.Lock()
	m.active = selected
	m.mu.Unlock()

	if len(selected) == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		if m.cfg.OnNoReachablePeers != nil {
			m.cfg.OnNoReachablePeers()
		}
	} else {
		m.cfg.Logger.Infof("[peermgr] active peers: %v", selected)
	}
	return nil
}

// optimizePassive: режим -1 — переподключить весь список целиком.
// При первом вызове RemovePeer завершается ошибкой (пир ещё не добавлен) — это нормально.
func (m *Obj) optimizePassive() error {
	for _, uri := range m.cfg.Peers {
		_ = m.node.RemovePeer(uri)
		if err := m.node.AddPeer(uri); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", uri, err)
		}
	}

	m.mu.Lock()
	m.active = append([]string(nil), m.cfg.Peers...)
	m.mu.Unlock()

	m.cfg.Logger.Infof("[peermgr] passive mode, added %d peers", len(m.cfg.Peers))
	return nil
}
