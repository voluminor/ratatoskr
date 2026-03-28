package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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

//go:embed iface.tmpl
var ifaceTmpl string

//go:embed help.tmpl
var helpTmpl string

//go:embed comments.tmpl
var commentsTmpl string

//go:embed order.tmpl
var orderTmpl string

// // // // // // // // // //

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

	// Single-pass YAML walk: flags + branch usage + gen_interface paths
	walk := WalkYAML(parseTree)

	fieldOrder, err := ExtractFieldOrder(raw)
	if err != nil {
		fmt.Println("Error extracting field order:", err)
		return
	}

	// Resolve enums, Go types, imports, and build struct tree
	resolved := ResolveFlags(walk.Flags, walk.BranchUsage)

	propagateTrigger(resolved.Tree)
	propagateGenInterface(resolved.Tree, walk.GenIfacePaths, "", false)

	data := TemplateObj{
		GenerationTime:  time.Now().Format(time.RFC3339),
		Flags:           resolved.Flags,
		Enums:           resolved.Enums,
		Tree:            resolved.Tree,
		Comments:        buildComments(walk.BranchUsage, resolved.Flags),
		FieldOrder:      fieldOrder,
		TreeChildren:    populateChildren(resolved.Tree, fieldOrder),
		TypesImports:    sortedKeys(resolved.TypesImports),
		FlagsImports:    sortedKeys(resolved.FlagsImports),
		DefaultsImports: sortedKeys(resolved.DefaultsImports),
		HasCustomFlags:  resolved.HasCustomFlags,
		HasEnums:        len(resolved.Enums) > 0,
		HasTriggerFlags: resolved.HasTriggerFlags,
		HelpText:        buildHelpText(resolved.Flags, resolved.Tree),
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
		{"iface.go", ifaceTmpl, false},
		{"enums.go", enumsTmpl, !data.HasEnums},
		{"defaults.go", defaultsTmpl, false},
		{"flags.go", flagsTmpl, false},
		{"parse.go", parseTmpl, false},
		{"save.go", saveTmpl, false},
		{"init.go", initTmpl, false},
		{"help.go", helpTmpl, false},
		{"comments.go", commentsTmpl, false},
		{"order.go", orderTmpl, false},
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
