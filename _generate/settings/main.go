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

//go:embed template.tmpl
var templateText string

// //

type FlagObj struct {
	Name  string
	Type  string
	Value string
	Usage string
}

type TreeLeafObj struct {
	Name   string
	Type   string
	Branch map[string]*TreeLeafObj
}

type TemplateObj struct {
	GenerationTime string
	Path           string

	Flags       []FlagObj
	HasDuration bool
	Tree        map[string]*TreeLeafObj
}

// //

// collectFlags — рекурсивный обход YAML-дерева.
// Узел с ключом "type" считается определением флага,
// иначе — промежуточный уровень иерархии.
func collectFlags(node map[string]any, prefix string) []FlagObj {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []FlagObj

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

		if _, hasType := child["type"]; hasType {
			f := FlagObj{
				Name: fullKey,
				Type: fmt.Sprint(child["type"]),
			}
			if val, ok := child["value"]; ok {
				f.Value = fmt.Sprint(val)
			}
			if usage, ok := child["usage"]; ok {
				f.Usage = fmt.Sprint(usage)
			}
			result = append(result, f)
		} else {
			result = append(result, collectFlags(child, fullKey)...)
		}
	}

	return result
}

// //

func main() {
	flag.Parse()

	// определяем путь к папке и settings.yml
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
	tree := make(map[string]*TreeLeafObj)
	hasDuration := false

	for i, f := range flags {
		switch f.Type {
		case "duration":
			hasDuration = true

		case "bool":
			v := strings.ToLower(f.Value)
			if v == "true" || v == "1" || v == "yes" {
				flags[i].Value = "true"
			} else if v == "false" || v == "0" || v == "no" || v == "" {
				flags[i].Value = "false"
			}

		case "float":
			if f.Value == "" {
				flags[i].Value = "0"
			}
		}

		points := strings.Split(f.Name, ".")
		if f.Type == "" {
			fmt.Println("Missing type:", f.Name)
			return
		}
		switch len(points) {
		case 1:
			tree[points[0]] = &TreeLeafObj{
				Name: dep.GenGoName(points[0]),
				Type: f.Type,
			}
		case 2:
			branch, ok := tree[points[0]]
			if !ok {
				branch = &TreeLeafObj{
					Branch: make(map[string]*TreeLeafObj),
					Name:   dep.GenGoName(points[0]),
					Type:   dep.GenGoName(points[0]) + "Obj",
				}
				tree[points[0]] = branch
			}
			branch.Branch[points[1]] = &TreeLeafObj{
				Name: dep.GenGoName(points[1]),
				Type: f.Type,
			}
		case 3:
			branch, ok := tree[points[0]]
			if !ok {
				branch = &TreeLeafObj{
					Branch: make(map[string]*TreeLeafObj),
					Name:   dep.GenGoName(points[0]),
					Type:   dep.GenGoName(points[0]) + "Obj",
				}
				tree[points[0]] = branch
			}
			leaf, ok := branch.Branch[points[1]]
			if !ok {
				leaf = &TreeLeafObj{
					Branch: make(map[string]*TreeLeafObj),
					Name:   dep.GenGoName(points[1]),
					Type:   dep.GenGoName(points[1]) + "Obj",
				}
				branch.Branch[points[1]] = leaf
			}
			leaf.Branch[points[2]] = &TreeLeafObj{
				Name: dep.GenGoName(points[2]),
				Type: f.Type,
			}

		default:
			fmt.Println("The settings tree is too deep! Can't go deeper than 3\t", f.Name)
			return
		}
	}

	data := TemplateObj{
		GenerationTime: time.Now().Format(time.RFC3339),
		Flags:          flags,
		HasDuration:    hasDuration,
		Tree:           tree,
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

	err = dep.WriteFileFromTemplate(
		filepath.Join(outDir, "defaults.go"),
		templateText,
		data,
	)
	if err != nil {
		fmt.Println("Error generating settings:", err)
	}
}
