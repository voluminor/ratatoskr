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

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/forward"
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

// LoadConfigJSON разбирает NodeConfig JSON и сохраняет для Start(). Ошибка если нода запущена
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

// SetLogCallback регистрирует колбек логов; можно вызывать в любой момент
func (y *Ratatoskr) SetLogCallback(cb LogCallback) {
	y.logBridge.setCallback(cb)
}

// SetLogLevel — минимальный уровень: "trace", "debug", "info" (default), "warn", "error"
func (y *Ratatoskr) SetLogLevel(level string) {
	y.logBridge.setLevel(level)
}

// SetPeerChangeCallback — колбек при изменении числа пиров; можно вызывать в любой момент
func (y *Ratatoskr) SetPeerChangeCallback(cb PeerChangeCallback) {
	y.peerBridge.setCallback(cb)
}

// SetCoreStopTimeout — макс. ожидание остановки ядра в мс; 0 = бесконечно. До Start()
func (y *Ratatoskr) SetCoreStopTimeout(ms int64) {
	y.mu.Lock()
	y.coreStopMs = ms
	y.mu.Unlock()
}

// SetSessionTimeout — таймаут неактивности UDP-сессии в мс; default 120000. До Start()
func (y *Ratatoskr) SetSessionTimeout(ms int64) {
	y.mu.Lock()
	if ms > 0 {
		y.udpTimeout = time.Duration(ms) * time.Millisecond
		y.fwdMgr.SetTimeout(y.udpTimeout)
	}
	y.mu.Unlock()
}

// SetMulticastEnabled — mDNS-обнаружение пиров в локальной сети. До Start()
func (y *Ratatoskr) SetMulticastEnabled(enabled bool) {
	y.mu.Lock()
	y.multicast = enabled
	y.mu.Unlock()
}

// SetSOCKSMaxConnections — лимит SOCKS5-соединений; 0 = без ограничений. До Start()
func (y *Ratatoskr) SetSOCKSMaxConnections(max int) {
	y.mu.Lock()
	y.socksMaxConn = max
	y.mu.Unlock()
}

// // // // // // // // // //

// AddPeer добавляет пир; tcp, tls, quic, ws, wss.
// До Start() → в конфиг; во время работы → подключение немедленно
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

// RemovePeer удаляет пир; до Start() → из конфига, во время работы → отключение
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

// AddLocalTCPMapping — форвардинг локального TCP на Yggdrasil; "127.0.0.1:8080" → "[200:...]:80". До Start()
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

// AddLocalUDPMapping — форвардинг локального UDP на Yggdrasil. До Start()
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

// AddRemoteTCPMapping — экспозиция локального TCP в Yggdrasil; port → local. До Start()
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

// AddRemoteUDPMapping — экспозиция локального UDP в Yggdrasil; port → local. До Start()
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

// ClearLocalMappings сбрасывает локальные правила форвардинга. До Start()
func (y *Ratatoskr) ClearLocalMappings() {
	y.mu.Lock()
	y.fwdMgr.ClearLocal()
	y.mu.Unlock()
}

// ClearRemoteMappings сбрасывает удалённые правила форвардинга. До Start()
func (y *Ratatoskr) ClearRemoteMappings() {
	y.mu.Lock()
	y.fwdMgr.ClearRemote()
	y.mu.Unlock()
}

// // // // // // // // // //

// Start запускает ноду. socksAddr: адрес SOCKS5 (пусто = отключён); nameserver: DNS для .ygg (пусто = отключён)
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

// Stop останавливает ноду и форвардинг; безопасен если не запущена
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

// IsRunning — запущена ли нода
func (y *Ratatoskr) IsRunning() bool {
	y.mu.Lock()
	running := y.node != nil
	y.mu.Unlock()
	return running
}

// // // // // // // // // //

// GetAddress — IPv6-адрес ноды; пусто если не запущена
func (y *Ratatoskr) GetAddress() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return node.Address().String()
}

// GetSubnet — IPv6-подсеть ноды; пусто если не запущена
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

// GetPublicKey — Ed25519 ключ (hex); пусто если не запущена
func (y *Ratatoskr) GetPublicKey() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return hex.EncodeToString(node.PublicKey())
}

// GetPeers — URI всех пиров как JSON-массив; "[]" если не запущена
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

// peerJSONObj — детальная информация о пире для JSON
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

// GetPeersJSON — детальная статистика пиров (URI, трафик, латентность, аптайм) как JSON
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
