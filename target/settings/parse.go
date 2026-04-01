// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hjson/hjson-go/v4"
	"gopkg.in/yaml.v3"
)

// // // // // // // // // //

// ParseJSON unmarshals JSON data into the given Obj.
func ParseJSON(data []byte, obj *Obj) error {
	return parseDuration(data, obj, json.Unmarshal)
}

// ParseYAML unmarshals YAML data into the given Obj.
func ParseYAML(data []byte, obj *Obj) error {
	return parseDuration(data, obj, yaml.Unmarshal)
}

// ParseHJSON unmarshals HJSON data into the given Obj.
func ParseHJSON(data []byte, obj *Obj) error {
	return parseDuration(data, obj, hjson.Unmarshal)
}

// //

// parseDuration decodes raw data into a generic map, normalizes
// duration fields to nanosecond integers, re-encodes as JSON,
// and then unmarshals into the typed Obj.
func parseDuration(data []byte, obj *Obj, decode func([]byte, any) error) error {
	var raw map[string]any
	if err := decode(data, &raw); err != nil {
		return err
	}

	normalizeDurations(raw, "")

	normalized, err := json.Marshal(raw)
	if err != nil {
		return err
	}

	return json.Unmarshal(normalized, obj)
}

// //

// normalizeDurations recursively walks a map and converts duration
// values at known paths to nanosecond integers (int64).
func normalizeDurations(m map[string]any, prefix string) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		if DurationKeys[path] {
			m[k] = coerceDuration(v)
			continue
		}

		switch val := v.(type) {
		case map[string]any:
			normalizeDurations(val, path)
		case []any:
			for _, item := range val {
				if sub, ok := item.(map[string]any); ok {
					normalizeDurations(sub, path)
				}
			}
		}
	}
}

// //

// coerceDuration converts a duration value (string, float64, int, or nil)
// to nanosecond int64.
func coerceDuration(v any) int64 {
	switch val := v.(type) {
	case string:
		if d, err := time.ParseDuration(val); err == nil {
			return int64(d)
		}
		return 0
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	case json.Number:
		if n, err := val.Int64(); err == nil {
			return n
		}
		if f, err := val.Float64(); err == nil {
			return int64(f)
		}
		return 0
	case nil:
		return 0
	default:
		s := fmt.Sprint(val)
		if d, err := time.ParseDuration(s); err == nil {
			return int64(d)
		}
		return 0
	}
}
