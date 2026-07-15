package peermgr

import "errors"

// // // // // // // // // //

var (
	// ErrNodeRequired indicates a nil node in ConfigObj.
	ErrNodeRequired = errors.New("peermgr: node is required")
	// ErrNoPeers indicates that validation accepted no candidates.
	ErrNoPeers = errors.New("peermgr: no valid peers after validation")
	// ErrClosed indicates an Optimize call after shutdown began.
	ErrClosed = errors.New("peermgr: closed")
	// ErrDuplicatePeer indicates duplicate normalized peer identities.
	ErrDuplicatePeer = errors.New("peermgr: duplicate peer")
	// ErrInvalidURI indicates a malformed or structurally incomplete peer URI.
	ErrInvalidURI = errors.New("peermgr: invalid URI")
	// ErrInvalidMaxPerProto indicates a negative per-protocol limit.
	ErrInvalidMaxPerProto = errors.New("peermgr: MaxPerProto must be 0 or greater")
	// ErrInvalidProbeTimeout indicates a negative connection wait.
	ErrInvalidProbeTimeout = errors.New("peermgr: ProbeTimeout must be 0 or greater")
	// ErrInvalidBatchSize indicates a negative candidate budget.
	ErrInvalidBatchSize = errors.New("peermgr: BatchSize must be 0 or greater")
	// ErrInvalidMinPeers indicates a negative health threshold.
	ErrInvalidMinPeers = errors.New("peermgr: MinPeers must be 0 or greater")
	// ErrInvalidMinPeersConfirmations indicates a negative confirmation count.
	ErrInvalidMinPeersConfirmations = errors.New("peermgr: MinPeersConfirmations must be 0 or greater")
	// ErrMinPeersTooHigh indicates an unreachable threshold for the candidate set.
	ErrMinPeersTooHigh = errors.New("peermgr: MinPeers must be below the selectable peer capacity")
)
