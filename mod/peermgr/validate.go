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
	// Drop userinfo alongside the query: it is not part of the peer identity
	// (yggdrasil carries peer secrets in the query, which is also stripped) and
	// this form feeds both the dedup key and log/error output.
	v.User = nil
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

		// Keep transport schemes open-ended for future Yggdrasil versions, but
		// require the URI structure every peer transport needs: a scheme and either
		// an authority or a path (for transports such as unix://).
		if u.Scheme == "" || (u.Host == "" && u.Path == "") {
			errs = append(errs, fmt.Errorf("%w %q", ErrInvalidURI, normalizePeerURI(s)))
			continue
		}
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
