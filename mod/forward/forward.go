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

// TCPMappingObj — маппинг TCP: локальный адрес ↔ удалённый
type TCPMappingObj struct {
	Listen *net.TCPAddr
	Mapped *net.TCPAddr
}

// UDPMappingObj — маппинг UDP: локальный адрес ↔ удалённый
type UDPMappingObj struct {
	Listen *net.UDPAddr
	Mapped *net.UDPAddr
}

// //

// DefaultTCPCloseTimeout — ожидание второй стороны TCP после закрытия первой
const DefaultTCPCloseTimeout = 30 * time.Second

// ManagerObj — менеджер правил форвардинга: New → Add* → Start
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

// New создаёт менеджер; sessionTimeout — неактивность UDP до закрытия сессии
func New(log yggcore.Logger, sessionTimeout time.Duration) *ManagerObj {
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

// SetTimeout обновляет таймаут неактивности UDP-сессий. До Start()
func (m *ManagerObj) SetTimeout(d time.Duration) {
	m.timeout = d
}

// SetTCPCloseTimeout — ожидание второй стороны TCP при разрыве. До Start()
func (m *ManagerObj) SetTCPCloseTimeout(d time.Duration) {
	m.tcpCloseTimeout = d
}

// SetMaxUDPSessions — лимит UDP-сессий на маппинг; 0 = без ограничений. До Start()
func (m *ManagerObj) SetMaxUDPSessions(n int) {
	m.maxUDPSessions = n
}

// ClearLocal сбрасывает локальные маппинги. До Start()
func (m *ManagerObj) ClearLocal() {
	m.localTCPs = nil
	m.localUDPs = nil
}

// ClearRemote сбрасывает удалённые маппинги. До Start()
func (m *ManagerObj) ClearRemote() {
	m.remoteTCPs = nil
	m.remoteUDPs = nil
}

// //

// Start запускает горутины для всех маппингов; вызывается один раз
func (m *ManagerObj) Start(ctx context.Context, node core.Interface) {
	m.node = node
	m.startLocalTCP(ctx)
	m.startRemoteTCP(ctx)
	m.startLocalUDP(ctx)
	m.startRemoteUDP(ctx)
}

// Wait блокирует до завершения всех горутин
func (m *ManagerObj) Wait() {
	m.wg.Wait()
}
