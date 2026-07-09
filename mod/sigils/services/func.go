package services

import (
	"errors"
)

// // // // // // // // // //

func Name() string {
	return sigName
}

func Keys() []string {
	return append([]string(nil), sigKeys...)
}

// //

func ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[sigName]; ok {
		bufMap[sigName] = data
	}
	return bufMap
}

// Match expects map[string]any where each value is float64 (1–65535, integer).
func Match(NodeInfo map[string]any) bool {
	svc, ok := parseServices(NodeInfo)
	if !ok {
		return false
	}
	return validateServices(svc) == nil
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	svc, ok := parseServices(NodeInfo)
	if !ok {
		return nil, errors.New("services sigil not found or malformed")
	}
	return New(svc)
}
