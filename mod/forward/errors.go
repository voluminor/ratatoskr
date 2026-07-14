package forward

import "errors"

// // // // // // // // // //

var (
	// ErrInvalidSessionTimeout is returned by New when the UDP session timeout is not positive.
	ErrInvalidSessionTimeout = errors.New("forward: session timeout must be > 0")
	// ErrNodeRequired is returned by New when the forwarding network is missing.
	ErrNodeRequired = errors.New("forward: node is required")
	// ErrInvalidMapping is returned by New when a forwarding mapping is incomplete.
	ErrInvalidMapping = errors.New("forward: invalid mapping")
	// ErrInvalidLimit is returned when an object or standalone admission limit is negative.
	ErrInvalidLimit = errors.New("forward: connection and session limits must be >= 0")
)
