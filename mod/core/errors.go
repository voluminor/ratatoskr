package core

import "errors"

// // // // // // // // // //

var (
	ErrNotAvailable       = errors.New("core: netstack is not available")
	ErrCloseTimedOut      = errors.New("core: close timed out")
	ErrAlreadyEnabled     = errors.New("core: already enabled")
	ErrAdminDisabled      = errors.New("core: admin socket disabled")
	ErrUnsupportedNetwork = errors.New("core: unsupported network")
	ErrPortRequired       = errors.New("core: port is required")
	ErrPortOutOfRange     = errors.New("core: port out of range 0-65535")
	ErrInvalidAddress     = errors.New("core: invalid IP address")
	ErrIPv6Only           = errors.New("core: IPv6 address required")
	ErrRSTQueueTooLarge   = errors.New("core: RST queue size too large")

	ErrInvalidAllowedPublicKey = errors.New("core: invalid AllowedPublicKey")
)
