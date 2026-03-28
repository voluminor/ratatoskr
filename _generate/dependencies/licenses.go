package main

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// // // // // // // // // //

func CompressString(s string) ([]byte, error) {
	var buf bytes.Buffer

	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return nil, err
	}

	_, err = w.Write([]byte(s))
	if err != nil {
		_ = w.Close()
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type modJSON struct {
	Path    string
	Version string
	Dir     string
	Replace *struct {
		Path    string
		Version string
		Dir     string
	}
}

func listModulesAll(dir string) ([]modJSON, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	if dir != "" && dir != "." {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))

	var mods []modJSON
	for {
		var m modJSON
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		if m.Replace != nil && m.Replace.Dir != "" {
			m.Dir = m.Replace.Dir
			if m.Replace.Path != "" {
				m.Path = m.Replace.Path
			}
			if m.Replace.Version != "" {
				m.Version = m.Replace.Version
			}
		}
		mods = append(mods, m)
	}
	return mods, nil
}

var licenseCandidates = []string{
	"LICENSE", "LICENSE.txt", "LICENSE.md",
	"COPYING", "COPYING.txt", "COPYING.md",
	"NOTICE", "NOTICE.txt",
}

func findLicenseAtDir(dir string) (filename string, data []byte, ok bool) {
	if dir == "" {
		return "", nil, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		for _, cand := range licenseCandidates {
			if strings.EqualFold(name, cand) {
				b, err := os.ReadFile(filepath.Join(dir, name))
				if err == nil && len(b) > 0 {
					return name, b, true
				}
			}
		}
	}
	return "", nil, false
}

// //

func buildLicEntries(dependencies map[string]string, modDir string) (map[string][]byte, error) {
	mods, err := listModulesAll(modDir)
	if err != nil {
		return nil, err
	}
	dirByMod := make(map[string]string, len(mods))
	for _, m := range mods {
		if m.Path == "" || m.Version == "" || m.Dir == "" {
			continue
		}
		dirByMod[m.Path+"@"+m.Version] = m.Dir
	}

	outMap := map[string][]byte{}

	for path, ver := range dependencies {
		key := path + "@" + ver
		dir := dirByMod[key]
		if dir == "" {
			continue
		}
		if _, data, ok := findLicenseAtDir(dir); ok {
			outMap[path] = data
		}
	}
	return outMap, nil
}
