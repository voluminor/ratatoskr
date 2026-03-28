package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hjson/hjson-go/v4"
	"gopkg.in/yaml.v3"
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

	if err := unmarshalFile(target, dst); err != nil {
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
		next, err := probeConfig(current)
		if err != nil {
			return "", err
		}
		if next == "" {
			return current, nil
		}

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

func probeConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config %s: %w", path, err)
	}

	var probe struct {
		Config string `json:"config" yaml:"config"`
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		err = json.Unmarshal(data, &probe)
	case ".yml", ".yaml":
		err = yaml.Unmarshal(data, &probe)
	case ".hjson", ".conf":
		err = hjson.Unmarshal(data, &probe)
	default:
		return "", fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}

	if err != nil {
		return "", fmt.Errorf("parse config %s: %w", path, err)
	}
	return probe.Config, nil
}

// //

func unmarshalFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return json.Unmarshal(data, dst)
	case ".yml", ".yaml":
		return yaml.Unmarshal(data, dst)
	case ".hjson", ".conf":
		return hjson.Unmarshal(data, dst)
	default:
		return fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}
}

// //

// SaveFile marshals the settings object to a file.
// The "config" field is excluded from output.
func SaveFile(src Interface, path string) error {
	var data []byte
	var err error

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		data, err = json.MarshalIndent(src, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		data = append(data, '\n')
	case ".yml", ".yaml":
		data, err = yaml.Marshal(src)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
	case ".hjson", ".conf":
		data, err = hjson.MarshalWithOptions(src, hjson.DefaultOptions())
		if err != nil {
			return fmt.Errorf("marshal hjson: %w", err)
		}
		data = append(data, '\n')
	default:
		return fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}

	data = StripRootKey(data, "config")
	return os.WriteFile(path, data, 0644)
}

// //

// StripRootKey removes a root-level key line from serialized config output.
func StripRootKey(data []byte, key string) []byte {
	lines := strings.Split(string(data), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == key+":" ||
			strings.HasPrefix(trimmed, key+": ") ||
			strings.HasPrefix(trimmed, "\""+key+"\":") {
			continue
		}
		result = append(result, line)
	}
	return []byte(strings.Join(result, "\n"))
}
