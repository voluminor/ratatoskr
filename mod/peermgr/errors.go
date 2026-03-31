package peermgr

import "errors"

// // // // // // // // // //

var (
	ErrLoggerRequired    = errors.New("peermgr: logger is required")
	ErrNoPeers           = errors.New("peermgr: no valid peers after validation")
	ErrAlreadyRunning    = errors.New("peermgr: already running")
	ErrNotRunning        = errors.New("peermgr: not running")
	ErrDuplicatePeer     = errors.New("peermgr: duplicate peer")
	ErrInvalidURI        = errors.New("peermgr: invalid URI")
	ErrMissingHost       = errors.New("peermgr: missing host")
	ErrUnsupportedScheme = errors.New("peermgr: unsupported scheme")
	ErrMinPeersTooHigh   = errors.New("peermgr: MinPeers must be less than MaxPerProto")
	ErrMinPeersTooMany   = errors.New("peermgr: MinPeers must be less than the number of valid peers")
)
