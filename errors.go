package ratatoskr

import "errors"

// // // // // // // // // //

var (
	ErrPeersConflict         = errors.New("ratatoskr: cannot use Config.Peers and Peers manager simultaneously")
	ErrPeerManagerNotEnabled = errors.New("ratatoskr: peer manager not enabled")
	ErrClosed                = errors.New("ratatoskr: closed")
	ErrCloseTimedOut         = errors.New("ratatoskr: close timed out")
	ErrInvalidCloseTimeout   = errors.New("ratatoskr: close timeout must not be negative")
	ErrInvalidNodeInfo       = errors.New("ratatoskr: invalid NodeInfo")
)
