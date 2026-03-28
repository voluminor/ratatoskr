package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dep "github.com/voluminor/ratatoskr/_generate"
	"gopkg.in/yaml.v3"
)

// // // // // // // // // //

//go:embed types.tmpl
var typesTmpl string

//go:embed enums.tmpl
var enumsTmpl string

//go:embed defaults.tmpl
var defaultsTmpl string

//go:embed flags.tmpl
var flagsTmpl string

//go:embed parse.tmpl
var parseTmpl string

//go:embed save.tmpl
var saveTmpl string

//go:embed init.tmpl
var initTmpl string

// //

type FlagObj struct {
	Name    string
	Type    string
	Value   any
	Usage   string
	Enum    []string
	IsArray bool
	IsEnum  bool

	// Go-resolved fields (filled during tree building)
	GoType       string // resolved Go type: "string", "LogFormatEnum", "[]string", etc.
	GoDefault    string // Go literal for default value
	FlagAccessor string // dot path into Obj: "Log.Format"
	EnumType     string // "LogFormatEnum" if IsEnum, else ""
}

type EnumObj struct {
	TypeName  string   // e.g. "LogFormatEnum"
	Values    []string // e.g. ["text", "json"]
	GoConsts  []string // e.g. ["LogFormatText", "LogFormatJson"]
	ParseFunc string   // e.g. "ParseLogFormatEnum"
	NamesVar  string   // e.g. "logFormatEnumNames"
}

type TreeLeafObj struct {
	Name     string
	Type     string // Go type
	Key      string // original YAML key
	Usage    string // branch-level usage comment
	Branch   map[string]*TreeLeafObj
	IsEnum   bool
	IsArray  bool
	EnumType string // "LogFormatEnum" if enum
}

type TemplateObj struct {
	GenerationTime          string
	Path                    string
	Flags                   []FlagObj
	Enums                   []EnumObj
	Tree                    map[string]*TreeLeafObj
	HasDuration             bool
	HasCustomFlags          bool
	HasEnums                bool
	HasArrayFlags           bool
	HasNonNativeScalarFlags bool
}

// //

// nativeFlagTypes lists types supported by stdlib flag package directly.
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

func isArrayType(t string) bool {
	return strings.HasPrefix(t, "[]")
}

func baseType(t string) string {
	return strings.TrimPrefix(t, "[]")
}

func goType(t string) string {
	if t == "duration" {
		return "time.Duration"
	}
	if t == "float" {
		return "float32"
	}
	return t
}

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

func isCustomFlag(t string, isEnum, isArray bool) bool {
	if isEnum || isArray {
		return true
	}
	return !nativeFlagTypes[t]
}

// //

// collectFlags recursively walks the YAML tree.
// A node with a "type" key is a flag definition; otherwise it is a branch.
func collectFlags(node map[string]any, prefix string) []FlagObj {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []FlagObj

	for _, k := range keys {
		if strings.HasPrefix(k, "_") {
			continue
		}

		v := node[k]
		child, ok := v.(map[string]any)
		if !ok {
			continue
		}

		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}

		_, hasType := child["type"]
		_, hasEnum := child["enum"]

		if hasType || hasEnum {
			f := FlagObj{
				Name: fullKey,
			}
			if hasType {
				f.Type = fmt.Sprint(child["type"])
			}
			if val, ok := child["value"]; ok {
				f.Value = val
			}
			if usage, ok := child["usage"]; ok {
				f.Usage = fmt.Sprint(usage)
			}
			if hasEnum {
				if enumSlice, ok := child["enum"].([]any); ok {
					for _, ev := range enumSlice {
						f.Enum = append(f.Enum, fmt.Sprint(ev))
					}
					f.IsEnum = true
				}
			}
			f.IsArray = isArrayType(f.Type)
			result = append(result, f)
		} else {
			result = append(result, collectFlags(child, fullKey)...)
		}
	}

	return result
}

// collectBranchUsage extracts "usage" values from branch (non-leaf) YAML nodes.
// Returns a map of dot-separated paths to usage strings.
func collectBranchUsage(node map[string]any, prefix string) map[string]string {
	result := make(map[string]string)

	for k, v := range node {
		if strings.HasPrefix(k, "_") {
			continue
		}

		child, ok := v.(map[string]any)
		if !ok {
			continue
		}

		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}

		// Branch node: no "type" and no "enum" key
		_, hasType := child["type"]
		_, hasEnum := child["enum"]
		if !hasType && !hasEnum {
			if usage, ok := child["usage"]; ok {
				result[fullKey] = fmt.Sprint(usage)
			}
			for mk, mv := range collectBranchUsage(child, fullKey) {
				result[mk] = mv
			}
		}
	}

	return result
}

// //

// buildEnumTypeName creates an enum type name from dot-separated flag path.
// "log.format" → "LogFormatEnum"
func buildEnumTypeName(flagName string) string {
	parts := strings.Split(flagName, ".")
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(dep.GenGoName(p))
	}
	sb.WriteString("Enum")
	return sb.String()
}

// buildEnumConstName creates a constant name: "LogFormatText" from type "LogFormatEnum" + value "text"
func buildEnumConstName(typeName, value string) string {
	base := strings.TrimSuffix(typeName, "Enum")
	return base + dep.GenGoName(value)
}

func lowerFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// //

func goDefaultLiteral(f FlagObj, enumTypeName string) string {
	if f.Value == nil {
		if f.IsArray {
			return "nil"
		}
		switch f.Type {
		case "string":
			return `""`
		case "bool":
			return "false"
		case "duration":
			return "0"
		default:
			return "0"
		}
	}

	if f.IsEnum {
		valStr := fmt.Sprint(f.Value)
		if f.IsArray {
			if arr, ok := f.Value.([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, v := range arr {
					parts = append(parts, buildEnumConstName(enumTypeName, fmt.Sprint(v)))
				}
				return "[]" + enumTypeName + "{" + strings.Join(parts, ", ") + "}"
			}
			return "nil"
		}
		return buildEnumConstName(enumTypeName, valStr)
	}

	if f.IsArray {
		bt := goType(baseType(f.Type))
		if arr, ok := f.Value.([]any); ok {
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
		return "nil"
	}

	switch f.Type {
	case "string":
		return fmt.Sprintf("%q", fmt.Sprint(f.Value))
	case "bool":
		v := strings.ToLower(fmt.Sprint(f.Value))
		if v == "true" || v == "1" || v == "yes" {
			return "true"
		}
		return "false"
	case "float32", "float":
		return fmt.Sprint(f.Value)
	case "float64":
		return fmt.Sprint(f.Value)
	case "duration":
		return fmt.Sprint(f.Value)
	default:
		return fmt.Sprint(f.Value)
	}
}

// //

func buildFlagAccessor(flagName string) string {
	parts := strings.Split(flagName, ".")
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(dep.GenGoName(p))
		sb.WriteString(".")
	}
	s := sb.String()
	return strings.TrimSuffix(s, ".")
}

// //

func main() {
	flag.Parse()

	var subDir string
	settingsFile := "settings.yml"

	if flag.NArg() > 0 {
		subDir = filepath.Clean(flag.Arg(0))
		settingsFile = filepath.Join(subDir, "settings.yml")
	}

	raw, err := os.ReadFile(settingsFile)
	if err != nil {
		fmt.Println("Error reading settings.yml:", err)
		return
	}

	var parseTree map[string]any
	if err = yaml.Unmarshal(raw, &parseTree); err != nil {
		fmt.Println("Error parsing settings.yml:", err)
		return
	}

	flags := collectFlags(parseTree, "")
	branchUsage := collectBranchUsage(parseTree, "")
	tree := make(map[string]*TreeLeafObj)
	var enums []EnumObj
	enumMap := make(map[string]*EnumObj)
	hasDuration := false
	hasCustomFlags := false
	hasArrayFlags := false
	hasNonNativeScalarFlags := false

	// Build enums first
	for i := range flags {
		f := &flags[i]
		if f.IsEnum {
			typeName := buildEnumTypeName(f.Name)
			consts := make([]string, 0, len(f.Enum))
			for _, v := range f.Enum {
				consts = append(consts, buildEnumConstName(typeName, v))
			}
			e := EnumObj{
				TypeName:  typeName,
				Values:    f.Enum,
				GoConsts:  consts,
				ParseFunc: "Parse" + typeName,
				NamesVar:  lowerFirst(typeName) + "Names",
			}
			enums = append(enums, e)
			enumMap[f.Name] = &enums[len(enums)-1]
		}
	}

	// Resolve Go types and defaults
	for i := range flags {
		f := &flags[i]
		enumTypeName := ""
		if f.IsEnum {
			enumTypeName = enumMap[f.Name].TypeName
		}

		f.GoType = goTypeResolved(f.Type, f.IsEnum, enumTypeName, f.IsArray)
		f.GoDefault = goDefaultLiteral(*f, enumTypeName)
		f.FlagAccessor = buildFlagAccessor(f.Name)
		f.EnumType = enumTypeName

		bt := f.Type
		if f.IsArray {
			bt = baseType(bt)
		}
		if bt == "duration" {
			hasDuration = true
		}
		if isCustomFlag(bt, f.IsEnum, f.IsArray) {
			hasCustomFlags = true
		}
		if f.IsArray {
			hasArrayFlags = true
		}
		if !f.IsEnum && !f.IsArray && !nativeFlagTypes[f.Type] {
			hasNonNativeScalarFlags = true
		}

		if f.Type == "" && !f.IsEnum {
			fmt.Println("Missing type:", f.Name)
			return
		}

		// Build tree
		points := strings.Split(f.Name, ".")
		switch len(points) {
		case 1:
			tree[points[0]] = &TreeLeafObj{
				Name:     dep.GenGoName(points[0]),
				Type:     f.GoType,
				Key:      points[0],
				IsEnum:   f.IsEnum,
				IsArray:  f.IsArray,
				EnumType: enumTypeName,
			}
		case 2:
			branch, ok := tree[points[0]]
			if !ok {
				branch = &TreeLeafObj{
					Branch: make(map[string]*TreeLeafObj),
					Name:   dep.GenGoName(points[0]),
					Type:   dep.GenGoName(points[0]) + "Obj",
					Key:    points[0],
					Usage:  branchUsage[points[0]],
				}
				tree[points[0]] = branch
			}
			branch.Branch[points[1]] = &TreeLeafObj{
				Name:     dep.GenGoName(points[1]),
				Type:     f.GoType,
				Key:      points[1],
				IsEnum:   f.IsEnum,
				IsArray:  f.IsArray,
				EnumType: enumTypeName,
			}
		case 3:
			branch, ok := tree[points[0]]
			if !ok {
				branch = &TreeLeafObj{
					Branch: make(map[string]*TreeLeafObj),
					Name:   dep.GenGoName(points[0]),
					Type:   dep.GenGoName(points[0]) + "Obj",
					Key:    points[0],
					Usage:  branchUsage[points[0]],
				}
				tree[points[0]] = branch
			}
			leaf, ok := branch.Branch[points[1]]
			if !ok {
				leaf = &TreeLeafObj{
					Branch: make(map[string]*TreeLeafObj),
					Name:   dep.GenGoName(points[1]),
					Type:   dep.GenGoName(points[1]) + "Obj",
					Key:    points[1],
					Usage:  branchUsage[points[0]+"."+points[1]],
				}
				branch.Branch[points[1]] = leaf
			}
			leaf.Branch[points[2]] = &TreeLeafObj{
				Name:     dep.GenGoName(points[2]),
				Type:     f.GoType,
				Key:      points[2],
				IsEnum:   f.IsEnum,
				IsArray:  f.IsArray,
				EnumType: enumTypeName,
			}
		default:
			fmt.Println("The settings tree is too deep! Can't go deeper than 3\t", f.Name)
			return
		}
	}

	data := TemplateObj{
		GenerationTime:          time.Now().Format(time.RFC3339),
		Flags:                   flags,
		Enums:                   enums,
		Tree:                    tree,
		HasDuration:             hasDuration,
		HasCustomFlags:          hasCustomFlags,
		HasEnums:                len(enums) > 0,
		HasArrayFlags:           hasArrayFlags,
		HasNonNativeScalarFlags: hasNonNativeScalarFlags,
	}

	outDir := filepath.Join("target", "settings")
	if subDir != "" {
		outDir = filepath.Join("target", subDir, "settings")
		data.Path = subDir
	}

	if err = os.MkdirAll(outDir, 0755); err != nil {
		fmt.Println("Error creating directory:", err)
		return
	}

	templates := []struct {
		file string
		tmpl string
		skip bool
	}{
		{"types.go", typesTmpl, false},
		{"enums.go", enumsTmpl, !data.HasEnums},
		{"defaults.go", defaultsTmpl, false},
		{"flags.go", flagsTmpl, false},
		{"parse.go", parseTmpl, false},
		{"save.go", saveTmpl, false},
		{"init.go", initTmpl, false},
	}

	for _, t := range templates {
		if t.skip {
			continue
		}
		err = dep.WriteFileFromTemplate(
			filepath.Join(outDir, t.file),
			t.tmpl,
			data,
		)
		if err != nil {
			fmt.Printf("Error generating %s: %s\n", t.file, err)
			return
		}
	}
}
