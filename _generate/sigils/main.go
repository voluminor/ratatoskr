package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	dep "github.com/voluminor/ratatoskr/_generate"
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

type SigilObj struct {
	Name  string
	Dir   string
	Alias string
}

type TemplateObj struct {
	GenerationTime string
	PackageName    string
	ImportsArr     []string
	Sigils         []SigilObj
}

var reSigName = regexp.MustCompile(`(?m)^const sigName\s*=\s*"([^"]+)"`)

// //

func main() {
	entries, err := os.ReadDir(sigilsDir)
	if err != nil {
		fmt.Println("Error reading sigils directory:", err)
		return
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
			fmt.Printf("Error extracting sigName from %s: %s\n", dir, err)
			continue
		}

		found = append(found, SigilObj{
			Name:  name,
			Dir:   e.Name(),
			Alias: e.Name(),
		})
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].Name < found[j].Name
	})

	//

	imports := []string{
		modulePath + "/mod/sigils",
	}
	for _, s := range found {
		imports = append(imports, modulePath+"/mod/sigils/"+s.Dir)
	}

	data := &TemplateObj{
		GenerationTime: time.Now().Format(time.RFC3339),
		PackageName:    packageName,
		ImportsArr:     imports,
		Sigils:         found,
	}

	err = dep.WriteFileFromTemplate(filepath.Join("target", fileName), template_text, data)
	if err != nil {
		fmt.Println("Error saving generated file:", err)
	}
}

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
		return "", fmt.Errorf("sigName not found in %s", path)
	}

	return string(m[1]), nil
}
