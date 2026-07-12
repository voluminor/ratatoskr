package peermgr

import (
	"fmt"
	"net/url"
	"strings"
)

// // // // // // // // // //

// peerEntryObj — validated peer: original URI + transport scheme
type peerEntryObj struct {
	URI      string
	Scheme   string
	MatchURI string
}

func normalizePeerURL(u *url.URL) string {
	v := *u
	v.Scheme = strings.ToLower(v.Scheme)
	v.Host = strings.ToLower(v.Host)
	v.RawQuery = ""
	v.ForceQuery = false
	v.Fragment = ""
	return v.String()
}

func normalizePeerURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return normalizePeerURL(u)
}

// ValidatePeers validates an array of URI strings:
// empty strings are skipped; valid peers are deduplicated by normalized URI.
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

		u, err := url.Parse(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w %q: %w", ErrInvalidURI, normalizePeerURI(s), err))
			continue
		}
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)

		// Scheme and host are not validated here: the scheme is only used to group
		// peers per protocol, and node.AddPeer rejects and logs genuinely bad URIs
		// at probe time. Only malformed URIs (url.Parse failure) and duplicates fail.
		matchURI := normalizePeerURL(u)
		if seen[matchURI] {
			errs = append(errs, fmt.Errorf("%w %q", ErrDuplicatePeer, matchURI))
			continue
		}
		seen[matchURI] = true

		result = append(result, peerEntryObj{URI: u.String(), Scheme: u.Scheme, MatchURI: matchURI})
	}

	return result, errs
}
