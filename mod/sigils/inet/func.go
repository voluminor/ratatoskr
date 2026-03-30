package inet

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

// Match expects []any of strings.
func Match(NodeInfo map[string]any) bool {
	raw, ok := NodeInfo[sigName]
	if !ok {
		return false
	}

	arr, ok := raw.([]any)
	if !ok {
		return false
	}
	if len(arr) == 0 {
		return false
	}

	for _, item := range arr {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	if !Match(NodeInfo) {
		return nil, errors.New("inet sigil not found or malformed")
	}
	o := &Obj{}
	o.ParseParams(NodeInfo)
	return o, nil
}
