package main

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dep "github.com/voluminor/ratatoskr/_generate"
)

// // // // // // // // // //

const (
	packageName = "target"
	fileName    = "dependencies.go"
)

//go:embed template.tmpl
var template_text string

//

type Dep struct {
	Key   string
	Value string

	License string
}

type License struct {
	Data   []byte
	IsNull bool
}
type TemplateObj struct {
	GenerationTime string
	PackageName    string
	Path           string
	ImportsArr     []string

	Deps     []Dep
	Licenses map[string]License
}

// //

func main() {
	modPath := flag.String("mod", "go.mod", "path to go.mod")
	flag.Parse()

	file, err := os.Open(*modPath)
	if err != nil {
		fmt.Println("Error opening go.mod:", err)
		return
	}
	defer file.Close()

	dependencies := make(map[string]string)
	pRequire := 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if len(line) > 7 && line[0:7] == "require" {
			pRequire++
			if pRequire > 1 {
				break
			}
		}
		if pRequire == 0 {
			continue
		}

		if strings.HasPrefix(line, "\t") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				dependencies[fields[0]] = fields[1]
			}
		}
	}

	modDir := filepath.Dir(*modPath)
	lics, err := buildLicEntries(dependencies, modDir)
	if err != nil {
		fmt.Println("Error building licenses:", err)
		return
	}

	//

	keys := make([]string, 0, len(dependencies))
	for k := range dependencies {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	mapLicense := map[string]License{}
	for k := range dependencies {
		h := sha256.New()
		h.Write(lics[k])

		hash := hex.EncodeToString(h.Sum(nil))
		hash = hash[:16]

		_, ok := mapLicense[hash]
		if !ok {
			text := string(lics[k])
			text = strings.TrimSpace(text)

			if len(text) == 0 {
				mapLicense[hash] = License{IsNull: true}
				continue
			}

			compressed, err := CompressString(text)
			if err != nil {
				fmt.Printf("Error compressing license for %s: %s\n", k, err)
				continue
			}
			mapLicense[hash] = License{
				Data:   compressed,
				IsNull: false,
			}
		}
	}

	depList := make([]Dep, 0, len(keys))
	for _, k := range keys {
		h := sha256.New()
		h.Write(lics[k])

		depList = append(depList, Dep{
			Key:     k,
			Value:   dependencies[k],
			License: hex.EncodeToString(h.Sum(nil))[:16],
		})
	}

	//

	data := new(TemplateObj)
	data.GenerationTime = time.Now().Format(time.RFC3339)
	data.PackageName = packageName
	data.Deps = depList
	data.Licenses = mapLicense

	data.ImportsArr = make([]string, 0)
	data.ImportsArr = append(data.ImportsArr, "io")
	data.ImportsArr = append(data.ImportsArr, "bytes")
	data.ImportsArr = append(data.ImportsArr, "compress/flate")

	outDir := "target"
	if *modPath != "go.mod" {
		data.Path = filepath.Dir(*modPath)
		outDir = filepath.Join("target", data.Path)
	}

	err = dep.WriteFileFromTemplate(filepath.Join(outDir, fileName), template_text, data)
	if err != nil {
		fmt.Println("Error saving generated file:", err)
	}
}
