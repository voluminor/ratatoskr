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
	"github.com/voluminor/ratatoskr/target"
	"gopkg.in/yaml.v3"

	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

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

// ConfigPath builds the full output path: dir/GlobalName+ext.
func ConfigPath(dir string, format gsettings.GoConfExportFormatEnum) string {
	return filepath.Join(dir, target.GlobalName+FormatExt(format))
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

// SaveFile serializes src into the requested format and writes it to
// dir/GlobalName+ext. The "config" key is stripped from the output
// to prevent redirect loops when the file is later loaded.
// Creates dir if it does not exist. Returns the full output path.
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

// SaveFilePretty saves settings with human-friendly formatting:
// preserved field order and inline comments from gsettings.Comments.
// Writes to dir/GlobalName+ext. Returns the full output path.
func SaveFilePretty(src Interface, dir string, format gsettings.GoConfExportFormatEnum) (string, error) {
	return SaveUnsafePretty(Obj(src), dir, format)
}

// //

// SaveUnsafePretty writes arbitrary data (not necessarily *gsettings.Obj) with
// preserved field order and comments.
//
// "Unsafe" because:
//   - data is typed as any — no compile-time guarantee that the structure
//     matches the expected config schema;
//   - if data is map[string]any, key ordering relies on gsettings.FieldOrder;
//     missing or extra keys silently pass through without validation;
//   - the "config" key is stripped from output by text search, which may
//     remove unrelated keys named "config" in nested structures.
//
// Use SaveFilePretty when the source is a typed Interface.
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

// marshalPretty serializes data with human-friendly formatting.
// When data is map[string]any, field order follows gsettings.FieldOrder;
// otherwise falls back to the default encoder behavior.
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
// Works by line-scanning for patterns like `key:`, `"key":` — not by parsing
// the document tree. Also cleans up trailing commas left by JSON key removal.
//
// Limitation: only removes root-level occurrences. Nested keys with the same
// name are preserved. False positives are possible if a value line starts
// with the exact key pattern (unlikely for well-formed config).
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

// marshalByFormat serializes a typed settings object using standard encoders
// (no custom field ordering or comment injection).
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

// marshalYAMLWithComments serializes data to YAML, then re-parses the output
// as a yaml.Node tree to inject comments from gsettings.Comments and reorder
// fields according to gsettings.FieldOrder before final serialization.
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

// annotateYAMLNode recursively walks a YAML mapping node and sets
// HeadComment on keys whose dot-path appears in gsettings.Comments.
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

// reorderYAMLNode recursively reorders mapping node children
// to match the sequence defined in gsettings.FieldOrder[prefix].
// Keys absent from the order list are appended at the end.
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

// orderedKeys returns map keys sorted by gsettings.FieldOrder[prefix].
// Falls back to alphabetical sort if no order is defined.
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

// marshalOrderedJSON serializes a map to indented JSON with key order
// controlled by gsettings.FieldOrder.
func marshalOrderedJSON(m map[string]any, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	writeOrderedMap(&buf, m, prefix, true, 0)
	return buf.Bytes(), nil
}

// //

// marshalOrderedHJSON serializes a map to HJSON with key order
// controlled by gsettings.FieldOrder.
func marshalOrderedHJSON(m map[string]any, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	writeOrderedMap(&buf, m, prefix, false, 0)
	return buf.Bytes(), nil
}

// //

// writeOrderedMap recursively writes a map as an indented JSON or HJSON
// object. In jsonMode keys are quoted and entries are comma-separated.
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

// writeOrderedValue writes a single value to buf. In jsonMode delegates
// to json.Marshal; in HJSON mode handles common types explicitly.
// Marshal errors are silently ignored (output will contain "null").
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

// injectHJSONComments inserts "# comment" lines above HJSON keys
// whose dot-path appears in gsettings.Comments. Tracks nesting depth
// via a stack of key names to reconstruct dot-paths.
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
