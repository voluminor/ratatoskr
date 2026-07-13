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
	Enabled                  bool   `json:"enabled"`
	Addr                     string `json:"addr,omitempty"`
	IsUnix                   bool   `json:"is_unix,omitempty"`
	ActiveConnections        int    `json:"active_connections"`
	ActiveAssociateTargets   int    `json:"active_associate_targets"`
	PendingAssociateTargets  int64  `json:"pending_associate_targets"`
	RejectedAssociateTargets int64  `json:"rejected_associate_targets"`
}

// SnapshotObj — full node state at the time of the call
type SnapshotObj struct {
	Address       string            `json:"address"`
	Subnet        string            `json:"subnet"`
	PublicKey     string            `json:"public_key"`
	MTU           uint64            `json:"mtu"`
	RSTDropped    uint64            `json:"rst_dropped"`
	Peers         []PeerSnapshotObj `json:"peers"`
	ActivePeers   []string          `json:"active_peers,omitempty"`
	SOCKS         SOCKSSnapshotObj  `json:"socks"`
	CloseTimedOut bool              `json:"close_timed_out"`
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
	snap.MTU = o.core.MTU()
	snap.RSTDropped = o.core.RSTDropped()
	snap.CloseTimedOut = o.closeTimedOut.Load()
	if addr := o.core.Address(); addr != nil {
		snap.Address = addr.String()
	}
	if sn := o.core.Subnet(); len(sn.IP) > 0 {
		snap.Subnet = sn.String()
	}
	if pk := o.core.PublicKey(); len(pk) == ed25519.PublicKeySize {
		snap.PublicKey = hex.EncodeToString(pk)
	}

	snap.Peers = peerSnapshots(o.core.GetPeers())

	// Peer manager
	if o.peerManager != nil {
		snap.ActivePeers = o.peerManager.Active()
	}

	// SOCKS
	socksStats := o.socks.Snapshot()
	snap.SOCKS = SOCKSSnapshotObj{
		Enabled:                  o.socks.IsEnabled(),
		Addr:                     o.socks.Addr(),
		IsUnix:                   o.socks.IsUnix(),
		ActiveConnections:        socksStats.ActiveConnections,
		ActiveAssociateTargets:   socksStats.ActiveAssociateTargets,
		PendingAssociateTargets:  socksStats.PendingAssociateTargets,
		RejectedAssociateTargets: socksStats.RejectedAssociateTargets,
	}

	return snap
}
