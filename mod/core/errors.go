package core

import "errors"

// // // // // // // // // //

var (
	ErrNotAvailable       = errors.New("core: netstack is not available")
	ErrCloseTimedOut      = errors.New("core: close timed out")
	ErrAlreadyEnabled     = errors.New("core: already enabled")
	ErrAdminDisabled      = errors.New("core: admin socket disabled")
	ErrUnsupportedNetwork = errors.New("core: unsupported network")
	ErrPortOutOfRange     = errors.New("core: port out of range 0-65535")
	ErrInvalidAddress     = errors.New("core: invalid IP address")
)
