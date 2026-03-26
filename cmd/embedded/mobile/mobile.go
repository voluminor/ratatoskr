// Пакет mobile предоставляет gomobile-биндинги для Ratatoskr.
//
// Android: gomobile bind -target=android -o ratatoskr.aar .
// iOS:     gomobile bind -target=ios -o Ratatoskr.xcframework .
package mobile

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	ratatoskr "github.com/yggdrasil-network/ratatoskr"
	"github.com/yggdrasil-network/ratatoskr/mod/forward"
)

// // // // // // // // // //

const (
	defaultUDPSessionTimeout   = 120 * time.Second
	defaultPeerMonitorInterval = 5 * time.Second
)

// //

// Ratatoskr — основной тип мобильного биндинга. Создаётся через NewRatatoskr().
type Ratatoskr struct {
	mu         sync.Mutex
	node       *ratatoskr.Obj
	nodeCfg    *config.NodeConfig
	logBridge  *logBridgeObj
	peerBridge *peerBridgeObj

	// fwdMgr создаётся в NewYggstack(), запускается в Start()
	fwdMgr    *forward.ManagerObj
	fwdCancel context.CancelFunc
	peerMonWg sync.WaitGroup

	// Опции до Start()
	udpTimeout   time.Duration
	coreStopMs   int64
	multicast    bool
	socksMaxConn int
}

// NewRatatoskr создаёт новый экземпляр Ratatoskr.
func NewRatatoskr() *Ratatoskr {
	lb := newLogBridge()
	return &Ratatoskr{
		logBridge:  lb,
		peerBridge: newPeerBridge(),
		udpTimeout: defaultUDPSessionTimeout,
		fwdMgr:     forward.New(lb, defaultUDPSessionTimeout),
	}
}

// // // // // // // // // //

// LoadConfigJSON разбирает JSON-строку конфигурации и сохраняет её для Start().
// Формат — NodeConfig Yggdrasil (см. GenerateConfig). Возвращает ошибку если нода запущена или JSON невалидный.
func (y *Ratatoskr) LoadConfigJSON(jsonStr string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil {
		return fmt.Errorf("cannot change config while running; call Stop() first")
	}
	base := config.GenerateConfig()
	base.AdminListen = "none"
	if err := json.Unmarshal([]byte(jsonStr), base); err != nil {
		return fmt.Errorf("config parse: %w", err)
	}
	base.AdminListen = "none"
	y.nodeCfg = base
	return nil
}

// SetLogCallback регистрирует колбек для вывода логов. Можно вызывать в любой момент.
func (y *Ratatoskr) SetLogCallback(cb LogCallback) {
	y.logBridge.setCallback(cb)
}

// SetLogLevel задаёт минимальный уровень логирования. Допустимые значения: "trace", "debug", "info" (по умолчанию), "warn", "error".
func (y *Ratatoskr) SetLogLevel(level string) {
	y.logBridge.setLevel(level)
}

// SetPeerChangeCallback регистрирует колбек на изменение количества подключённых пиров. Можно вызывать в любой момент.
func (y *Ratatoskr) SetPeerChangeCallback(cb PeerChangeCallback) {
	y.peerBridge.setCallback(cb)
}

// SetCoreStopTimeout задаёт максимальное время ожидания остановки ядра в мс.
// 0 — ждать бесконечно (по умолчанию). Вызывать до Start().
func (y *Ratatoskr) SetCoreStopTimeout(ms int64) {
	y.mu.Lock()
	y.coreStopMs = ms
	y.mu.Unlock()
}

// SetSessionTimeout задаёт таймаут неактивности UDP-сессии в мс.
// По истечении без трафика сессия закрывается. По умолчанию: 120000 (120с). Вызывать до Start().
func (y *Ratatoskr) SetSessionTimeout(ms int64) {
	y.mu.Lock()
	if ms > 0 {
		y.udpTimeout = time.Duration(ms) * time.Millisecond
		y.fwdMgr.SetTimeout(y.udpTimeout)
	}
	y.mu.Unlock()
}

// SetMulticastEnabled включает или отключает mDNS-обнаружение пиров в локальной сети. Вызывать до Start().
func (y *Ratatoskr) SetMulticastEnabled(enabled bool) {
	y.mu.Lock()
	y.multicast = enabled
	y.mu.Unlock()
}

// SetSOCKSMaxConnections задаёт максимальное количество одновременных SOCKS5-соединений.
// 0 — без ограничений (по умолчанию). Вызывать до Start().
func (y *Ratatoskr) SetSOCKSMaxConnections(max int) {
	y.mu.Lock()
	y.socksMaxConn = max
	y.mu.Unlock()
}

// // // // // // // // // //

// AddPeer добавляет пир по URI. Поддерживаемые схемы: tcp, tls, quic, ws, wss.
// До Start() — сохраняется в конфиг. Во время работы — подключается немедленно.
func (y *Ratatoskr) AddPeer(uri string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil {
		return y.node.AddPeer(uri)
	}
	if y.nodeCfg == nil {
		y.nodeCfg = config.GenerateConfig()
		y.nodeCfg.AdminListen = "none"
	}
	for _, p := range y.nodeCfg.Peers {
		if p == uri {
			return nil
		}
	}
	y.nodeCfg.Peers = append(y.nodeCfg.Peers, uri)
	return nil
}

// RemovePeer удаляет пир по URI.
// До Start() — удаляется из конфига. Во время работы — отключается немедленно.
func (y *Ratatoskr) RemovePeer(uri string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil {
		return y.node.RemovePeer(uri)
	}
	if y.nodeCfg == nil {
		return nil
	}
	filtered := y.nodeCfg.Peers[:0]
	for _, p := range y.nodeCfg.Peers {
		if p != uri {
			filtered = append(filtered, p)
		}
	}
	y.nodeCfg.Peers = filtered
	return nil
}

// // // // // // // // // //

// AddLocalTCPMapping добавляет правило форвардинга локального TCP-порта на Yggdrasil-адрес.
// local: адрес прослушивания, например "127.0.0.1:8080"; remote: назначение в Yggdrasil, например "[200:1234::1]:80".
// Вызывать до Start(); вступает в силу при следующем Start().
func (y *Ratatoskr) AddLocalTCPMapping(local, remote string) error {
	m, err := parseTCPMapping(local, remote)
	if err != nil {
		return err
	}
	y.mu.Lock()
	y.fwdMgr.AddLocalTCP(m)
	y.mu.Unlock()
	return nil
}

// AddLocalUDPMapping добавляет правило форвардинга локального UDP-порта на Yggdrasil-адрес.
// local: адрес прослушивания, например "127.0.0.1:5353"; remote: назначение в Yggdrasil, например "[200:1234::1]:53".
// Вызывать до Start(); вступает в силу при следующем Start().
func (y *Ratatoskr) AddLocalUDPMapping(local, remote string) error {
	m, err := parseUDPMapping(local, remote)
	if err != nil {
		return err
	}
	y.mu.Lock()
	y.fwdMgr.AddLocalUDP(m)
	y.mu.Unlock()
	return nil
}

// AddRemoteTCPMapping открывает локальный TCP-сервис в сети Yggdrasil.
// port: порт прослушивания на стороне Yggdrasil (1-65535); local: локальный сервис, например "127.0.0.1:80".
// Вызывать до Start(); вступает в силу при следующем Start().
func (y *Ratatoskr) AddRemoteTCPMapping(port int, local string) error {
	m, err := parseRemoteTCPMapping(port, local)
	if err != nil {
		return err
	}
	y.mu.Lock()
	y.fwdMgr.AddRemoteTCP(m)
	y.mu.Unlock()
	return nil
}

// AddRemoteUDPMapping открывает локальный UDP-сервис в сети Yggdrasil.
// port: порт прослушивания на стороне Yggdrasil (1-65535); local: локальный сервис, например "127.0.0.1:53".
// Вызывать до Start(); вступает в силу при следующем Start().
func (y *Ratatoskr) AddRemoteUDPMapping(port int, local string) error {
	m, err := parseRemoteUDPMapping(port, local)
	if err != nil {
		return err
	}
	y.mu.Lock()
	y.fwdMgr.AddRemoteUDP(m)
	y.mu.Unlock()
	return nil
}

// ClearLocalMappings удаляет все ожидающие локальные правила TCP/UDP-форвардинга.
// Вызывать до Start(); во время работы не имеет эффекта.
func (y *Ratatoskr) ClearLocalMappings() {
	y.mu.Lock()
	y.fwdMgr.ClearLocal()
	y.mu.Unlock()
}

// ClearRemoteMappings удаляет все ожидающие удалённые правила TCP/UDP-форвардинга.
// Вызывать до Start(); во время работы не имеет эффекта.
func (y *Ratatoskr) ClearRemoteMappings() {
	y.mu.Lock()
	y.fwdMgr.ClearRemote()
	y.mu.Unlock()
}

// // // // // // // // // //

// Start запускает ноду Yggdrasil с SOCKS5-прокси на socksAddr и опциональным DNS-сервером.
// Возвращает ошибку если нода уже запущена или запуск не удался.
//
// socksAddr: адрес SOCKS5-прокси (TCP или UNIX-сокет), например "127.0.0.1:1080". Пустая строка — SOCKS5 отключён.
// nameserver: DNS-сервер в сети Yggdrasil для доменов .ygg. Пустая строка — внешнее .ygg DNS отключено.
func (y *Ratatoskr) Start(socksAddr, nameserver string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil {
		return fmt.Errorf("already running; call Stop() first")
	}

	nodeCfg := y.nodeCfg
	if nodeCfg == nil {
		nodeCfg = config.GenerateConfig()
		nodeCfg.AdminListen = "none"
	}

	cfg := ratatoskr.ConfigObj{
		Config: nodeCfg,
		Logger: y.logBridge,
	}
	if y.coreStopMs > 0 {
		cfg.CoreStopTimeout = time.Duration(y.coreStopMs) * time.Millisecond
	}

	node, err := ratatoskr.New(cfg)
	if err != nil {
		return fmt.Errorf("start node: %w", err)
	}

	if socksAddr != "" {
		if err = node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
			Addr:           socksAddr,
			Nameserver:     nameserver,
			MaxConnections: y.socksMaxConn,
		}); err != nil {
			_ = node.Close()
			return fmt.Errorf("enable SOCKS: %w", err)
		}
	}

	if y.multicast {
		if err = node.EnableMulticast(nil); err != nil {
			_ = node.Close()
			return fmt.Errorf("enable multicast: %w", err)
		}
	}

	y.node = node

	fwdCtx, fwdCancel := context.WithCancel(context.Background())
	y.fwdCancel = fwdCancel
	y.fwdMgr.Start(fwdCtx, node)

	y.peerMonWg.Add(1)
	go y.peerMonitorLoop(fwdCtx)

	return nil
}

// Stop останавливает ноду и весь форвардинг портов. Безопасен при вызове если нода не запущена.
func (y *Ratatoskr) Stop() error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node == nil {
		return nil
	}
	y.fwdCancel()
	err := y.node.Close()
	y.fwdMgr.Wait()
	y.peerMonWg.Wait()
	y.node = nil
	y.fwdCancel = nil
	return err
}

// IsRunning возвращает true если нода запущена.
func (y *Ratatoskr) IsRunning() bool {
	y.mu.Lock()
	running := y.node != nil
	y.mu.Unlock()
	return running
}

// // // // // // // // // //

// GetAddress возвращает IPv6-адрес ноды в сети Yggdrasil, например "200:1234::1".
// Возвращает пустую строку если нода не запущена.
func (y *Ratatoskr) GetAddress() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return node.Address().String()
}

// GetSubnet возвращает IPv6-подсеть ноды в сети Yggdrasil, например "300:1234::/64".
// Возвращает пустую строку если нода не запущена.
func (y *Ratatoskr) GetSubnet() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	s := node.Subnet()
	return s.String()
}

// GetPublicKey возвращает Ed25519 публичный ключ ноды в виде hex-строки.
// Возвращает пустую строку если нода не запущена.
func (y *Ratatoskr) GetPublicKey() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return hex.EncodeToString(node.PublicKey())
}

// GetPeers возвращает список URI настроенных пиров в виде JSON-массива.
// Включает как подключённые, так и отключённые пиры. Возвращает "[]" если нода не запущена.
func (y *Ratatoskr) GetPeers() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return "[]"
	}
	peers := node.GetPeers()
	uris := make([]string, 0, len(peers))
	for _, p := range peers {
		uris = append(uris, p.URI)
	}
	b, err := json.Marshal(uris)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// peerJSONObj — структура для сериализации информации о пире
type peerJSONObj struct {
	URI           string `json:"uri"`
	Up            bool   `json:"up"`
	Inbound       bool   `json:"inbound"`
	Key           string `json:"key"`
	LatencyMs     int64  `json:"latency_ms"`
	Cost          uint64 `json:"cost"`
	RXBytes       uint64 `json:"rx_bytes"`
	TXBytes       uint64 `json:"tx_bytes"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	LastError     string `json:"last_error,omitempty"`
}

// GetPeersJSON возвращает детальную статистику по пирам в виде JSON-массива.
// Каждая запись содержит URI, состояние соединения, счётчики трафика, задержку и аптайм.
// Возвращает "[]" если нода не запущена или при ошибке.
func (y *Ratatoskr) GetPeersJSON() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return "[]"
	}
	peers := node.GetPeers()
	result := make([]peerJSONObj, 0, len(peers))
	for _, p := range peers {
		entry := peerJSONObj{
			URI:           p.URI,
			Up:            p.Up,
			Inbound:       p.Inbound,
			Key:           hex.EncodeToString(p.Key),
			LatencyMs:     p.Latency.Milliseconds(),
			Cost:          p.Cost,
			RXBytes:       p.RXBytes,
			TXBytes:       p.TXBytes,
			UptimeSeconds: int64(p.Uptime.Seconds()),
		}
		if p.LastError != nil {
			entry.LastError = p.LastError.Error()
		}
		result = append(result, entry)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// RetryPeersNow инициирует немедленное переподключение ко всем отключённым пирам.
// Не выполняет действий если нода не запущена.
func (y *Ratatoskr) RetryPeersNow() {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node != nil {
		node.RetryPeers()
	}
}

// TriggerPeerUpdate вызывает PeerChangeCallback с текущим количеством пиров.
// Полезно для обновления UI после регистрации колбека во время работы.
// Не выполняет действий если нода не запущена или колбек не установлен.
func (y *Ratatoskr) TriggerPeerUpdate() {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return
	}
	var connected, total int64
	for _, p := range node.GetPeers() {
		total++
		if p.Up {
			connected++
		}
	}
	y.peerBridge.OnPeerCountChanged(connected, total)
}

// // // // // // // // // //

// peerMonitorLoop периодически проверяет состояние пиров и уведомляет callback
func (y *Ratatoskr) peerMonitorLoop(ctx context.Context) {
	defer y.peerMonWg.Done()
	ticker := time.NewTicker(defaultPeerMonitorInterval)
	defer ticker.Stop()

	var prevConnected, prevTotal int64 = -1, -1

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			y.mu.Lock()
			node := y.node
			y.mu.Unlock()
			if node == nil {
				return
			}
			var connected, total int64
			for _, p := range node.GetPeers() {
				total++
				if p.Up {
					connected++
				}
			}
			if connected != prevConnected || total != prevTotal {
				prevConnected = connected
				prevTotal = total
				y.peerBridge.OnPeerCountChanged(connected, total)
			}
		}
	}
}
