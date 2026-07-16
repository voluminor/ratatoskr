package resolver

import "errors"

// // // // // // // // // //

var (
	// ErrNoNameserver indicates that DNS resolution is disabled.
	ErrNoNameserver = errors.New("resolver: no nameserver configured")
	// ErrNoAddresses indicates a DNS response without addresses.
	ErrNoAddresses = errors.New("resolver: no addresses found")
	// ErrDialerRequired indicates a nameserver configured without a dialer.
	ErrDialerRequired = errors.New("resolver: dialer is required")
	// ErrInvalidPublicKeyDomain indicates a malformed .pk.ygg name.
	ErrInvalidPublicKeyDomain = errors.New("resolver: invalid public key domain")
	// ErrInvalidKeyLength indicates a public key that is not 32 bytes.
	ErrInvalidKeyLength = errors.New("resolver: invalid public key length")
	// ErrNonYggdrasilAddress indicates DNS answers outside Yggdrasil ranges.
	ErrNonYggdrasilAddress = errors.New("resolver: DNS response is not a yggdrasil address")
	// ErrLookupBusy indicates that 256 distinct DNS lookups are active.
	ErrLookupBusy = errors.New("resolver: too many concurrent lookups")
	// ErrClosed indicates an operation attempted after Close.
	ErrClosed = errors.New("resolver: closed")
)
