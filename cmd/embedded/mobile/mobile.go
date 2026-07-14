// Package mobile provides gomobile bindings for Ratatoskr.
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

// Ratatoskr — main type of the mobile binding. Created via NewRatatoskr().
type Ratatoskr struct {
	mu         sync.Mutex
	node       *ratatoskr.Obj
	nodeCfg    *config.NodeConfig
	logBridge  *logBridgeObj
	peerBridge *peerBridgeObj

	fwdMgr      *forward.ManagerObj
	runCancel   context.CancelFunc
	peerMonDone chan struct{}
	stopRun     *mobileStopObj

	localTCPs  []forward.TCPMappingObj
	localUDPs  []forward.UDPMappingObj
	remoteTCPs []forward.TCPMappingObj
	remoteUDPs []forward.UDPMappingObj

	// Options before Start()
	udpTimeout   time.Duration
	coreStopMs   int64
	multicast    bool
	socksMaxConn int
}

type mobileStopObj struct {
	done chan struct{}
	err  error
}

// NewRatatoskr creates a new Ratatoskr instance.
func NewRatatoskr() *Ratatoskr {
	lb := newLogBridge()
	return &Ratatoskr{
		logBridge:  lb,
		peerBridge: newPeerBridge(),
		udpTimeout: defaultUDPSessionTimeout,
	}
}

// // // // // // // // // //

// LoadConfigJSON parses the NodeConfig JSON and stores it for Start(). Error if the node is running
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

// SetLogCallback registers the log callback; can be called at any time
func (y *Ratatoskr) SetLogCallback(cb LogCallback) {
	y.logBridge.setCallback(cb)
}

// SetLogLevel — minimum level: "trace", "debug", "info" (default), "warn", "error"
func (y *Ratatoskr) SetLogLevel(level string) {
	y.logBridge.setLevel(level)
}

// SetPeerChangeCallback — callback on peer count change; can be called at any time
func (y *Ratatoskr) SetPeerChangeCallback(cb PeerChangeCallback) {
	y.peerBridge.setCallback(cb)
}

// SetCoreStopTimeout — max aggregate stop wait in ms; 0 = library default. Before Start().
func (y *Ratatoskr) SetCoreStopTimeout(ms int64) {
	y.mu.Lock()
	y.coreStopMs = ms
	y.mu.Unlock()
}

// SetSessionTimeout — UDP session inactivity timeout in ms; default 120000. Before Start()
func (y *Ratatoskr) SetSessionTimeout(ms int64) {
	y.mu.Lock()
	if ms > 0 {
		y.udpTimeout = time.Duration(ms) * time.Millisecond
	}
	y.mu.Unlock()
}

// SetMulticastEnabled — mDNS peer discovery on the local network. Before Start()
func (y *Ratatoskr) SetMulticastEnabled(enabled bool) {
	y.mu.Lock()
	y.multicast = enabled
	y.mu.Unlock()
}

// SetSOCKSMaxConnections — SOCKS5 connection limit; 0 = safe default. Before Start()
func (y *Ratatoskr) SetSOCKSMaxConnections(max int) {
	y.mu.Lock()
	y.socksMaxConn = max
	y.mu.Unlock()
}

// // // // // // // // // //

// AddPeer adds a peer; tcp, tls, quic, ws, wss.
// Before Start() → stored in config; while running → connect immediately
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

// RemovePeer removes a peer; before Start() → from config, while running → disconnect
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

// AddLocalTCPMapping — forward local TCP to Yggdrasil; "127.0.0.1:8080" → "[200:...]:80". Before Start()
func (y *Ratatoskr) AddLocalTCPMapping(local, remote string) error {
	m, err := parseTCPMapping(local, remote)
	if err != nil {
		return err
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil || y.stopRun != nil {
		return fmt.Errorf("cannot change mappings while running")
	}
	y.localTCPs = append(y.localTCPs, m)
	return nil
}

// AddLocalUDPMapping — forward local UDP to Yggdrasil. Before Start()
func (y *Ratatoskr) AddLocalUDPMapping(local, remote string) error {
	m, err := parseUDPMapping(local, remote)
	if err != nil {
		return err
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil || y.stopRun != nil {
		return fmt.Errorf("cannot change mappings while running")
	}
	y.localUDPs = append(y.localUDPs, m)
	return nil
}

// AddRemoteTCPMapping — expose local TCP to Yggdrasil; port → local. Before Start()
func (y *Ratatoskr) AddRemoteTCPMapping(port int, local string) error {
	m, err := parseRemoteTCPMapping(port, local)
	if err != nil {
		return err
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil || y.stopRun != nil {
		return fmt.Errorf("cannot change mappings while running")
	}
	y.remoteTCPs = append(y.remoteTCPs, m)
	return nil
}

// AddRemoteUDPMapping — expose local UDP to Yggdrasil; port → local. Before Start()
func (y *Ratatoskr) AddRemoteUDPMapping(port int, local string) error {
	m, err := parseRemoteUDPMapping(port, local)
	if err != nil {
		return err
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil || y.stopRun != nil {
		return fmt.Errorf("cannot change mappings while running")
	}
	y.remoteUDPs = append(y.remoteUDPs, m)
	return nil
}

// ClearLocalMappings clears local forwarding rules. Before Start()
func (y *Ratatoskr) ClearLocalMappings() {
	y.mu.Lock()
	if y.node == nil && y.stopRun == nil {
		y.localTCPs = nil
		y.localUDPs = nil
	}
	y.mu.Unlock()
}

// ClearRemoteMappings clears remote forwarding rules. Before Start()
func (y *Ratatoskr) ClearRemoteMappings() {
	y.mu.Lock()
	if y.node == nil && y.stopRun == nil {
		y.remoteTCPs = nil
		y.remoteUDPs = nil
	}
	y.mu.Unlock()
}

// // // // // // // // // //

// Start starts the node. socksAddr: SOCKS5 address (empty = disabled); nameserver: DNS for .ygg (empty = disabled)
func (y *Ratatoskr) Start(socksAddr, nameserver string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.node != nil {
		return fmt.Errorf("already running; call Stop() first")
	}
	if y.stopRun != nil {
		return fmt.Errorf("stop is still in progress")
	}

	nodeCfg := y.nodeCfg
	if nodeCfg == nil {
		nodeCfg = config.GenerateConfig()
		nodeCfg.AdminListen = "none"
		y.nodeCfg = nodeCfg
	}

	cfg := ratatoskr.ConfigObj{
		Config: nodeCfg,
		Logger: y.logBridge,
	}
	if y.coreStopMs > 0 {
		cfg.CloseTimeout = time.Duration(y.coreStopMs) * time.Millisecond
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
		if err = node.Core().EnableMulticast(); err != nil {
			_ = node.Close()
			return fmt.Errorf("enable multicast: %w", err)
		}
	}

	fwdMgr := forward.New(forward.ConfigObj{
		Logger:     y.logBridge,
		Node:       node.Core(),
		UDPTimeout: y.udpTimeout,
	})
	if err = fwdMgr.AddLocalTCP(y.localTCPs...); err != nil {
		_ = node.Close()
		return fmt.Errorf("configure local TCP forwarding: %w", err)
	}
	if err = fwdMgr.AddRemoteTCP(y.remoteTCPs...); err != nil {
		_ = node.Close()
		return fmt.Errorf("configure remote TCP forwarding: %w", err)
	}
	if err = fwdMgr.AddLocalUDP(y.localUDPs...); err != nil {
		_ = node.Close()
		return fmt.Errorf("configure local UDP forwarding: %w", err)
	}
	if err = fwdMgr.AddRemoteUDP(y.remoteUDPs...); err != nil {
		_ = node.Close()
		return fmt.Errorf("configure remote UDP forwarding: %w", err)
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	if err = fwdMgr.Start(runCtx); err != nil {
		runCancel()
		_ = node.Close()
		return fmt.Errorf("start forwarding: %w", err)
	}

	y.node = node
	y.fwdMgr = fwdMgr
	y.runCancel = runCancel
	y.peerMonDone = make(chan struct{})
	go y.peerMonitorLoop(runCtx, node, y.peerMonDone)

	return nil
}

// Stop stops the node and forwarding; safe if not running
func (y *Ratatoskr) Stop() error {
	y.mu.Lock()
	if y.stopRun != nil {
		run := y.stopRun
		y.mu.Unlock()
		<-run.done
		return run.err
	}
	if y.node == nil {
		y.mu.Unlock()
		return nil
	}
	run := &mobileStopObj{done: make(chan struct{})}
	y.stopRun = run
	node := y.node
	mgr := y.fwdMgr
	cancel := y.runCancel
	peerDone := y.peerMonDone
	y.mu.Unlock()

	cancel()
	_ = mgr.Close()
	<-peerDone
	err := node.Close()

	y.mu.Lock()
	y.node = nil
	y.fwdMgr = nil
	y.runCancel = nil
	y.peerMonDone = nil
	run.err = err
	y.stopRun = nil
	close(run.done)
	y.mu.Unlock()
	return err
}

// IsRunning — whether the node is running
func (y *Ratatoskr) IsRunning() bool {
	y.mu.Lock()
	running := y.node != nil
	y.mu.Unlock()
	return running
}

// // // // // // // // // //

// GetAddress — node IPv6 address; empty if not running
func (y *Ratatoskr) GetAddress() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return node.Address().String()
}

// GetSubnet — node IPv6 subnet; empty if not running
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

// GetPublicKey — Ed25519 key (hex); empty if not running
func (y *Ratatoskr) GetPublicKey() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return hex.EncodeToString(node.PublicKey())
}

// GetPeers — URI of all peers as a JSON array; "[]" if not running
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

// peerJSONObj — detailed peer information for JSON
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

// GetPeersJSON — detailed peer stats (URI, traffic, latency, uptime) as JSON
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

// RetryPeersNow initiates immediate reconnection to all disconnected peers.
// No-op if the node is not running.
func (y *Ratatoskr) RetryPeersNow() {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node != nil {
		_ = node.Core().RetryPeers()
	}
}

// TriggerPeerUpdate calls PeerChangeCallback with the current peer count.
// Useful for refreshing the UI after registering a callback while running.
// No-op if the node is not running or the callback is not set.
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

// peerMonitorLoop periodically checks peer state and notifies the callback
func (y *Ratatoskr) peerMonitorLoop(ctx context.Context, node *ratatoskr.Obj, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(defaultPeerMonitorInterval)
	defer ticker.Stop()

	var prevConnected, prevTotal int64 = -1, -1

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
