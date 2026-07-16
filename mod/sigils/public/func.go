// Package public describes public peering endpoints grouped by transport.
package public

import (
	"errors"
)

// // // // // // // // // //

// Name returns the sigil identifier.
func Name() string {
	return sigName
}

// Keys returns the owned NodeInfo keys.
func Keys() []string {
	return append([]string(nil), sigKeys...)
}

// //

// ParseParams returns the public fragment present in NodeInfo.
func ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[sigName]; ok {
		bufMap[sigName] = data
	}
	return bufMap
}

// Match reports whether NodeInfo contains valid grouped peering URIs.
func Match(NodeInfo map[string]any) bool {
	peers, ok := parsePeers(NodeInfo)
	if !ok {
		return false
	}
	return validatePeers(peers) == nil
}

// //

// Parse validates foreign NodeInfo and returns the parsed sigil.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	peers, ok := parsePeers(NodeInfo)
	if !ok {
		return nil, errors.New("public sigil not found or malformed")
	}
	return New(peers)
}
