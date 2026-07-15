package core

import "errors"

// // // // // // // // // //

var (
	// ErrNotAvailable indicates that the node or netstack has closed.
	ErrNotAvailable = errors.New("core: netstack is not available")
	// ErrAlreadyEnabled indicates that an optional component is already active.
	ErrAlreadyEnabled = errors.New("core: already enabled")
	// ErrAdminDisabled indicates that the requested address disabled admin upstream.
	ErrAdminDisabled = errors.New("core: admin socket disabled")
	// ErrUnsupportedNetwork indicates an unsupported Go network name.
	ErrUnsupportedNetwork = errors.New("core: unsupported network")
	// ErrPortRequired indicates that an address omitted its port.
	ErrPortRequired = errors.New("core: port is required")
	// ErrPortOutOfRange indicates a port outside 0 through 65535.
	ErrPortOutOfRange = errors.New("core: port out of range 0-65535")
	// ErrInvalidAddress indicates an invalid IP literal.
	ErrInvalidAddress = errors.New("core: invalid IP address")
	// ErrIPv6Only indicates that an IPv4 address was supplied.
	ErrIPv6Only = errors.New("core: IPv6 address required")
	// ErrInvalidNodeInfo indicates data that cannot be cloned safely.
	ErrInvalidNodeInfo = errors.New("core: invalid NodeInfo")

	// ErrInvalidAllowedPublicKey indicates a malformed allowlist entry.
	ErrInvalidAllowedPublicKey = errors.New("core: invalid AllowedPublicKey")
)
