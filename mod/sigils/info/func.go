// Package info describes a node identity card stored in NodeInfo.
package info

import "errors"

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

// ParseParams returns the info keys present in NodeInfo.
func ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	for _, key := range sigKeys {
		if data, ok := NodeInfo[key]; ok {
			bufMap[key] = data
		}
	}
	return bufMap
}

// Match reports whether NodeInfo contains a valid identity card.
func Match(NodeInfo map[string]any) bool {
	conf, ok := parseConfig(NodeInfo)
	if !ok {
		return false
	}
	return validateConfig(&conf) == nil
}

// //

// Parse validates foreign NodeInfo and returns the parsed sigil.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	conf, ok := parseConfig(NodeInfo)
	if !ok {
		return nil, errors.New("info sigil not found or malformed")
	}
	return New(conf)
}
