package info

import (
	"errors"
	"fmt"
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
	if _, ok := bufMap["name"]; !ok {
		return false
	}
	if _, ok := bufMap["type"]; !ok {
		return false
	}

	for key, data := range bufMap {
		switch key {
		case "name", "type", "location", "description":
			if _, ok := data.(string); !ok {
				return false
			}
		case "contact":
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

	parsed := ParseParams(NodeInfo)

	conf := ConfigObj{}

	if v, ok := parsed["name"].(string); ok {
		conf.Name = v
	}
	if v, ok := parsed["type"].(string); ok {
		conf.Type = v
	}
	if v, ok := parsed["location"].(string); ok {
		conf.Location = v
	}
	if v, ok := parsed["description"].(string); ok {
		conf.Description = v
	}

	if raw, ok := parsed["contact"].(map[string]any); ok {
		conf.Contacts = make(map[string][]string, len(raw))
		for group, v := range raw {
			arr, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("invalid contact group %s", group)
			}
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("invalid contact in group %s", group)
				}
				strs = append(strs, s)
			}
			conf.Contacts[group] = strs
		}
	}

	return &Obj{conf: &conf}, nil
}
