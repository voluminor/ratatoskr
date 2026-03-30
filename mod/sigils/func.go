package sigils

import (
	"fmt"
	"regexp"
)

// // // // // // // // // //

// MergeParams copies nodeInfo and adds params on top.
// Returns error on key conflict. Never mutates the input.
func MergeParams(nodeInfo map[string]any, params map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(nodeInfo)+len(params))
	for k, v := range nodeInfo {
		out[k] = v
	}
	for k, v := range params {
		if _, ok := out[k]; ok {
			return nil, fmt.Errorf("conflict key: %s", k)
		}
		out[k] = v
	}
	return out, nil
}

// //

var reName = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

func ValidateName(name string) bool {
	return reName.MatchString(name)
}
