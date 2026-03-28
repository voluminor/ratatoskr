package socks

import "errors"

// // // // // // // // // //

var (
	ErrAlreadyEnabled   = errors.New("socks: already enabled")
	ErrAlreadyListening = errors.New("socks: another instance is listening")
	ErrSymlinkRefusal   = errors.New("socks: refusing to remove symlink")
)
