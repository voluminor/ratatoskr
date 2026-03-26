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

// DefaultTCPCloseTimeout — время ожидания второй стороны TCP-соединения после закрытия первой.
const DefaultTCPCloseTimeout = 30 * time.Second

// ManagerObj запускает и останавливает все правила форвардинга.
// Создаётся через New; маппинги добавляются через Add*; запускается через Start.
type ManagerObj struct {
	log             yggcore.Logger
	node            core.Interface
	timeout         time.Duration
	tcpCloseTimeout time.Duration
	wg              sync.WaitGroup

	localTCPs  []TCPMappingObj
	remoteTCPs []TCPMappingObj
	localUDPs  []UDPMappingObj
	remoteUDPs []UDPMappingObj
}

// New создаёт менеджер форвардинга. sessionTimeout — таймаут неактивности UDP-сессии.
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

// SetTimeout обновляет таймаут неактивности UDP-сессий. Должен вызываться до Start().
func (m *ManagerObj) SetTimeout(d time.Duration) {
	m.timeout = d
}

// SetTCPCloseTimeout задаёт время ожидания второй стороны TCP при закрытии соединения.
// По умолчанию DefaultTCPCloseTimeout. Должен вызываться до Start().
func (m *ManagerObj) SetTCPCloseTimeout(d time.Duration) {
	m.tcpCloseTimeout = d
}

// ClearLocal сбрасывает все локальные маппинги (TCP и UDP). Должен вызываться до Start().
func (m *ManagerObj) ClearLocal() {
	m.localTCPs = nil
	m.localUDPs = nil
}

// ClearRemote сбрасывает все удалённые маппинги (TCP и UDP). Должен вызываться до Start().
func (m *ManagerObj) ClearRemote() {
	m.remoteTCPs = nil
	m.remoteUDPs = nil
}

// //

// Start запускает горутины для всех добавленных маппингов.
// Вызывается один раз после добавления всех маппингов.
func (m *ManagerObj) Start(ctx context.Context, node core.Interface) {
	m.node = node
	m.startLocalTCP(ctx)
	m.startRemoteTCP(ctx)
	m.startLocalUDP(ctx)
	m.startRemoteUDP(ctx)
}

// Wait блокирует до завершения всех горутин форвардинга.
func (m *ManagerObj) Wait() {
	m.wg.Wait()
}
