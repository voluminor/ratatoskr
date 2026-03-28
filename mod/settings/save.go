package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hjson/hjson-go/v4"
	"gopkg.in/yaml.v3"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const ConfigBaseName = "ratatoskr-config"

// //

// FormatExt returns the file extension for a given format enum.
func FormatExt(format gsettings.GoConfExportFormatEnum) string {
	switch format {
	case gsettings.GoConfExportFormatJson:
		return ".json"
	case gsettings.GoConfExportFormatYml:
		return ".yml"
	case gsettings.GoConfExportFormatConf:
		return ".conf"
	default:
		return ".yml"
	}
}

// ConfigPath builds the full output path: dir/ConfigBaseName+ext.
func ConfigPath(dir string, format gsettings.GoConfExportFormatEnum) string {
	return filepath.Join(dir, ConfigBaseName+FormatExt(format))
}

// //

// ValidateDir checks that path is a directory (creates it if missing).
func ValidateDir(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("output directory path is required")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(abs, 0755); err != nil {
				return "", fmt.Errorf("create directory %q: %w", abs, err)
			}
			return abs, nil
		}
		return "", fmt.Errorf("stat %q: %w", abs, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", abs)
	}

	return abs, nil
}

// //

// SaveFile marshals the settings object and writes it to dir/ConfigBaseName+ext.
// The "config" field is excluded from output.
// Returns the full output path.
func SaveFile(src Interface, dir string, format gsettings.GoConfExportFormatEnum) (string, error) {
	absDir, err := ValidateDir(dir)
	if err != nil {
		return "", err
	}

	path := ConfigPath(absDir, format)

	raw, err := marshalByFormat(Obj(src), format)
	if err != nil {
		return "", err
	}

	raw = StripRootKey(raw, "config")
	return path, os.WriteFile(path, raw, 0644)
}

// //

// SaveFilePretty saves settings with preserved field order and comments
// to dir/ConfigBaseName+ext. Returns the full output path.
func SaveFilePretty(src Interface, dir string, format gsettings.GoConfExportFormatEnum) (string, error) {
	return SaveUnsafePretty(Obj(src), dir, format)
}

// //

// SaveUnsafePretty saves arbitrary data with preserved field order and comments
// to dir/ConfigBaseName+ext. Returns the full output path.
func SaveUnsafePretty(data any, dir string, format gsettings.GoConfExportFormatEnum) (string, error) {
	absDir, err := ValidateDir(dir)
	if err != nil {
		return "", err
	}

	path := ConfigPath(absDir, format)

	raw, err := marshalPretty(data, format)
	if err != nil {
		return "", err
	}

	raw = StripRootKey(raw, "config")
	return path, os.WriteFile(path, raw, 0644)
}

// //

func marshalPretty(data any, format gsettings.GoConfExportFormatEnum) ([]byte, error) {
	m, isMap := data.(map[string]any)

	switch format {
	case gsettings.GoConfExportFormatJson:
		var raw []byte
		var err error
		if isMap {
			raw, err = marshalOrderedJSON(m, "")
		} else {
			raw, err = json.MarshalIndent(data, "", "  ")
		}
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		return append(raw, '\n'), nil

	case gsettings.GoConfExportFormatConf:
		var raw []byte
		var err error
		if isMap {
			raw, err = marshalOrderedHJSON(m, "")
		} else {
			raw, err = hjson.MarshalWithOptions(data, hjson.DefaultOptions())
		}
		if err != nil {
			return nil, fmt.Errorf("marshal hjson: %w", err)
		}
		return injectHJSONComments(raw), nil

	case gsettings.GoConfExportFormatYml:
		raw, err := marshalYAMLWithComments(data)
		if err != nil {
			return nil, fmt.Errorf("marshal yaml: %w", err)
		}
		return raw, nil

	default:
		return nil, fmt.Errorf("unsupported format: %d", format)
	}
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

// //

func marshalByFormat(obj *gsettings.Obj, format gsettings.GoConfExportFormatEnum) ([]byte, error) {
	switch format {
	case gsettings.GoConfExportFormatJson:
		data, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		return append(data, '\n'), nil
	case gsettings.GoConfExportFormatConf:
		data, err := hjson.MarshalWithOptions(obj, hjson.DefaultOptions())
		if err != nil {
			return nil, fmt.Errorf("marshal hjson: %w", err)
		}
		return append(data, '\n'), nil
	case gsettings.GoConfExportFormatYml:
		data, err := yaml.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("marshal yaml: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported format: %d", format)
	}
}

// //

func marshalYAMLWithComments(data any) ([]byte, error) {
	raw, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		reorderYAMLNode(doc.Content[0], "")
		annotateYAMLNode(doc.Content[0], "")
	}

	return yaml.Marshal(&doc)
}

// //

func annotateYAMLNode(node *yaml.Node, prefix string) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]

		path := keyNode.Value
		if prefix != "" {
			path = prefix + "." + keyNode.Value
		}

		if comment, ok := gsettings.Comments[path]; ok {
			isGroup := valNode.Kind == yaml.MappingNode
			if isGroup && i > 0 {
				keyNode.HeadComment = "\n" + comment
			} else {
				keyNode.HeadComment = comment
			}
		}

		if valNode.Kind == yaml.MappingNode {
			annotateYAMLNode(valNode, path)
		}
	}
}

// //

func reorderYAMLNode(node *yaml.Node, prefix string) {
	if node.Kind != yaml.MappingNode {
		return
	}

	if order, ok := gsettings.FieldOrder[prefix]; ok && len(node.Content) > 2 {
		idx := make(map[string]int, len(node.Content)/2)
		for i := 0; i < len(node.Content)-1; i += 2 {
			idx[node.Content[i].Value] = i
		}

		out := make([]*yaml.Node, 0, len(node.Content))
		seen := make(map[string]bool)
		for _, k := range order {
			if i, exists := idx[k]; exists {
				out = append(out, node.Content[i], node.Content[i+1])
				seen[k] = true
			}
		}
		for i := 0; i < len(node.Content)-1; i += 2 {
			if !seen[node.Content[i].Value] {
				out = append(out, node.Content[i], node.Content[i+1])
			}
		}
		node.Content = out
	}

	for i := 0; i < len(node.Content)-1; i += 2 {
		val := node.Content[i+1]
		if val.Kind == yaml.MappingNode {
			p := node.Content[i].Value
			if prefix != "" {
				p = prefix + "." + p
			}
			reorderYAMLNode(val, p)
		}
	}
}

// //

func orderedKeys(m map[string]any, prefix string) []string {
	order, ok := gsettings.FieldOrder[prefix]
	if !ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}

	result := make([]string, 0, len(m))
	seen := make(map[string]bool, len(m))
	for _, k := range order {
		if _, exists := m[k]; exists {
			result = append(result, k)
			seen[k] = true
		}
	}
	for k := range m {
		if !seen[k] {
			result = append(result, k)
		}
	}
	return result
}

// //

func marshalOrderedJSON(m map[string]any, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	writeOrderedMap(&buf, m, prefix, true, 0)
	return buf.Bytes(), nil
}

// //

func marshalOrderedHJSON(m map[string]any, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	writeOrderedMap(&buf, m, prefix, false, 0)
	return buf.Bytes(), nil
}

// //

func writeOrderedMap(buf *bytes.Buffer, m map[string]any, prefix string, jsonMode bool, level int) {
	indent := strings.Repeat("  ", level)
	childIndent := strings.Repeat("  ", level+1)
	keys := orderedKeys(m, prefix)

	buf.WriteString("{\n")
	for i, k := range keys {
		buf.WriteString(childIndent)

		childPrefix := k
		if prefix != "" {
			childPrefix = prefix + "." + k
		}

		if jsonMode {
			fmt.Fprintf(buf, "%q: ", k)
		} else {
			buf.WriteString(k)
			buf.WriteString(": ")
		}

		if nested, ok := m[k].(map[string]any); ok {
			writeOrderedMap(buf, nested, childPrefix, jsonMode, level+1)
		} else {
			writeOrderedValue(buf, m[k], jsonMode, level+1)
		}

		if jsonMode && i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(indent)
	buf.WriteByte('}')
}

// //

func writeOrderedValue(buf *bytes.Buffer, v any, jsonMode bool, level int) {
	if jsonMode {
		raw, _ := json.Marshal(v)
		buf.Write(raw)
		return
	}

	switch val := v.(type) {
	case string:
		fmt.Fprintf(buf, "%q", val)
	case bool:
		fmt.Fprintf(buf, "%t", val)
	case int:
		fmt.Fprintf(buf, "%d", val)
	case int64:
		fmt.Fprintf(buf, "%d", val)
	case uint64:
		fmt.Fprintf(buf, "%d", val)
	case float64:
		fmt.Fprintf(buf, "%g", val)
	case []string:
		if len(val) == 0 {
			buf.WriteString("[]")
		} else {
			buf.WriteString("[\n")
			for _, s := range val {
				buf.WriteString(strings.Repeat("  ", level+1))
				fmt.Fprintf(buf, "%q", s)
				buf.WriteByte('\n')
			}
			buf.WriteString(strings.Repeat("  ", level))
			buf.WriteByte(']')
		}
	case nil:
		buf.WriteString("null")
	default:
		raw, _ := json.Marshal(val)
		buf.Write(raw)
	}
}

// //

func injectHJSONComments(raw []byte) []byte {
	lines := strings.Split(string(raw), "\n")
	var stack []string
	result := make([]string, 0, len(lines)+len(gsettings.Comments))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "}") {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			result = append(result, line)
			continue
		}

		colonIdx := strings.Index(trimmed, ":")
		if colonIdx <= 0 || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			result = append(result, line)
			continue
		}

		key := trimmed[:colonIdx]
		path := key
		if len(stack) > 0 {
			path = strings.Join(stack, ".") + "." + key
		}

		rest := strings.TrimSpace(trimmed[colonIdx+1:])
		isGroup := strings.HasSuffix(rest, "{") || rest == "{"

		if comment, ok := gsettings.Comments[path]; ok {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

			if isGroup && len(result) > 0 {
				prev := strings.TrimSpace(result[len(result)-1])
				if prev != "{" && prev != "" {
					result = append(result, "")
				}
			}

			result = append(result, indent+"# "+comment)
		}

		if isGroup {
			stack = append(stack, key)
		}

		result = append(result, line)
	}

	return append([]byte(strings.Join(result, "\n")), '\n')
}
