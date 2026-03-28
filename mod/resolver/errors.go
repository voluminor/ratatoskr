package resolver

import "errors"

// // // // // // // // // //

var (
	ErrNoNameserver     = errors.New("resolver: no nameserver configured")
	ErrNoAddresses      = errors.New("resolver: no addresses found")
	ErrInvalidKeyLength = errors.New("resolver: invalid public key length")
)
