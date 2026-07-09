package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// // // // // // // // // //

const (
	packageName = "target"
	fileName    = "sigils.go"
	sigilsDir   = "mod/sigils"
	modulePath  = "github.com/voluminor/ratatoskr"
)

//go:embed template.tmpl
var template_text string

//

// SigilObj describes one discovered sigil package.
// Alias is the folder name, which also becomes the import alias and the
// package identifier used to qualify every generated reference. Name is the
// sigName literal from values.go, kept only for duplicate detection.
type SigilObj struct {
	Alias string
	Name  string
}

// ImportObj is a single aliased import line for the generated file.
type ImportObj struct {
	Alias string
	Path  string
}

type TemplateObj struct {
	GenerationTime string
	PackageName    string
	SigilsImport   string
	Imports        []ImportObj
	Sigils         []SigilObj
}

var reSigName = regexp.MustCompile(`(?m)^const sigName\s*=\s*"([^"]+)"`)

// //

func hasRequiredFiles(dir string) bool {
	for _, name := range []string{"func.go", "obj.go", "values.go"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func extractSigName(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	m := reSigName.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("top-level `const sigName = \"...\"` not found in %s", path)
	}

	return string(m[1]), nil
}

// checkDuplicates fails when two folders declare the same sigName, which would
// otherwise produce colliding map keys in the generated file.
func checkDuplicates(list []SigilObj) error {
	seen := make(map[string]string, len(list))
	for _, s := range list {
		if prev, ok := seen[s.Name]; ok {
			return fmt.Errorf("duplicate sigName %q in folders %q and %q", s.Name, prev, s.Alias)
		}
		seen[s.Name] = s.Alias
	}
	return nil
}

// //

func run() error {
	entries, err := os.ReadDir(sigilsDir)
	if err != nil {
		return fmt.Errorf("read sigils directory: %w", err)
	}

	var found []SigilObj
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		dir := filepath.Join(sigilsDir, e.Name())
		if !hasRequiredFiles(dir) {
			continue
		}

		name, err := extractSigName(filepath.Join(dir, "values.go"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %s\n", dir, err)
			continue
		}

		found = append(found, SigilObj{Alias: e.Name(), Name: name})
	}

	if err := checkDuplicates(found); err != nil {
		return err
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].Name < found[j].Name
	})

	//

	imports := make([]ImportObj, 0, len(found))
	for _, s := range found {
		imports = append(imports, ImportObj{
			Alias: s.Alias,
			Path:  modulePath + "/mod/sigils/" + s.Alias,
		})
	}

	data := &TemplateObj{
		GenerationTime: time.Now().Format(time.RFC3339),
		PackageName:    packageName,
		SigilsImport:   modulePath + "/mod/sigils",
		Imports:        imports,
		Sigils:         found,
	}

	if err := writeFileFromTemplate(filepath.Join("target", fileName), template_text, data); err != nil {
		return fmt.Errorf("write generated file: %w", err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
