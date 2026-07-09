package ratatoskr

import (
	"crypto/ed25519"
	"encoding/hex"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// PeerSnapshotObj — state of a single peer
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

// SOCKSSnapshotObj — SOCKS5 proxy state
type SOCKSSnapshotObj struct {
	Enabled           bool   `json:"enabled"`
	Addr              string `json:"addr,omitempty"`
	IsUnix            bool   `json:"is_unix,omitempty"`
	ActiveConnections int    `json:"active_connections"`
}

// SnapshotObj — full node state at the time of the call
type SnapshotObj struct {
	Address     string            `json:"address"`
	Subnet      string            `json:"subnet"`
	PublicKey   string            `json:"public_key"`
	MTU         uint64            `json:"mtu"`
	RSTDropped  uint64            `json:"rst_dropped"`
	Peers       []PeerSnapshotObj `json:"peers"`
	ActivePeers []string          `json:"active_peers,omitempty"`
	SOCKS       SOCKSSnapshotObj  `json:"socks"`
}

// //

func peerSnapshots(peers []yggcore.PeerInfo) []PeerSnapshotObj {
	out := make([]PeerSnapshotObj, len(peers))
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
		out[i] = entry
	}
	return out
}

// Snapshot collects the full node state in a single call
func (o *Obj) Snapshot() SnapshotObj {
	snap := SnapshotObj{}
	snap.MTU = o.MTU()
	snap.RSTDropped = o.RSTDropped()
	if addr := o.Address(); addr != nil {
		snap.Address = addr.String()
	}
	if sn := o.Subnet(); len(sn.IP) > 0 {
		snap.Subnet = sn.String()
	}
	if pk := o.PublicKey(); len(pk) == ed25519.PublicKeySize {
		snap.PublicKey = hex.EncodeToString(pk)
	}

	snap.Peers = peerSnapshots(o.GetPeers())

	// Peer manager
	if o.peerManager != nil {
		snap.ActivePeers = o.peerManager.Active()
	}

	// SOCKS
	socksServer := o.socks
	if socksServer != nil {
		snap.SOCKS = SOCKSSnapshotObj{
			Enabled:           socksServer.IsEnabled(),
			Addr:              socksServer.Addr(),
			IsUnix:            socksServer.IsUnix(),
			ActiveConnections: socksServer.ActiveConnections(),
		}
	}

	return snap
}
