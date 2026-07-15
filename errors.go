package ratatoskr

import "errors"

// // // // // // // // // //

var (
	// ErrPeersConflict means both static and managed peers were configured.
	ErrPeersConflict = errors.New("ratatoskr: cannot use Config.Peers and Peers manager simultaneously")
	// ErrPeerManagerNotEnabled means a peer-manager operation was requested
	// without configuring the manager.
	ErrPeerManagerNotEnabled = errors.New("ratatoskr: peer manager not enabled")
	// ErrClosed means the node is closed or closing.
	ErrClosed = errors.New("ratatoskr: closed")
	// ErrCloseTimedOut means Close exceeded its configured wait budget.
	ErrCloseTimedOut = errors.New("ratatoskr: close timed out")
	// ErrInvalidCloseTimeout means a negative close timeout was configured.
	ErrInvalidCloseTimeout = errors.New("ratatoskr: close timeout must not be negative")
	// ErrInvalidNodeInfo means local NodeInfo could not be cloned safely.
	ErrInvalidNodeInfo = errors.New("ratatoskr: invalid NodeInfo")
	// ErrInvalidSigils means sigil assembly or parser configuration failed.
	ErrInvalidSigils = errors.New("ratatoskr: invalid sigil configuration")
)
