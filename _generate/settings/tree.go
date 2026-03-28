package main

import (
	"strings"

	dep "github.com/voluminor/ratatoskr/_generate"
)

// // // // // // // // // //

// insertLeaf navigates/creates branch nodes along the dot-separated path
// and places the leaf at the terminal position.
func insertLeaf(tree map[string]*TreeLeafObj, points []string, leaf *TreeLeafObj, branchUsage map[string]string) {
	current := tree
	for i := 0; i < len(points)-1; i++ {
		key := points[i]
		branch, ok := current[key]
		if !ok {
			usageKey := strings.Join(points[:i+1], ".")
			typeName := branchTypeName(points[:i+1])
			branch = &TreeLeafObj{
				Branch: make(map[string]*TreeLeafObj),
				Name:   dep.GenGoName(key),
				Type:   typeName,
				Key:    key,
				Usage:  branchUsage[usageKey],
			}
			current[key] = branch
		}
		current = branch.Branch
	}
	current[points[len(points)-1]] = leaf
}

// branchTypeName builds the struct type name for a branch node.
// Root-level branches (depth 1) use just the key: "GoObj".
// Deeper branches include the full path: "GoKeyObj", "YggdrasilPeersManagerObj".
func branchTypeName(path []string) string {
	if len(path) <= 1 {
		return dep.GenGoName(path[len(path)-1]) + "Obj"
	}
	var b strings.Builder
	for _, p := range path {
		b.WriteString(dep.GenGoName(p))
	}
	b.WriteString("Obj")
	return b.String()
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

// propagateGenInterface sets GenInterface on matching tree nodes
// and inherits the flag to all descendant branches.
func propagateGenInterface(tree map[string]*TreeLeafObj, paths map[string]bool, prefix string, inherited bool) {
	for key, node := range tree {
		if node.Branch == nil {
			continue
		}
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		active := inherited || paths[fullKey]
		if active {
			node.GenInterface = true
			node.InterfaceType = strings.TrimSuffix(node.Type, "Obj") + "Interface"
		}
		propagateGenInterface(node.Branch, paths, fullKey, active)
	}
}
