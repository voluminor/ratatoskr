package ninfo

import "errors"

// // // // // // // // // //

var (
	// ErrSourceRequired indicates a nil source in ConfigObj.
	ErrSourceRequired = errors.New("ninfo: source is required")
	// ErrNodeInfoNotCaptured indicates that upstream did not expose getNodeInfo.
	ErrNodeInfoNotCaptured = errors.New("ninfo: getNodeInfo was not captured")
	// ErrInvalidKeyLength indicates a public key that is not 32 bytes.
	ErrInvalidKeyLength = errors.New("ninfo: invalid key length")
	// ErrUnexpectedResponse indicates an incompatible upstream response type.
	ErrUnexpectedResponse = errors.New("ninfo: unexpected response type")
	// ErrEmptyResponse indicates a getNodeInfo response without an entry.
	ErrEmptyResponse = errors.New("ninfo: empty response")
	// ErrNodeInfoTooLarge indicates a response larger than 16 KiB.
	ErrNodeInfoTooLarge = errors.New("ninfo: nodeinfo response too large")
	// ErrUnresolvableAddr indicates an address lookup that found no key.
	ErrUnresolvableAddr = errors.New("ninfo: cannot resolve address to public key")
	// ErrInvalidAddr indicates an unsupported or malformed address.
	ErrInvalidAddr = errors.New("ninfo: invalid address format")
	// ErrClosed indicates an operation attempted after Close.
	ErrClosed = errors.New("ninfo: closed")
	// ErrAskBusy indicates that 64 distinct NodeInfo queries are active.
	ErrAskBusy = errors.New("ninfo: too many distinct node-info queries")
	// ErrResolveBusy indicates that 64 distinct address lookups are active.
	ErrResolveBusy = errors.New("ninfo: too many distinct address lookups")
	// ErrInvalidLookupInterval indicates a negative polling interval.
	ErrInvalidLookupInterval = errors.New("ninfo: lookup interval must not be negative")
	// ErrInvalidSigil indicates an invalid custom parser configuration.
	ErrInvalidSigil = errors.New("ninfo: invalid custom sigil")
)
