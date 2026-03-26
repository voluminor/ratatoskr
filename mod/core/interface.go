package core

import (
	"context"
	"crypto/ed25519"
	"net"

	golog "github.com/gologme/log"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Interface — public contract of the Yggdrasil node
type Interface interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	Listen(network, address string) (net.Listener, error)
	ListenPacket(network, address string) (net.PacketConn, error)
	Address() net.IP
	Subnet() net.IPNet
	PublicKey() ed25519.PublicKey
	MTU() uint64
	AddPeer(uri string) error
	RemovePeer(uri string) error
	GetPeers() []yggcore.PeerInfo
	EnableMulticast(logger *golog.Logger) error // todo: gologme — temporary dependency until a new Yggdrasil release
	DisableMulticast() error
	EnableAdmin(addr string) error
	DisableAdmin() error
	Close() error
}
