package resolver

import "errors"

// // // // // // // // // //

var (
	ErrNoNameserver           = errors.New("resolver: no nameserver configured")
	ErrNoAddresses            = errors.New("resolver: no addresses found")
	ErrDialerRequired         = errors.New("resolver: dialer is required")
	ErrInvalidPublicKeyDomain = errors.New("resolver: invalid public key domain")
	ErrInvalidKeyLength       = errors.New("resolver: invalid public key length")
	ErrNonYggdrasilAddress    = errors.New("resolver: DNS response is not a yggdrasil address")
)
