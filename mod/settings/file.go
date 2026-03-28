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

// //

// SaveFile marshals the settings object to a file.
// The "config" field is excluded from output.
func SaveFile(src Interface, path string) error {
	obj := Obj(src)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := gsettings.SaveJSON(obj, path); err != nil {
			return err
		}
	case ".yml", ".yaml":
		if err := gsettings.SaveYAML(obj, path); err != nil {
			return err
		}
	case ".hjson", ".conf":
		if err := gsettings.SaveHJSON(obj, path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, StripRootKey(data, "config"), 0644)
}

// //

// StripRootKey removes a root-level key line from serialized config output.
// Also removes trailing commas left by JSON removal.
func StripRootKey(data []byte, key string) []byte {
	lines := strings.Split(string(data), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == key+":" ||
			strings.HasPrefix(trimmed, key+": ") ||
			strings.HasPrefix(trimmed, "\""+key+"\":") {
			if n := len(result); n > 0 {
				prev := strings.TrimRight(result[n-1], " \t")
				if strings.HasSuffix(prev, ",") {
					result[n-1] = prev[:len(prev)-1]
				}
			}
			continue
		}
		result = append(result, line)
	}
	return []byte(strings.Join(result, "\n"))
}
