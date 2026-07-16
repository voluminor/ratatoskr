package ratatoskr

import (
	"crypto/ed25519"
	"encoding/hex"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// PeerSnapshotObj describes one peer at snapshot time.
type PeerSnapshotObj struct {
	URI           string        `json:"uri"`                       // URI identifies the configured or inbound peer.
	Up            bool          `json:"up"`                        // Up reports whether the peer is connected.
	Inbound       bool          `json:"inbound"`                   // Inbound reports the connection direction.
	Key           string        `json:"key"`                       // Key is the hexadecimal peer public key.
	Latency       time.Duration `json:"latency"`                   // Latency is the measured peer latency.
	Cost          uint64        `json:"cost"`                      // Cost is the routing cost through the peer.
	RXBytes       uint64        `json:"rx_bytes"`                  // RXBytes is the received byte count.
	TXBytes       uint64        `json:"tx_bytes"`                  // TXBytes is the transmitted byte count.
	Uptime        time.Duration `json:"uptime"`                    // Uptime is the current connection duration.
	LastError     string        `json:"last_error,omitempty"`      // LastError is the latest peer error.
	LastErrorTime time.Time     `json:"last_error_time,omitempty"` // LastErrorTime is when LastError occurred.
}

// SOCKSSnapshotObj describes the SOCKS5 service at snapshot time.
type SOCKSSnapshotObj struct {
	Enabled                  bool   `json:"enabled"`                    // Enabled reports whether the service is running.
	Addr                     string `json:"addr,omitempty"`             // Addr is the bound listener address.
	IsUnix                   bool   `json:"is_unix,omitempty"`          // IsUnix reports whether Addr is a Unix socket.
	ActiveConnections        int    `json:"active_connections"`         // ActiveConnections is the accepted connection count.
	ActiveAssociateTargets   int    `json:"active_associate_targets"`   // ActiveAssociateTargets is the established UDP target count.
	PendingAssociateTargets  int64  `json:"pending_associate_targets"`  // PendingAssociateTargets is the in-flight UDP target count.
	RejectedAssociateTargets uint64 `json:"rejected_associate_targets"` // RejectedAssociateTargets is the admission rejection count.
	DroppedAssociatePackets  uint64 `json:"dropped_associate_packets"`  // DroppedAssociatePackets is the overload drop count.
}

// SnapshotObj describes the node at snapshot time.
type SnapshotObj struct {
	Address       string            `json:"address"`                // Address is the node IPv6 address.
	Subnet        string            `json:"subnet"`                 // Subnet is the node routable /64 subnet.
	PublicKey     string            `json:"public_key"`             // PublicKey is the hexadecimal node public key.
	MTU           uint64            `json:"mtu"`                    // MTU is the node interface MTU.
	Peers         []PeerSnapshotObj `json:"peers"`                  // Peers contains configured and inbound peers.
	ActivePeers   []string          `json:"active_peers,omitempty"` // ActivePeers contains peer-manager selections.
	SOCKS         SOCKSSnapshotObj  `json:"socks"`                  // SOCKS describes the SOCKS5 service.
	CloseTimedOut bool              `json:"close_timed_out"`        // CloseTimedOut reports a prior close timeout.
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

// Snapshot returns the current node and service state.
func (o *Obj) Snapshot() SnapshotObj {
	snap := SnapshotObj{}
	snap.MTU = o.core.MTU()
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

	if o.peerManager != nil {
		snap.ActivePeers = o.peerManager.Active()
	}

	socksStats := o.socks.Snapshot()
	snap.SOCKS = SOCKSSnapshotObj{
		Enabled:                  o.socks.IsEnabled(),
		Addr:                     o.socks.Addr(),
		IsUnix:                   o.socks.IsUnix(),
		ActiveConnections:        socksStats.ActiveConnections,
		ActiveAssociateTargets:   socksStats.ActiveAssociateTargets,
		PendingAssociateTargets:  socksStats.PendingAssociateTargets,
		RejectedAssociateTargets: socksStats.RejectedAssociateTargets,
		DroppedAssociatePackets:  socksStats.DroppedAssociatePackets,
	}

	return snap
}
