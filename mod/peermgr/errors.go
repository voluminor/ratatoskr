package peermgr

import "errors"

// // // // // // // // // //

var (
	ErrNodeRequired       = errors.New("peermgr: node is required")
	ErrNoPeers            = errors.New("peermgr: no valid peers after validation")
	ErrAlreadyRunning     = errors.New("peermgr: already running")
	ErrNotRunning         = errors.New("peermgr: not running")
	ErrDuplicatePeer      = errors.New("peermgr: duplicate peer")
	ErrInvalidURI         = errors.New("peermgr: invalid URI")
	ErrMissingHost        = errors.New("peermgr: missing host")
	ErrUnsupportedScheme  = errors.New("peermgr: unsupported scheme")
	ErrInvalidMaxPerProto = errors.New("peermgr: MaxPerProto must be -1 or greater")
)
