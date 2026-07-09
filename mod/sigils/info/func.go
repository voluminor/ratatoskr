package info

import "errors"

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
	for _, key := range sigKeys {
		if data, ok := NodeInfo[key]; ok {
			bufMap[key] = data
		}
	}
	return bufMap
}

// Match requires at least "name" and "type" as strings.
// "contact" must be map[string]any → []any → string.
func Match(NodeInfo map[string]any) bool {
	conf, ok := parseConfig(NodeInfo)
	if !ok {
		return false
	}
	return validateConfig(&conf) == nil
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	conf, ok := parseConfig(NodeInfo)
	if !ok {
		return nil, errors.New("info sigil not found or malformed")
	}
	return New(conf)
}
