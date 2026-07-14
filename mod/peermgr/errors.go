package peermgr

import "errors"

// // // // // // // // // //

var (
	ErrNodeRequired                 = errors.New("peermgr: node is required")
	ErrNoPeers                      = errors.New("peermgr: no valid peers after validation")
	ErrClosed                       = errors.New("peermgr: closed")
	ErrDuplicatePeer                = errors.New("peermgr: duplicate peer")
	ErrInvalidURI                   = errors.New("peermgr: invalid URI")
	ErrInvalidMaxPerProto           = errors.New("peermgr: MaxPerProto must be 0 or greater")
	ErrInvalidProbeTimeout          = errors.New("peermgr: ProbeTimeout must be 0 or greater")
	ErrInvalidBatchSize             = errors.New("peermgr: BatchSize must be 0 or greater")
	ErrInvalidMinPeers              = errors.New("peermgr: MinPeers must be 0 or greater")
	ErrInvalidMinPeersConfirmations = errors.New("peermgr: MinPeersConfirmations must be 0 or greater")
	ErrMinPeersTooHigh              = errors.New("peermgr: MinPeers must be below the selectable peer capacity")
)
