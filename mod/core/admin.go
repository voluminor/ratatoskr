package core

import (
	"fmt"

	"github.com/voluminor/ratatoskr/mod/core/admin"
)

// // // // // // // // // //

func (o *Obj) newAdminSocket(addr string) (*admin.Obj, func() error, error) {
	c := o.corePtr.Load()
	if c == nil {
		return nil, nil, ErrNotAvailable
	}
	as, err := admin.New(admin.ConfigObj{
		Core:    c,
		Logger:  o.logger,
		Address: addr,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("admin.New: %w", err)
	}
	if as == nil {
		return nil, nil, fmt.Errorf("%w for address %q", ErrAdminDisabled, addr)
	}
	return as, as.Stop, nil
}

// EnableAdmin starts the unsafe upstream admin socket at addr.
func (o *Obj) EnableAdmin(addr string) error {
	o.adminMu.Lock()
	defer o.adminMu.Unlock()

	err := o.adminSocket.enable(func() (*admin.Obj, func() error, error) {
		return o.newAdminSocket(addr)
	})
	if err != nil {
		return err
	}
	o.attachMulticastAdminHandler()
	return nil
}

// DisableAdmin stops the admin listener without revoking accepted connections.
func (o *Obj) DisableAdmin() error {
	o.adminMu.Lock()
	defer o.adminMu.Unlock()

	return o.adminSocket.disable()
}

func (o *Obj) attachMulticastAdminHandler() {
	as, adminActive := o.adminSocket.get()
	mc, multicastActive := o.multicast.get()
	if adminActive && multicastActive {
		as.AttachMulticast(mc)
	}
}
