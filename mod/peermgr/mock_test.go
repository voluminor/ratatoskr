package peermgr

import (
	"errors"
	"sync"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type mockNodeObj struct {
	mu             sync.Mutex
	peers          []yggcore.PeerInfo
	added          []string
	removed        []string
	addAttempts    int
	addPeerFail    map[string]bool
	removePeerFail map[string]bool
}

func (m *mockNodeObj) AddPeer(uri string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addAttempts++
	if m.addPeerFail[uri] {
		return errors.New("add peer failed")
	}
	m.added = append(m.added, uri)
	return nil
}

func (m *mockNodeObj) RemovePeer(uri string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, uri)
	if m.removePeerFail[uri] {
		return errors.New("remove peer failed")
	}
	return nil
}

func (m *mockNodeObj) GetPeers() []yggcore.PeerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]yggcore.PeerInfo, len(m.peers))
	copy(out, m.peers)
	return out
}
