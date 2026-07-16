package socks

import "errors"

// // // // // // // // // //

var (
	// ErrAlreadyEnabled indicates Start on a running server.
	ErrAlreadyEnabled = errors.New("socks: already enabled")
	// ErrAlreadyListening indicates a live owner of a Unix socket.
	ErrAlreadyListening = errors.New("socks: another instance is listening")
	// ErrAssociateTargetLimit indicates exhausted UDP target admission.
	ErrAssociateTargetLimit = errors.New("socks: UDP associate target limit reached")
	// ErrInvalidAddress indicates an empty or malformed listen address.
	ErrInvalidAddress = errors.New("socks: invalid listen address")
	// ErrNetworkRequired indicates a nil outbound dialer.
	ErrNetworkRequired = errors.New("socks: network dialer is required")
	// ErrResolverRequired indicates a domain target without an allowed resolver.
	ErrResolverRequired = errors.New("socks: resolver is required for domain targets")
	// ErrSymlinkRefusal indicates safe stale-socket cleanup rejected a symlink.
	ErrSymlinkRefusal = errors.New("socks: refusing to remove symlink")
	// ErrSocketRefusal indicates safe cleanup rejected a non-socket path.
	ErrSocketRefusal = errors.New("socks: refusing to remove non-socket path")
	// ErrUnsafeSocketDir indicates a non-private Unix socket directory.
	ErrUnsafeSocketDir = errors.New("socks: unsafe unix socket directory")
	// ErrSocketChanged indicates replacement during stale-socket validation.
	ErrSocketChanged = errors.New("socks: unix socket path changed")
)
