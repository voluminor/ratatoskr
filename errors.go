package ratatoskr

import "errors"

// // // // // // // // // //

var (
	ErrPeersConflict         = errors.New("ratatoskr: cannot use Config.Peers and Peers manager simultaneously")
	ErrPeerManagerNotEnabled = errors.New("ratatoskr: peer manager not enabled")
)
