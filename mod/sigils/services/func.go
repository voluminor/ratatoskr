package services

import (
	"errors"
)

// // // // // // // // // //

func Name() string {
	return sigName
}

func Keys() []string {
	return sigKeys
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
	raw, ok := NodeInfo[sigName]
	if !ok {
		return false
	}

	svc, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if len(svc) == 0 {
		return false
	}

	for name, v := range svc {
		if !reServiceName.MatchString(name) {
			return false
		}
		port, ok := v.(float64)
		if !ok || port <= 0 || port > 65535 || port != float64(int(port)) {
			return false
		}
	}
	return true
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	if !Match(NodeInfo) {
		return nil, errors.New("services sigil not found or malformed")
	}

	raw := NodeInfo[sigName].(map[string]any)
	svc := make(map[string]uint16, len(raw))
	for name, v := range raw {
		svc[name] = uint16(v.(float64))
	}

	return &Obj{services: svc}, nil
}
