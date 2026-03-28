package main

import (
	"fmt"
	"sort"
	"strings"
)

// // // // // // // // // //

// WalkResultObj holds everything extracted from a single YAML tree traversal.
type WalkResultObj struct {
	Flags         []FlagObj
	BranchUsage   map[string]string
	GenIfacePaths map[string]bool
}

// //

// WalkYAML traverses the YAML schema tree once, collecting flags, branch usage strings,
// and gen_interface markers that would otherwise require three separate passes.
func WalkYAML(node map[string]any) WalkResultObj {
	r := WalkResultObj{
		BranchUsage:   make(map[string]string),
		GenIfacePaths: make(map[string]bool),
	}
	walkNode(node, "", false, &r)
	return r
}

// //

// walkNode recursively processes a YAML node.
// Branch nodes contribute usage and gen_interface markers; leaf nodes produce flags.
func walkNode(node map[string]any, prefix string, inheritTrigger bool, r *WalkResultObj) {
	keys := sortedNodeKeys(node)

	for _, k := range keys {
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
			// Branch node: collect usage and gen_interface, then recurse
			if usage, ok := child["usage"]; ok {
				r.BranchUsage[fullKey] = fmt.Sprint(usage)
			}
			if gi, ok := child["gen_interface"].(bool); ok && gi {
				r.GenIfacePaths[fullKey] = true
			}
			walkNode(child, fullKey, inheritTrigger || hasTrigger, r)
			continue
		}

		// Leaf node: build a flag
		if f, ok := buildFlag(child, fullKey, hasTrigger || inheritTrigger); ok {
			r.Flags = append(r.Flags, f)
		}
	}
}

// //

// hasChildNodes returns true if the YAML node contains nested setting nodes.
func hasChildNodes(node map[string]any) bool {
	for k, v := range node {
		if strings.HasPrefix(k, "_") {
			continue
		}
		switch k {
		case "type", "enum", "trigger", "usage", "value", "gen_interface":
			continue
		}
		if _, ok := v.(map[string]any); ok {
			return true
		}
	}
	return false
}

// buildFlag constructs a FlagObj from a leaf YAML node.
// Returns false if the node doesn't define a valid flag.
func buildFlag(child map[string]any, fullKey string, isTrigger bool) (FlagObj, bool) {
	_, hasType := child["type"]
	_, hasEnum := child["enum"]

	if !hasType && !hasEnum && !isTrigger {
		return FlagObj{}, false
	}

	f := FlagObj{Name: fullKey}

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
				return FlagObj{}, false
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
	f.IsMap = isMapType(f.Type)

	if f.Type == "" && !f.IsEnum {
		fmt.Println("Missing type:", fullKey)
		return FlagObj{}, false
	}

	return f, true
}

// //

func sortedNodeKeys(node map[string]any) []string {
	keys := make([]string, 0, len(node))
	for k := range node {
		if !strings.HasPrefix(k, "_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
