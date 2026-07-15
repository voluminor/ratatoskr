package peermgr

import (
	"fmt"
	"net/url"
	"strings"
)

// // // // // // // // // //

// PeerEntryObj is a validated peer candidate.
type PeerEntryObj struct {
	// URI preserves the connection URI, including transport options.
	URI string
	// Scheme groups candidates for per-protocol selection.
	Scheme string
	// MatchURI is the normalized, credential-free peer identity.
	MatchURI string
}

func normalizePeerURL(u *url.URL) string {
	v := *u
	v.Scheme = strings.ToLower(v.Scheme)
	v.Host = strings.ToLower(v.Host)
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

// ValidatePeers validates, normalizes, and deduplicates peer URIs in input order.
func ValidatePeers(peers []string) ([]PeerEntryObj, []error) {
	var errs []error
	result := make([]PeerEntryObj, 0, len(peers))
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

		result = append(result, PeerEntryObj{URI: u.String(), Scheme: u.Scheme, MatchURI: matchURI})
	}

	return result, errs
}
