package inet

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

// Match expects []any of strings.
func Match(NodeInfo map[string]any) bool {
	addrs, ok := parseAddrs(NodeInfo)
	if !ok {
		return false
	}
	return validateAddrs(addrs) == nil
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	addrs, ok := parseAddrs(NodeInfo)
	if !ok {
		return nil, errors.New("inet sigil not found or malformed")
	}
	return New(addrs)
}
