package peermgr

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// // // // // // // // // //

// Allowed Yggdrasil transport schemes
var AllowedSchemes = []string{"tcp", "tls", "quic", "ws", "wss"}

// peerEntryObj — validated peer: original URI + transport scheme
type peerEntryObj struct {
	URI    string
	Scheme string
}

// ValidatePeers validates an array of URI strings:
// empty strings are skipped, duplicates → error, then URI parsing and scheme validation.
// Order of valid entries is preserved
func ValidatePeers(peers []string) ([]peerEntryObj, []error) {
	var errs []error
	result := make([]peerEntryObj, 0, len(peers))
	seen := make(map[string]bool, len(peers))

	for _, raw := range peers {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		if seen[s] {
			errs = append(errs, fmt.Errorf("duplicate peer %q", s))
			continue
		}
		seen[s] = true

		u, err := url.Parse(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid URI %q: %w", s, err))
			continue
		}

		if u.Host == "" {
			errs = append(errs, fmt.Errorf("missing host in %q", s))
			continue
		}

		if !slices.Contains(AllowedSchemes, u.Scheme) {
			errs = append(errs, fmt.Errorf("unsupported scheme %q in %q, allowed: %v", u.Scheme, s, AllowedSchemes))
			continue
		}

		result = append(result, peerEntryObj{URI: u.String(), Scheme: u.Scheme})
	}

	return result, errs
}
