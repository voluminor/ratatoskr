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

// ParseFile reads a config file and unmarshals it into dst.
// Format is detected by file extension: .json, .yml/.yaml, .hjson/.conf
func ParseFile(path string, dst Interface) error {
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

// SaveFile marshals src and writes it to the given path.
// Format is detected by file extension: .json, .yml/.yaml, .hjson/.conf
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

	return os.WriteFile(path, data, 0644)
}
