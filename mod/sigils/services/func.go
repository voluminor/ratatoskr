// Package services describes ports exposed inside the Yggdrasil network.
package services

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

// ParseParams returns the services fragment present in NodeInfo.
func ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[sigName]; ok {
		bufMap[sigName] = data
	}
	return bufMap
}

// Match reports whether NodeInfo contains valid service names and ports.
func Match(NodeInfo map[string]any) bool {
	svc, ok := parseServices(NodeInfo)
	if !ok {
		return false
	}
	return validateServices(svc) == nil
}

// //

// Parse validates foreign NodeInfo and returns the parsed sigil.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	svc, ok := parseServices(NodeInfo)
	if !ok {
		return nil, errors.New("services sigil not found or malformed")
	}
	return New(svc)
}
