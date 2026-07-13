package socks

import "errors"

// // // // // // // // // //

var (
	ErrAlreadyEnabled       = errors.New("socks: already enabled")
	ErrAlreadyListening     = errors.New("socks: another instance is listening")
	ErrAssociateTargetLimit = errors.New("socks: UDP associate target limit reached")
	ErrInvalidAddress       = errors.New("socks: invalid listen address")
	ErrNetworkRequired      = errors.New("socks: network dialer is required")
	ErrResolverRequired     = errors.New("socks: resolver is required for domain targets")
	ErrSymlinkRefusal       = errors.New("socks: refusing to remove symlink")
	ErrSocketRefusal        = errors.New("socks: refusing to remove non-socket path")
	ErrUnsafeSocketDir      = errors.New("socks: unsafe unix socket directory")
	ErrSocketChanged        = errors.New("socks: unix socket path changed")
)
