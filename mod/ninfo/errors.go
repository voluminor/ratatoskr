package ninfo

import "errors"

// // // // // // // // // //

var (
	ErrSourceRequired        = errors.New("ninfo: source is required")
	ErrNodeInfoNotCaptured   = errors.New("ninfo: getNodeInfo was not captured")
	ErrInvalidKeyLength      = errors.New("ninfo: invalid key length")
	ErrUnexpectedResponse    = errors.New("ninfo: unexpected response type")
	ErrEmptyResponse         = errors.New("ninfo: empty response")
	ErrNodeInfoTooLarge      = errors.New("ninfo: nodeinfo response too large")
	ErrUnresolvableAddr      = errors.New("ninfo: cannot resolve address to public key")
	ErrInvalidAddr           = errors.New("ninfo: invalid address format")
	ErrClosed                = errors.New("ninfo: closed")
	ErrAskBusy               = errors.New("ninfo: too many distinct node-info queries")
	ErrResolveBusy           = errors.New("ninfo: too many distinct address lookups")
	ErrInvalidLookupInterval = errors.New("ninfo: lookup interval must not be negative")
	ErrInvalidSigil          = errors.New("ninfo: invalid custom sigil")
)
