// Package admin exposes the intentionally thin Yggdrasil admin-socket adapter
// used by mod/core. It inherits the upstream implementation's unsafe lifecycle,
// concurrency, resource-management, and process-exit behavior.
package admin

import (
	yggadmin "github.com/yggdrasil-network/yggdrasil-go/src/admin"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
)

// // // // // // // // // //

// ConfigObj contains upstream admin-socket construction parameters.
type ConfigObj struct {
	Core    *yggcore.Core
	Logger  yggcore.Logger
	Address string
}

// //

// Obj is a direct lifecycle wrapper around the upstream admin socket.
type Obj struct {
	socket *yggadmin.AdminSocket
}

// New starts the upstream admin socket and registers its standard handlers.
// The upstream listener starts before handler registration completes and may
// call os.Exit(1) on bind or Unix-socket cleanup failures.
func New(cfg ConfigObj) (*Obj, error) {
	socket, err := yggadmin.New(cfg.Core, cfg.Logger, yggadmin.ListenAddress(cfg.Address))
	if err != nil || socket == nil {
		return nil, err
	}
	socket.SetupAdminHandlers()
	return &Obj{socket: socket}, nil
}

// //

// AttachMulticast registers the upstream multicast diagnostic handler. It is
// unsafe while the admin socket is serving because upstream mutates its handler
// map without synchronization.
func (o *Obj) AttachMulticast(component *multicast.Multicast) {
	if o == nil || o.socket == nil || component == nil {
		return
	}
	component.SetupAdminHandlers(o.socket)
}

// Stop delegates to the upstream socket. Upstream closes the listener but does
// not close already accepted keepalive connections.
func (o *Obj) Stop() error {
	if o == nil || o.socket == nil {
		return nil
	}
	return o.socket.Stop()
}
