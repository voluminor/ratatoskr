package info

import "errors"

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
	bufMap := ParseParams(NodeInfo)
	if len(bufMap) < 2 {
		return false
	}
	if _, ok := bufMap[keyName]; !ok {
		return false
	}
	if _, ok := bufMap[keyType]; !ok {
		return false
	}

	for key, data := range bufMap {
		switch key {
		case keyName, keyType, keyLocation, keyDescription:
			if _, ok := data.(string); !ok {
				return false
			}
		case keyContact:
			m, ok := data.(map[string]any)
			if !ok {
				return false
			}
			for _, v := range m {
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
		}
	}
	return true
}

// //

// Parse creates an Obj from foreign NodeInfo.
func Parse(NodeInfo map[string]any) (*Obj, error) {
	if !Match(NodeInfo) {
		return nil, errors.New("info sigil not found or malformed")
	}
	o := &Obj{conf: &ConfigObj{}}
	o.ParseParams(NodeInfo)
	return o, nil
}
