package ratatoskr

import (
	"crypto/ed25519"
	"encoding/hex"
	"time"

	"github.com/voluminor/ratatoskr/mod/core"
)

// // // // // // // // // //

// PeerSnapshotObj — состояние одного пира
type PeerSnapshotObj struct {
	URI           string        `json:"uri"`
	Up            bool          `json:"up"`
	Inbound       bool          `json:"inbound"`
	Key           string        `json:"key"`
	Latency       time.Duration `json:"latency"`
	Cost          uint64        `json:"cost"`
	RXBytes       uint64        `json:"rx_bytes"`
	TXBytes       uint64        `json:"tx_bytes"`
	Uptime        time.Duration `json:"uptime"`
	LastError     string        `json:"last_error,omitempty"`
	LastErrorTime time.Time     `json:"last_error_time,omitempty"`
}

// SOCKSSnapshotObj — состояние SOCKS5-прокси
type SOCKSSnapshotObj struct {
	Enabled bool   `json:"enabled"`
	Addr    string `json:"addr,omitempty"`
	IsUnix  bool   `json:"is_unix,omitempty"`
}

// SnapshotObj — полное состояние узла в момент вызова
type SnapshotObj struct {
	Address     string            `json:"address"`
	Subnet      string            `json:"subnet"`
	PublicKey   string            `json:"public_key"`
	MTU         uint64            `json:"mtu"`
	RSTDropped  int64             `json:"rst_dropped"`
	Peers       []PeerSnapshotObj `json:"peers"`
	ActivePeers []string          `json:"active_peers,omitempty"`
	SOCKS       SOCKSSnapshotObj  `json:"socks"`
}

// //

// Snapshot собирает полное состояние узла за один вызов
func (o *Obj) Snapshot() SnapshotObj {
	snap := SnapshotObj{
		MTU: o.Interface.MTU(),
	}
	if coreNode, ok := o.Interface.(*core.Obj); ok {
		snap.RSTDropped = coreNode.RSTDropped()
	}

	if addr := o.Interface.Address(); addr != nil {
		snap.Address = addr.String()
	}
	if sn := o.Interface.Subnet(); len(sn.IP) > 0 {
		snap.Subnet = sn.String()
	}
	if pk := o.Interface.PublicKey(); len(pk) == ed25519.PublicKeySize {
		snap.PublicKey = hex.EncodeToString(pk)
	}

	// Пиры
	peers := o.Interface.GetPeers()
	snap.Peers = make([]PeerSnapshotObj, len(peers))
	for i, p := range peers {
		entry := PeerSnapshotObj{
			URI:     p.URI,
			Up:      p.Up,
			Inbound: p.Inbound,
			Key:     hex.EncodeToString(p.Key),
			Latency: p.Latency,
			Cost:    p.Cost,
			RXBytes: p.RXBytes,
			TXBytes: p.TXBytes,
			Uptime:  p.Uptime,
		}
		if p.LastError != nil {
			entry.LastError = p.LastError.Error()
			entry.LastErrorTime = p.LastErrorTime
		}
		snap.Peers[i] = entry
	}

	// Менеджер пиров
	if o.peerMgr != nil {
		snap.ActivePeers = o.peerMgr.Active()
	}

	// SOCKS
	snap.SOCKS = SOCKSSnapshotObj{
		Enabled: o.socksServer.IsEnabled(),
		Addr:    o.socksServer.Addr(),
		IsUnix:  o.socksServer.IsUnix(),
	}

	return snap
}
