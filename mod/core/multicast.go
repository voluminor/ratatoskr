package core

import (
	"fmt"
	"regexp"

	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
)

// // // // // // // // // //

// EnableMulticast starts local peer discovery using the configured interfaces.
func (o *Obj) EnableMulticast() error {
	err := o.multicast.enable(func() (*multicast.Multicast, func() error, error) {
		c := o.corePtr.Load()
		if c == nil {
			return nil, nil, ErrNotAvailable
		}
		options := make([]multicast.SetupOption, 0, len(o.nodeCfg.MulticastInterfaces))
		for _, intf := range o.nodeCfg.MulticastInterfaces {
			re, err := regexp.Compile(intf.Regex)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid multicast regex %q: %w", intf.Regex, err)
			}
			options = append(options, multicast.MulticastInterface{
				Regex:    re,
				Beacon:   intf.Beacon,
				Listen:   intf.Listen,
				Port:     intf.Port,
				Priority: uint8(intf.Priority),
				Password: intf.Password,
			})
		}
		mc, err := multicast.New(c, o.logger, options...)
		if err != nil {
			return nil, nil, fmt.Errorf("multicast.New: %w", err)
		}
		return mc, mc.Stop, nil
	})
	if err != nil {
		return err
	}
	o.adminMu.Lock()
	o.attachMulticastAdminHandler()
	o.adminMu.Unlock()
	return nil
}

// DisableMulticast stops local peer discovery.
func (o *Obj) DisableMulticast() error {
	return o.multicast.disable()
}
