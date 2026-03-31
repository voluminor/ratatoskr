package ninfo

import "errors"

// // // // // // // // // //

var (
	ErrCoreRequired        = errors.New("ninfo: core is required")
	ErrLoggerRequired      = errors.New("ninfo: logger is required")
	ErrNodeInfoNotCaptured = errors.New("ninfo: getNodeInfo was not captured")
	ErrInvalidKeyLength    = errors.New("ninfo: invalid key length")
	ErrUnexpectedResponse  = errors.New("ninfo: unexpected response type")
	ErrEmptyResponse       = errors.New("ninfo: empty response")
	ErrUnresolvableAddr    = errors.New("ninfo: cannot resolve address to public key")
	ErrInvalidAddr         = errors.New("ninfo: invalid address format")
)
