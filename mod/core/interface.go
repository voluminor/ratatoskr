package core

import (
	"context"
	"crypto/ed25519"
	"net"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// NetworkInterface is the networking and identity contract of the node.
type NetworkInterface interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	Listen(network, address string) (net.Listener, error)
	ListenPacket(network, address string) (net.PacketConn, error)
	Address() net.IP
	Subnet() net.IPNet
	PublicKey() ed25519.PublicKey
	MTU() uint64
}

// PeerInterface controls and reports peer state.
type PeerInterface interface {
	AddPeer(uri string) error
	RemovePeer(uri string) error
	GetPeers() []yggcore.PeerInfo
	RetryPeers() error
}

// MulticastInterface controls local peer discovery.
type MulticastInterface interface {
	EnableMulticast() error
	DisableMulticast() error
}

// AdminInterface controls the optional admin socket.
type AdminInterface interface {
	EnableAdmin(addr string) error
	DisableAdmin() error
}

// StatsInterface exposes node counters.
type StatsInterface interface {
	RSTDropped() uint64
}

// Interface is the full public contract of the Yggdrasil node.
type Interface interface {
	NetworkInterface
	PeerInterface
	AdminInterface
	MulticastInterface
	StatsInterface
	Close() error
}
