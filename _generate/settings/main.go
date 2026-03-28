package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

//go:embed help.tmpl
var helpTmpl string

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

	// Trigger: CLI-only bool, excluded from config file serialization
	IsTrigger bool

	// Help display fields
	Group string // top-level branch key: "log", "ui", or "" for root-level flags
}

type EnumObj struct {
	TypeName  string   // e.g. "LogFormatEnum"
	Values    []string // e.g. ["text", "json"]
	GoConsts  []string // e.g. ["LogFormatText", "LogFormatJson"]
	ParseFunc string   // e.g. "ParseLogFormatEnum"
	NamesVar  string   // e.g. "logFormatEnumNames"
}

type TreeLeafObj struct {
	Name      string
	Type      string // Go type
	Key       string // original YAML key
	Usage     string // branch-level usage comment
	Branch    map[string]*TreeLeafObj
	IsEnum    bool
	IsArray   bool
	IsTrigger bool
	EnumType  string // "LogFormatEnum" if enum
}

type TemplateObj struct {
	GenerationTime  string
	Path            string
	Flags           []FlagObj
	Enums           []EnumObj
	Tree            map[string]*TreeLeafObj
	TypesImports    []string
	FlagsImports    []string
	HasCustomFlags  bool
	HasEnums        bool
	HasTriggerFlags bool
	HelpText        string
}

// //

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

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// hasChildNodes returns true if the YAML node contains nested setting nodes.
func hasChildNodes(node map[string]any) bool {
	for k, v := range node {
		if strings.HasPrefix(k, "_") {
			continue
		}
		switch k {
		case "type", "enum", "trigger", "usage", "value":
			continue
		}
		if _, ok := v.(map[string]any); ok {
			return true
		}
	}
	return false
}

// collectFlags recursively walks the YAML tree.
// A node with children is a branch; a leaf node defines a flag.
// inheritTrigger propagates trigger status from parent groups to all descendants.
func collectFlags(node map[string]any, prefix string, inheritTrigger bool) []FlagObj {
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

		_, hasTrigger := child["trigger"]

		if hasChildNodes(child) {
			result = append(result, collectFlags(child, fullKey, inheritTrigger || hasTrigger)...)
			continue
		}

		_, hasType := child["type"]
		_, hasEnum := child["enum"]
		isTrigger := hasTrigger || inheritTrigger

		if hasType || hasEnum || isTrigger {
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
					if len(enumSlice) == 0 {
						fmt.Println("Empty enum:", fullKey)
						return nil
					}
					for _, ev := range enumSlice {
						f.Enum = append(f.Enum, fmt.Sprint(ev))
					}
					f.IsEnum = true
				}
			}
			if isTrigger {
				f.IsTrigger = true
				if f.Type == "" {
					f.Type = "bool"
				}
			}
			f.IsArray = isArrayType(f.Type)
			result = append(result, f)
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

		if hasChildNodes(child) {
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
		s := fmt.Sprint(f.Value)
		if d, err := time.ParseDuration(s); err == nil {
			return fmt.Sprintf("time.Duration(%d)", int64(d))
		}
		return s
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

// propagateTrigger marks branch nodes as trigger when all their children are triggers.
func propagateTrigger(tree map[string]*TreeLeafObj) {
	for _, node := range tree {
		if node.Branch == nil {
			continue
		}
		propagateTrigger(node.Branch)
		allTrigger := true
		for _, child := range node.Branch {
			if !child.IsTrigger {
				allTrigger = false
				break
			}
		}
		if allTrigger {
			node.IsTrigger = true
		}
	}
}

// //

func buildHelpText(flags []FlagObj, tree map[string]*TreeLeafObj) string {
	var b strings.Builder

	// Collect groups in sorted order
	type groupObj struct {
		key   string
		title string
		usage string
		flags []FlagObj
	}

	groupMap := make(map[string]*groupObj)
	var groupOrder []string

	for _, f := range flags {
		gk := f.Group
		g, ok := groupMap[gk]
		if !ok {
			g = &groupObj{key: gk}
			if gk == "" {
				g.title = "General"
			} else {
				g.title = dep.GenGoName(gk)
				if branch, ok := tree[gk]; ok && branch.Usage != "" {
					g.usage = branch.Usage
				}
			}
			groupMap[gk] = g
			groupOrder = append(groupOrder, gk)
		}
		g.flags = append(g.flags, f)
	}

	// Sort groups: "" (General) first, then alphabetically
	sort.SliceStable(groupOrder, func(i, j int) bool {
		if groupOrder[i] == "" {
			return true
		}
		if groupOrder[j] == "" {
			return false
		}
		return groupOrder[i] < groupOrder[j]
	})

	// Find max flag name width for alignment
	maxWidth := 0
	for _, f := range flags {
		w := len(f.Name) + 1 // "-" prefix
		if w > maxWidth {
			maxWidth = w
		}
	}
	// Account for -h/-help and -i/-info
	if w := len("-help"); w > maxWidth {
		maxWidth = w
	}

	b.WriteString("Usage: <program> [flags]\n")

	for gi, gk := range groupOrder {
		g := groupMap[gk]

		if gi > 0 {
			b.WriteString("\n")
		}

		if g.usage != "" {
			fmt.Fprintf(&b, "\n%s (%s):\n", g.title, g.usage)
		} else {
			fmt.Fprintf(&b, "\n%s:\n", g.title)
		}

		// Add built-in flags to General group
		if gk == "" {
			fmt.Fprintf(&b, "  -%-*s  show this help message\n", maxWidth, "h, -help")
			fmt.Fprintf(&b, "  -%-*s  show application info\n", maxWidth, "i, -info")
		}

		// Regular flags first, then triggers
		for _, f := range g.flags {
			if f.IsTrigger {
				continue
			}
			var line strings.Builder
			fmt.Fprintf(&line, "  -%-*s  %s", maxWidth, f.Name, f.Usage)

			if f.IsEnum && len(f.Enum) > 0 {
				fmt.Fprintf(&line, " [%s]", strings.Join(f.Enum, ", "))
			}

			if f.Value != nil {
				fmt.Fprintf(&line, " (default: %v)", f.Value)
			}

			b.WriteString(line.String())
			b.WriteString("\n")
		}

		// Trigger flags with [trigger] marker
		hasTriggers := false
		for _, f := range g.flags {
			if !f.IsTrigger {
				continue
			}
			if !hasTriggers {
				b.WriteString("  ---\n")
				hasTriggers = true
			}
			fmt.Fprintf(&b, "  -%-*s  %s [trigger]\n", maxWidth, f.Name, f.Usage)
		}
	}

	return b.String()
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

	flags := collectFlags(parseTree, "", false)
	branchUsage := collectBranchUsage(parseTree, "")
	tree := make(map[string]*TreeLeafObj)
	var enums []EnumObj
	enumMap := make(map[string]*EnumObj)
	hasCustomFlags := false
	hasTriggerFlags := false

	typesImports := map[string]bool{}
	flagsImports := map[string]bool{"flag": true}

	// Build enums — deduplicate identical value sets
	enumByValues := make(map[string]*EnumObj) // key: joined values
	for i := range flags {
		f := &flags[i]
		if !f.IsEnum {
			continue
		}

		valKey := strings.Join(f.Enum, "\x00")
		if existing, ok := enumByValues[valKey]; ok {
			enumMap[f.Name] = existing
			continue
		}

		typeName := buildEnumTypeName(f.Name)
		consts := make([]string, 0, len(f.Enum))
		for _, v := range f.Enum {
			consts = append(consts, buildEnumConstName(typeName, v))
		}
		e := EnumObj{
			TypeName:  typeName,
			Values:    slices.Clone(f.Enum),
			GoConsts:  consts,
			ParseFunc: "Parse" + typeName,
			NamesVar:  lowerFirst(typeName) + "Names",
		}
		enums = append(enums, e)
		enumByValues[valKey] = &enums[len(enums)-1]
		enumMap[f.Name] = &enums[len(enums)-1]
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
		if parts := strings.SplitN(f.Name, ".", 2); len(parts) > 1 {
			f.Group = parts[0]
		}

		bt := f.Type
		if f.IsArray {
			bt = baseType(bt)
		}
		if bt == "duration" {
			typesImports["time"] = true
			if f.IsArray {
				flagsImports["time"] = true
			}
		}
		if isCustomFlag(bt, f.IsEnum, f.IsArray) {
			hasCustomFlags = true
		}
		if f.IsArray {
			flagsImports["strings"] = true
		}
		if f.IsTrigger {
			hasTriggerFlags = true
		}
		if !f.IsEnum && !f.IsArray && !nativeFlagTypes[f.Type] {
			flagsImports["fmt"] = true
			flagsImports["strconv"] = true
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
				Name:      dep.GenGoName(points[0]),
				Type:      f.GoType,
				Key:       points[0],
				IsEnum:    f.IsEnum,
				IsArray:   f.IsArray,
				IsTrigger: f.IsTrigger,
				EnumType:  enumTypeName,
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
				Name:      dep.GenGoName(points[1]),
				Type:      f.GoType,
				Key:       points[1],
				IsEnum:    f.IsEnum,
				IsArray:   f.IsArray,
				IsTrigger: f.IsTrigger,
				EnumType:  enumTypeName,
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
				Name:      dep.GenGoName(points[2]),
				Type:      f.GoType,
				Key:       points[2],
				IsEnum:    f.IsEnum,
				IsArray:   f.IsArray,
				IsTrigger: f.IsTrigger,
				EnumType:  enumTypeName,
			}
		default:
			fmt.Println("The settings tree is too deep! Can't go deeper than 3\t", f.Name)
			return
		}
	}

	propagateTrigger(tree)

	data := TemplateObj{
		GenerationTime:  time.Now().Format(time.RFC3339),
		Flags:           flags,
		Enums:           enums,
		Tree:            tree,
		TypesImports:    sortedKeys(typesImports),
		FlagsImports:    sortedKeys(flagsImports),
		HasCustomFlags:  hasCustomFlags,
		HasEnums:        len(enums) > 0,
		HasTriggerFlags: hasTriggerFlags,
		HelpText:        buildHelpText(flags, tree),
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
		{"help.go", helpTmpl, false},
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
