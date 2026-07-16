package core

import (
	"fmt"
	"net/url"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// AddPeer adds a peer by URI.
func (o *Obj) AddPeer(uri string) error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("url.Parse: %w", err)
	}
	return c.AddPeer(u, "")
}

// RemovePeer removes a peer by URI.
func (o *Obj) RemovePeer(uri string) error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("url.Parse: %w", err)
	}
	return c.RemovePeer(u, "")
}

// GetPeers returns configured and connected peers.
func (o *Obj) GetPeers() []yggcore.PeerInfo {
	c := o.corePtr.Load()
	if c == nil {
		return nil
	}
	return c.GetPeers()
}

// RetryPeers requests an immediate retry of configured peers.
func (o *Obj) RetryPeers() error {
	c := o.corePtr.Load()
	if c == nil {
		return ErrNotAvailable
	}
	c.RetryPeersNow()
	return nil
}
