package public

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

// Match expects map[string]any where each value is []any of strings.
func Match(NodeInfo map[string]any) bool {
	raw, ok := NodeInfo[sigName]
	if !ok {
		return false
	}

	peers, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if len(peers) == 0 {
		return false
	}

	for group, v := range peers {
		if !reGroup.MatchString(group) {
			return false
		}
		arr, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range arr {
			if _, ok := item.(string); !ok {
				return false
			}
		}
	}
	return true
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	if !Match(NodeInfo) {
		return nil, errors.New("public sigil not found or malformed")
	}

	raw := NodeInfo[sigName].(map[string]any)
	peers := make(map[string][]string, len(raw))

	for group, v := range raw {
		arr := v.([]any)
		strs := make([]string, 0, len(arr))
		for _, item := range arr {
			strs = append(strs, item.(string))
		}
		peers[group] = strs
	}

	return &Obj{peers: peers}, nil
}
