package common

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// // // // // // // // // //

const (
	// PublicKeyDomainSuffix identifies deterministic Yggdrasil public-key names.
	PublicKeyDomainSuffix = ".pk.ygg"
	publicKeyHexLength    = ed25519.PublicKeySize * 2
)

var (
	// ErrInvalidPublicKeyDomain reports malformed hexadecimal key data.
	ErrInvalidPublicKeyDomain = errors.New("invalid public key domain")
	// ErrInvalidPublicKeyLength reports a key whose encoded length is not 64 characters.
	ErrInvalidPublicKeyLength = errors.New("invalid public key length")
)

// ParsePublicKeyDomain parses an exact public-key domain. The boolean reports
// whether the name has the .pk.ygg suffix, including malformed candidates.
func ParsePublicKeyDomain(name string) (ed25519.PublicKey, bool, error) {
	if len(name) < len(PublicKeyDomainSuffix) || !strings.EqualFold(name[len(name)-len(PublicKeyDomainSuffix):], PublicKeyDomainSuffix) {
		return nil, false, nil
	}
	hexKey := name[:len(name)-len(PublicKeyDomainSuffix)]
	if len(hexKey) != publicKeyHexLength {
		return nil, true, fmt.Errorf("%w: expected %d hex characters, got %d", ErrInvalidPublicKeyLength, publicKeyHexLength, len(hexKey))
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %w", ErrInvalidPublicKeyDomain, err)
	}
	return ed25519.PublicKey(key), true, nil
}
