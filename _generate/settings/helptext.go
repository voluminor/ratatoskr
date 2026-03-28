package main

import (
	"fmt"
	"sort"
	"strings"

	dep "github.com/voluminor/ratatoskr/_generate"
)

// // // // // // // // // //

type helpGroupObj struct {
	key   string
	title string
	usage string
	flags []FlagObj
}

// //

// buildHelpText produces the formatted help output for all registered flags.
func buildHelpText(flags []FlagObj, tree map[string]*TreeLeafObj) string {
	groups, order := groupFlags(flags, tree)
	maxWidth := maxFlagWidth(flags)

	var b strings.Builder
	b.WriteString("Usage: <program> [flags]\n")

	for gi, gk := range order {
		g := groups[gk]
		if gi > 0 {
			b.WriteString("\n")
		}
		writeGroupHeader(&b, g)
		writeBuiltinFlags(&b, gk, maxWidth)
		writeRegularFlags(&b, g.flags, maxWidth)
		writeTriggerFlags(&b, g.flags, maxWidth)
	}

	return b.String()
}

// //

func groupFlags(flags []FlagObj, tree map[string]*TreeLeafObj) (map[string]*helpGroupObj, []string) {
	groups := make(map[string]*helpGroupObj)
	var order []string

	for _, f := range flags {
		g, ok := groups[f.Group]
		if !ok {
			g = &helpGroupObj{key: f.Group}
			if f.Group == "" {
				g.title = "General"
			} else {
				g.title = dep.GenGoName(f.Group)
				if branch, ok := tree[f.Group]; ok && branch.Usage != "" {
					g.usage = branch.Usage
				}
			}
			groups[f.Group] = g
			order = append(order, f.Group)
		}
		g.flags = append(g.flags, f)
	}

	// "" (General) first, then alphabetical
	sort.SliceStable(order, func(i, j int) bool {
		if order[i] == "" {
			return true
		}
		if order[j] == "" {
			return false
		}
		return order[i] < order[j]
	})

	return groups, order
}

func maxFlagWidth(flags []FlagObj) int {
	w := len("-help")
	for _, f := range flags {
		if fw := len(f.Name) + 1; fw > w {
			w = fw
		}
	}
	return w
}

// //

func writeGroupHeader(b *strings.Builder, g *helpGroupObj) {
	if g.usage != "" {
		fmt.Fprintf(b, "\n%s (%s):\n", g.title, g.usage)
	} else {
		fmt.Fprintf(b, "\n%s:\n", g.title)
	}
}

func writeBuiltinFlags(b *strings.Builder, groupKey string, maxWidth int) {
	if groupKey != "" {
		return
	}
	fmt.Fprintf(b, "  -%-*s  show this help message\n", maxWidth, "h, -help")
	fmt.Fprintf(b, "  -%-*s  show application info\n", maxWidth, "i, -info")
}

func writeRegularFlags(b *strings.Builder, flags []FlagObj, maxWidth int) {
	for _, f := range flags {
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
}

func writeTriggerFlags(b *strings.Builder, flags []FlagObj, maxWidth int) {
	first := true
	for _, f := range flags {
		if !f.IsTrigger {
			continue
		}
		if first {
			b.WriteString("  ---\n")
			first = false
		}
		fmt.Fprintf(b, "  -%-*s  %s [trigger]\n", maxWidth, f.Name, f.Usage)
	}
}
