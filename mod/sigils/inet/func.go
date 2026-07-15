// Package inet describes public Internet addresses associated with a node.
package inet

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

// ParseParams returns the inet fragment present in NodeInfo.
func ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[sigName]; ok {
		bufMap[sigName] = data
	}
	return bufMap
}

// Match reports whether NodeInfo contains a valid inet address list.
func Match(NodeInfo map[string]any) bool {
	addrs, ok := parseAddrs(NodeInfo)
	if !ok {
		return false
	}
	return validateAddrs(addrs) == nil
}

// //

// Parse validates foreign NodeInfo and returns the parsed sigil.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	addrs, ok := parseAddrs(NodeInfo)
	if !ok {
		return nil, errors.New("inet sigil not found or malformed")
	}
	return New(addrs)
}
