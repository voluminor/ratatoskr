package peermgr

import (
	"context"
	"crypto/ed25519"
	"net"
	"sync"

	golog "github.com/gologme/log"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// mockNodeObj — minimal core.Interface for peermgr tests
type mockNodeObj struct {
	mu      sync.Mutex
	peers   []yggcore.PeerInfo
	added   []string
	removed []string
}

func (m *mockNodeObj) AddPeer(uri string) error {
	m.mu.Lock()
	m.added = append(m.added, uri)
	m.mu.Unlock()
	return nil
}

func (m *mockNodeObj) RemovePeer(uri string) error {
	m.mu.Lock()
	m.removed = append(m.removed, uri)
	m.mu.Unlock()
	return nil
}

func (m *mockNodeObj) GetPeers() []yggcore.PeerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]yggcore.PeerInfo, len(m.peers))
	copy(out, m.peers)
	return out
}

func (m *mockNodeObj) DialContext(_ context.Context, _, _ string) (net.Conn, error) { return nil, nil }
func (m *mockNodeObj) Listen(_, _ string) (net.Listener, error)                     { return nil, nil }
func (m *mockNodeObj) ListenPacket(_, _ string) (net.PacketConn, error)             { return nil, nil }
func (m *mockNodeObj) Address() net.IP                                              { return net.ParseIP("200::1") }
func (m *mockNodeObj) Subnet() net.IPNet                                            { return net.IPNet{} }
func (m *mockNodeObj) PublicKey() ed25519.PublicKey                                 { return nil }
func (m *mockNodeObj) MTU() uint64                                                  { return 65535 }
func (m *mockNodeObj) EnableMulticast(_ *golog.Logger) error                        { return nil }
func (m *mockNodeObj) DisableMulticast() error                                      { return nil }
func (m *mockNodeObj) EnableAdmin(_ string) error                                   { return nil }
func (m *mockNodeObj) DisableAdmin() error                                          { return nil }
func (m *mockNodeObj) Close() error                                                 { return nil }

// //

// noopLogObj — yggcore.Logger that discards all messages
type noopLogObj struct{}

func (noopLogObj) Printf(string, ...interface{}) {}
func (noopLogObj) Println(...interface{})        {}
func (noopLogObj) Infof(string, ...interface{})  {}
func (noopLogObj) Infoln(...interface{})         {}
func (noopLogObj) Warnf(string, ...interface{})  {}
func (noopLogObj) Warnln(...interface{})         {}
func (noopLogObj) Errorf(string, ...interface{}) {}
func (noopLogObj) Errorln(...interface{})        {}
func (noopLogObj) Debugf(string, ...interface{}) {}
func (noopLogObj) Debugln(...interface{})        {}
func (noopLogObj) Traceln(...interface{})        {}
