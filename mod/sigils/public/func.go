package public

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

// Match expects map[string]any where each value is []any of strings.
func Match(NodeInfo map[string]any) bool {
	peers, ok := parsePeers(NodeInfo)
	if !ok {
		return false
	}
	return validatePeers(peers) == nil
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	peers, ok := parsePeers(NodeInfo)
	if !ok {
		return nil, errors.New("public sigil not found or malformed")
	}
	return New(peers)
}
