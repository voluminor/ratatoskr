package socks

import "errors"

// // // // // // // // // //

var (
	ErrAlreadyEnabled   = errors.New("socks: already enabled")
	ErrAlreadyListening = errors.New("socks: another instance is listening")
	ErrNetworkRequired  = errors.New("socks: network dialer is required")
	ErrSymlinkRefusal   = errors.New("socks: refusing to remove symlink")
	ErrSocketRefusal    = errors.New("socks: refusing to remove non-socket path")
)
