package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const maxConfigChain = 32

// //

// ParseFile follows config redirects (up to 32) until a terminal file
// (one without a "config" field) is found, then parses that file.
// When a file contains "config", all its other fields are ignored.
func ParseFile(path string, dst Interface) error {
	target, err := resolveChain(path)
	if err != nil {
		return err
	}

	if err := unmarshalFile(target, Obj(dst)); err != nil {
		return err
	}

	Obj(dst).Config = ""
	return nil
}

// //

// resolveChain walks the config→config redirect chain starting from path.
// Returns the terminal file path (one without a "config" field).
// Detects cycles via a visited set; aborts after maxConfigChain hops.
func resolveChain(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}

	visited := map[string]bool{current: true}

	for range maxConfigChain {
		probe := gsettings.NewDefault()
		if err := unmarshalFile(current, probe); err != nil {
			return "", err
		}
		if probe.Config == "" {
			return current, nil
		}

		next := probe.Config
		if !filepath.IsAbs(next) {
			next = filepath.Join(filepath.Dir(current), next)
		}
		next, err = filepath.Abs(next)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", next, err)
		}

		if visited[next] {
			return "", fmt.Errorf("circular config reference: %s", next)
		}

		visited[next] = true
		current = next
	}

	return "", fmt.Errorf("config chain exceeds %d redirects", maxConfigChain)
}

// //

// unmarshalFile reads the file at path and decodes it into dst
// based on the file extension (.json, .yml/.yaml, .hjson/.conf).
func unmarshalFile(path string, dst *gsettings.Obj) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return gsettings.ParseJSON(data, dst)
	case ".yml", ".yaml":
		return gsettings.ParseYAML(data, dst)
	case ".hjson", ".conf":
		return gsettings.ParseHJSON(data, dst)
	default:
		return fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}
}
