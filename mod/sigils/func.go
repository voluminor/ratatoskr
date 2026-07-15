// Package sigils defines typed, cloneable fragments of Yggdrasil NodeInfo.
package sigils

import (
	"fmt"
	"regexp"
)

// // // // // // // // // //

// MergeParams returns a shallow copy containing nodeInfo and params. A key
// conflict returns an error without mutating either input map.
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

// ValidateName reports whether name contains 3 to 32 lowercase sigil-name characters.
func ValidateName(name string) bool {
	return reName.MatchString(name)
}
