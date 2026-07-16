// Package mobile exposes Ratatoskr through gomobile-compatible types.
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

// RatatoskrObj owns one mobile Ratatoskr node and its forwarding configuration.
type RatatoskrObj struct {
	mu         sync.Mutex
	node       *ratatoskr.Obj
	nodeCfg    *config.NodeConfig
	logBridge  *logBridgeObj
	peerBridge *peerBridgeObj

	fwdMgr      *forward.Obj
	runCancel   context.CancelFunc
	peerMonDone chan struct{}
	stopRun     *mobileStopObj

	localTCPs  []forward.TCPMappingObj
	localUDPs  []forward.UDPMappingObj
	remoteTCPs []forward.TCPMappingObj
	remoteUDPs []forward.UDPMappingObj

	udpTimeout   time.Duration
	coreStopMs   int64
	multicast    bool
	socksMaxConn int
}

type mobileStopObj struct {
	done chan struct{}
	err  error
}

// NewRatatoskr creates a stopped mobile node.
func NewRatatoskr() *RatatoskrObj {
	lb := newLogBridge()
	return &RatatoskrObj{
		logBridge:  lb,
		peerBridge: newPeerBridge(),
		udpTimeout: defaultUDPSessionTimeout,
	}
}

// // // // // // // // // //

// LoadConfigJSON replaces the Yggdrasil configuration while the node is stopped.
func (y *RatatoskrObj) LoadConfigJSON(jsonStr string) error {
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

// SetLogCallback replaces the callback that receives log messages.
func (y *RatatoskrObj) SetLogCallback(cb LogCallbackInterface) {
	y.logBridge.setCallback(cb)
}

// SetLogLevel sets the minimum callback level to trace, debug, info, warn, or error.
func (y *RatatoskrObj) SetLogLevel(level string) {
	y.logBridge.setLevel(level)
}

// SetPeerChangeCallback replaces the callback that receives peer-count changes.
func (y *RatatoskrObj) SetPeerChangeCallback(cb PeerChangeCallbackInterface) {
	y.peerBridge.setCallback(cb)
}

// SetCoreStopTimeout sets the aggregate node shutdown budget in milliseconds.
// Zero uses the library default. Call it before Start.
func (y *RatatoskrObj) SetCoreStopTimeout(ms int64) {
	y.mu.Lock()
	y.coreStopMs = ms
	y.mu.Unlock()
}

// SetSessionTimeout sets UDP session inactivity in milliseconds. Non-positive
// values leave the current value unchanged. Call it before Start.
func (y *RatatoskrObj) SetSessionTimeout(ms int64) {
	y.mu.Lock()
	if ms > 0 {
		y.udpTimeout = time.Duration(ms) * time.Millisecond
	}
	y.mu.Unlock()
}

// SetMulticastEnabled configures local multicast peer discovery before Start.
func (y *RatatoskrObj) SetMulticastEnabled(enabled bool) {
	y.mu.Lock()
	y.multicast = enabled
	y.mu.Unlock()
}

// SetSOCKSMaxConnections configures the SOCKS5 connection limit before Start.
func (y *RatatoskrObj) SetSOCKSMaxConnections(max int) {
	y.mu.Lock()
	y.socksMaxConn = max
	y.mu.Unlock()
}

// // // // // // // // // //

// AddPeer stores a peer while stopped or connects it immediately while running.
func (y *RatatoskrObj) AddPeer(uri string) error {
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

// RemovePeer removes a stored peer or disconnects it while running.
func (y *RatatoskrObj) RemovePeer(uri string) error {
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

// AddLocalTCPMapping adds a local TCP listener mapped to Yggdrasil. Call it
// before Start.
func (y *RatatoskrObj) AddLocalTCPMapping(local, remote string) error {
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

// AddLocalUDPMapping adds a local UDP listener mapped to Yggdrasil. Call it
// before Start.
func (y *RatatoskrObj) AddLocalUDPMapping(local, remote string) error {
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

// AddRemoteTCPMapping exposes a local TCP service on a Yggdrasil port. Call it
// before Start.
func (y *RatatoskrObj) AddRemoteTCPMapping(port int, local string) error {
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

// AddRemoteUDPMapping exposes a local UDP service on a Yggdrasil port. Call it
// before Start.
func (y *RatatoskrObj) AddRemoteUDPMapping(port int, local string) error {
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

// ClearLocalMappings clears local forwarding rules while stopped.
func (y *RatatoskrObj) ClearLocalMappings() {
	y.mu.Lock()
	if y.node == nil && y.stopRun == nil {
		y.localTCPs = nil
		y.localUDPs = nil
	}
	y.mu.Unlock()
}

// ClearRemoteMappings clears remote forwarding rules while stopped.
func (y *RatatoskrObj) ClearRemoteMappings() {
	y.mu.Lock()
	if y.node == nil && y.stopRun == nil {
		y.remoteTCPs = nil
		y.remoteUDPs = nil
	}
	y.mu.Unlock()
}

// // // // // // // // // //

// Start starts the node, optional SOCKS5 listener, and configured mappings.
// Empty socksAddr disables SOCKS5; empty nameserver disables .ygg DNS.
func (y *RatatoskrObj) Start(socksAddr, nameserver string) error {
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

	runCtx, runCancel := context.WithCancel(context.Background())
	fwdMgr, err := forward.New(forward.ConfigObj{
		Logger:     y.logBridge,
		Node:       node.Core(),
		UDPTimeout: y.udpTimeout,
		LocalTCP:   y.localTCPs,
		RemoteTCP:  y.remoteTCPs,
		LocalUDP:   y.localUDPs,
		RemoteUDP:  y.remoteUDPs,
	})
	if err != nil {
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

// Stop closes forwarding and the node. It is safe to call while stopped.
func (y *RatatoskrObj) Stop() error {
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

// IsRunning reports whether the node is running.
func (y *RatatoskrObj) IsRunning() bool {
	y.mu.Lock()
	running := y.node != nil
	y.mu.Unlock()
	return running
}

// // // // // // // // // //

// GetAddress returns the node IPv6 address, or an empty string while stopped.
func (y *RatatoskrObj) GetAddress() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return node.Address().String()
}

// GetSubnet returns the node IPv6 subnet, or an empty string while stopped.
func (y *RatatoskrObj) GetSubnet() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	s := node.Subnet()
	return s.String()
}

// GetPublicKey returns the hexadecimal Ed25519 public key, or an empty string
// while stopped.
func (y *RatatoskrObj) GetPublicKey() string {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node == nil {
		return ""
	}
	return hex.EncodeToString(node.PublicKey())
}

// GetPeers returns peer URIs as a JSON array, or "[]" while stopped.
func (y *RatatoskrObj) GetPeers() string {
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

// GetPeersJSON returns detailed peer state as JSON, or "[]" while stopped.
func (y *RatatoskrObj) GetPeersJSON() string {
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

// RetryPeersNow requests immediate reconnection while running.
func (y *RatatoskrObj) RetryPeersNow() {
	y.mu.Lock()
	node := y.node
	y.mu.Unlock()
	if node != nil {
		_ = node.Core().RetryPeers()
	}
}

// TriggerPeerUpdate emits the current peer counts while running.
func (y *RatatoskrObj) TriggerPeerUpdate() {
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

func (y *RatatoskrObj) peerMonitorLoop(ctx context.Context, node *ratatoskr.Obj, done chan<- struct{}) {
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
