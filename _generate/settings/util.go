package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	dep "github.com/voluminor/ratatoskr/_generate"
)

// // // // // // // // // //

var nativeFlagTypes = map[string]bool{
	"string":   true,
	"bool":     true,
	"int":      true,
	"int64":    true,
	"uint":     true,
	"uint64":   true,
	"float64":  true,
	"duration": true,
}

// //

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isArrayType(t string) bool  { return strings.HasPrefix(t, "[]") }
func isMapType(t string) bool    { return strings.HasPrefix(t, "map[") }
func baseType(t string) string   { return strings.TrimPrefix(t, "[]") }
func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

// //

// goType maps YAML type names to Go type names.
func goType(t string) string {
	switch t {
	case "duration":
		return "time.Duration"
	case "float":
		return "float32"
	default:
		return t
	}
}

// goTypeResolved produces the full Go type string for a flag.
func goTypeResolved(t string, isEnum bool, enumTypeName string, isArray bool) string {
	if isEnum {
		if isArray {
			return "[]" + enumTypeName
		}
		return enumTypeName
	}
	if isArray {
		return "[]" + goType(baseType(t))
	}
	return goType(t)
}

// isCustomFlag returns true for flags that need custom parsing (not native to flag package).
func isCustomFlag(t string, isEnum, isArray bool) bool {
	return isEnum || isArray || !nativeFlagTypes[t]
}

// //

// buildEnumTypeName converts "log.format" → "LogFormatEnum".
func buildEnumTypeName(flagName string) string {
	parts := strings.Split(flagName, ".")
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(dep.GenGoName(p))
	}
	sb.WriteString("Enum")
	return sb.String()
}

// buildEnumConstName converts type "LogFormatEnum" + value "text" → "LogFormatText".
func buildEnumConstName(typeName, value string) string {
	return strings.TrimSuffix(typeName, "Enum") + dep.GenGoName(value)
}

// buildFlagAccessor converts "log.format" → "Log.Format".
func buildFlagAccessor(flagName string) string {
	parts := strings.Split(flagName, ".")
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString(".")
		}
		sb.WriteString(dep.GenGoName(p))
	}
	return sb.String()
}

// //

// goDefaultLiteral returns a Go source literal for the flag's default value.
func goDefaultLiteral(f FlagObj, enumTypeName string) string {
	if f.Value == nil {
		return zeroLiteral(f)
	}
	if f.IsEnum {
		return enumDefaultLiteral(f, enumTypeName)
	}
	if f.IsArray {
		return arrayDefaultLiteral(f)
	}
	return scalarDefaultLiteral(f)
}

func zeroLiteral(f FlagObj) string {
	if f.IsArray || f.IsMap {
		return "nil"
	}
	switch f.Type {
	case "string":
		return `""`
	case "bool":
		return "false"
	default:
		return "0"
	}
}

func enumDefaultLiteral(f FlagObj, enumTypeName string) string {
	if f.IsArray {
		arr, ok := f.Value.([]any)
		if !ok {
			return "nil"
		}
		parts := make([]string, 0, len(arr))
		for _, v := range arr {
			parts = append(parts, buildEnumConstName(enumTypeName, fmt.Sprint(v)))
		}
		return "[]" + enumTypeName + "{" + strings.Join(parts, ", ") + "}"
	}
	return buildEnumConstName(enumTypeName, fmt.Sprint(f.Value))
}

func arrayDefaultLiteral(f FlagObj) string {
	arr, ok := f.Value.([]any)
	if !ok {
		return "nil"
	}
	bt := goType(baseType(f.Type))
	parts := make([]string, 0, len(arr))
	for _, v := range arr {
		if bt == "string" {
			parts = append(parts, fmt.Sprintf("%q", fmt.Sprint(v)))
		} else {
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return "[]" + bt + "{" + strings.Join(parts, ", ") + "}"
}

func scalarDefaultLiteral(f FlagObj) string {
	switch f.Type {
	case "string":
		return fmt.Sprintf("%q", fmt.Sprint(f.Value))
	case "bool":
		v := strings.ToLower(fmt.Sprint(f.Value))
		if v == "true" || v == "1" || v == "yes" {
			return "true"
		}
		return "false"
	case "float32", "float", "float64":
		return fmt.Sprint(f.Value)
	case "duration":
		s := fmt.Sprint(f.Value)
		if d, err := time.ParseDuration(s); err == nil {
			return fmt.Sprintf("time.Duration(%d)", int64(d))
		}
		return s
	default:
		return fmt.Sprint(f.Value)
	}
}
