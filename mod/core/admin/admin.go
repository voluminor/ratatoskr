// Package admin provides the upstream Yggdrasil admin-socket adapter used by core.
package admin

import (
	yggadmin "github.com/yggdrasil-network/yggdrasil-go/src/admin"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
)

// // // // // // // // // //

// ConfigObj contains upstream admin-socket construction parameters.
type ConfigObj struct {
	// Core is the Yggdrasil core that owns the handlers.
	Core *yggcore.Core
	// Logger receives upstream admin logs.
	Logger yggcore.Logger
	// Address is an upstream unix:// or tcp:// listen address.
	Address string
}

// //

// Obj wraps an upstream Yggdrasil admin socket.
type Obj struct {
	socket *yggadmin.AdminSocket
}

// New starts the upstream admin socket and registers its standard handlers.
func New(cfg ConfigObj) (*Obj, error) {
	socket, err := yggadmin.New(cfg.Core, cfg.Logger, yggadmin.ListenAddress(cfg.Address))
	if err != nil || socket == nil {
		return nil, err
	}
	socket.SetupAdminHandlers()
	return &Obj{socket: socket}, nil
}

// //

// AttachMulticast registers the upstream multicast diagnostic handler.
func (o *Obj) AttachMulticast(component *multicast.Multicast) {
	if o == nil || o.socket == nil || component == nil {
		return
	}
	component.SetupAdminHandlers(o.socket)
}

// Stop closes the upstream listener but not accepted keepalive connections.
func (o *Obj) Stop() error {
	if o == nil || o.socket == nil {
		return nil
	}
	return o.socket.Stop()
}
