package peermgr

import (
	"context"
	"fmt"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
)

// // // // // // // // // //

const defaultProbeTimeout = 10 * time.Second

// //

// ConfigObj — параметры менеджера пиров
type ConfigObj struct {
	// Список URI-кандидатов ("tls://host:port", "tcp://...", "quic://...", etc.)
	Peers []string

	// Таймаут ожидания подключения пира при пробинге. 0 → 10s.
	// Применяется к каждому батчу отдельно. Игнорируется при MaxPerProto == -1.
	ProbeTimeout time.Duration

	// Интервал автоматической перепроверки. 0 → только при запуске.
	RefreshInterval time.Duration

	// Количество лучших пиров на протокол:
	//   0 или 1  — один лучший на протокол (default)
	//   N > 1    — топ-N на протокол
	//  -1        — пассивный режим: добавить всех без выбора;
	//              при RefreshInterval > 0 список переподключается целиком
	MaxPerProto int

	// Размер батча при пробинге:
	//   0 или 1 — все кандидаты добавляются одним батчем (default)
	//   N >= 2  — скользящее окно: N кандидатов за раз, гонка на выбывание
	BatchSize int

	// Логгер; обязателен
	Logger yggcore.Logger

	// OnNoReachablePeers вызывается после пробинга, если ни один пир не ответил.
	// Вызывается из горутины менеджера; не должен блокировать.
	OnNoReachablePeers func()
}

// // // // // // // // // //

// Obj — менеджер пиров
type Obj struct {
	cfg        ConfigObj
	node       core.Interface
	peers      []peerEntryObj
	ctx        context.Context
	cancel     context.CancelFunc
	active     []string
	mu         sync.Mutex
	optimizeMu sync.Mutex
	wg         sync.WaitGroup
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

	peers, errs := ValidatePeers(cfg.Peers)
	for _, err := range errs {
		cfg.Logger.Warnf("[peermgr] %v", err)
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("peermgr: no valid peers after validation")
	}

	return &Obj{cfg: cfg, node: node, peers: peers}, nil
}

// // // // // // // // // //

// Start запускает менеджер асинхронно; повторный вызов без Stop — ошибка
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

	m.cfg.Logger.Infof("[peermgr] starting, %d candidates, MaxPerProto=%d, BatchSize=%d",
		len(m.peers), m.cfg.MaxPerProto, m.cfg.BatchSize)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx)
	}()
	return nil
}

// Stop отменяет контекст, удаляет управляемые пиры; безопасен для повторного вызова
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

// Active — копия текущего списка активных пиров
func (m *Obj) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.active))
	copy(out, m.active)
	return out
}

// Optimize — внеплановая перепроверка; блокирует до завершения
func (m *Obj) Optimize() error {
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("peermgr: not running")
	}
	return m.optimizeLocked(ctx)
}
