package forward

import (
	"context"
	"net"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/core"
)

// // // // // // // // // //

// TCPMappingObj — TCP mapping: local address ↔ remote
type TCPMappingObj struct {
	Listen *net.TCPAddr
	Mapped *net.TCPAddr
}

// UDPMappingObj — UDP mapping: local address ↔ remote
type UDPMappingObj struct {
	Listen *net.UDPAddr
	Mapped *net.UDPAddr
}

// //

// DefaultTCPCloseTimeout — wait for the TCP peer after one side closes
const DefaultTCPCloseTimeout = 30 * time.Second

// ManagerObj — forwarding rule manager: New → Add* → Start
type ManagerObj struct {
	log             yggcore.Logger
	node            core.Interface
	timeout         time.Duration
	tcpCloseTimeout time.Duration
	maxUDPSessions  int
	wg              sync.WaitGroup

	localTCPs  []TCPMappingObj
	remoteTCPs []TCPMappingObj
	localUDPs  []UDPMappingObj
	remoteUDPs []UDPMappingObj
}

// New creates a manager; sessionTimeout — UDP inactivity before closing a session.
// Panics if sessionTimeout <= 0
func New(log yggcore.Logger, sessionTimeout time.Duration) *ManagerObj {
	if sessionTimeout <= 0 {
		panic("forward: sessionTimeout must be > 0")
	}
	return &ManagerObj{
		log:             log,
		timeout:         sessionTimeout,
		tcpCloseTimeout: DefaultTCPCloseTimeout,
	}
}

// //

func (m *ManagerObj) AddLocalTCP(mappings ...TCPMappingObj) {
	m.localTCPs = append(m.localTCPs, mappings...)
}

func (m *ManagerObj) AddRemoteTCP(mappings ...TCPMappingObj) {
	m.remoteTCPs = append(m.remoteTCPs, mappings...)
}

func (m *ManagerObj) AddLocalUDP(mappings ...UDPMappingObj) {
	m.localUDPs = append(m.localUDPs, mappings...)
}

func (m *ManagerObj) AddRemoteUDP(mappings ...UDPMappingObj) {
	m.remoteUDPs = append(m.remoteUDPs, mappings...)
}

// SetTimeout updates the UDP session inactivity timeout. Before Start()
func (m *ManagerObj) SetTimeout(d time.Duration) {
	m.timeout = d
}

// SetTCPCloseTimeout — wait for the TCP peer on disconnect. Before Start()
func (m *ManagerObj) SetTCPCloseTimeout(d time.Duration) {
	m.tcpCloseTimeout = d
}

// SetMaxUDPSessions — UDP session limit per mapping; 0 = unlimited. Before Start()
func (m *ManagerObj) SetMaxUDPSessions(n int) {
	m.maxUDPSessions = n
}

// ClearLocal clears local mappings. Before Start()
func (m *ManagerObj) ClearLocal() {
	m.localTCPs = nil
	m.localUDPs = nil
}

// ClearRemote clears remote mappings. Before Start()
func (m *ManagerObj) ClearRemote() {
	m.remoteTCPs = nil
	m.remoteUDPs = nil
}

// //

// Start launches goroutines for all mappings; called once
func (m *ManagerObj) Start(ctx context.Context, node core.Interface) {
	m.node = node
	m.startLocalTCP(ctx)
	m.startRemoteTCP(ctx)
	m.startLocalUDP(ctx)
	m.startRemoteUDP(ctx)
}

// Wait blocks until all goroutines finish
func (m *ManagerObj) Wait() {
	m.wg.Wait()
}
