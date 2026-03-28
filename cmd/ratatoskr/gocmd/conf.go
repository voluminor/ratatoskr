package gocmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hjson/hjson-go/v4"
	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"
	"gopkg.in/yaml.v3"

	msettings "github.com/voluminor/ratatoskr/mod/settings"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const configBaseName = "ratatoskr-config"

// //

func confCmd(cfg *gsettings.GoConfObj) (bool, error) {
	if cfg.Generate.Path != "" {
		return true, confGenerate(&cfg.Generate)
	}
	if cfg.Import.From != "" {
		return true, confImport(&cfg.Import)
	}
	if cfg.Export.From != "" {
		return true, confExport(&cfg.Export)
	}
	return false, nil
}

// //

func confGenerate(cfg *gsettings.GoConfGenerateObj) error {
	outDir, err := validateDir(cfg.Path)
	if err != nil {
		return err
	}

	ext := confFormatExt(cfg.Format)
	outPath := filepath.Join(outDir, configBaseName+ext)

	nodeCfg := yggconfig.GenerateConfig()
	obj := msettings.FromNodeConfig(nodeCfg, gsettings.NewDefault())
	base := msettings.Obj(obj)

	var data any
	switch cfg.Preset {
	case gsettings.GoConfGeneratePresetBasic:
		data = presetBasic(base)
	case gsettings.GoConfGeneratePresetMedium:
		data = presetMedium(base)
	default:
		data = base
	}

	if err := saveWithComments(data, outPath); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

// //

func confImport(cfg *gsettings.GoConfImportObj) error {
	outDir, err := validateDir(cfg.To)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}

	f, err := os.Open(cfg.From)
	if err != nil {
		return fmt.Errorf("open yggdrasil config: %w", err)
	}
	defer f.Close()

	nodeCfg := &yggconfig.NodeConfig{}
	if _, err := nodeCfg.ReadFrom(f); err != nil {
		return fmt.Errorf("parse yggdrasil config: %w", err)
	}

	obj := msettings.FromNodeConfig(nodeCfg, gsettings.NewDefault())
	base := msettings.Obj(obj)

	ext := confFormatExt(cfg.Format)
	outPath := filepath.Join(outDir, configBaseName+ext)

	if err := saveWithComments(base, outPath); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

// //

func confExport(cfg *gsettings.GoConfExportObj) error {
	outDir, err := validateDir(cfg.To)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}

	obj := gsettings.NewDefault()
	if err := msettings.ParseFile(cfg.From, obj); err != nil {
		return fmt.Errorf("read ratatoskr config: %w", err)
	}

	nodeCfg, err := msettings.NodeConfig(obj.GetYggdrasil())
	if err != nil {
		return fmt.Errorf("convert to yggdrasil config: %w", err)
	}

	ext := confFormatExt(cfg.Format)
	outPath := filepath.Join(outDir, "yggdrasil"+ext)

	data, err := marshalNodeConfig(nodeCfg, ext)
	if err != nil {
		return err
	}

	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

// //

func presetBasic(obj *gsettings.Obj) map[string]any {
	return map[string]any{
		"yggdrasil": map[string]any{
			"key": map[string]any{
				"text": obj.Yggdrasil.Key.Text,
			},
			"peers": map[string]any{
				"url": obj.Yggdrasil.Peers.Url,
			},
		},
	}
}

// //

func presetMedium(obj *gsettings.Obj) map[string]any {
	return map[string]any{
		"yggdrasil": map[string]any{
			"key": map[string]any{
				"text": obj.Yggdrasil.Key.Text,
			},
			"listen": obj.Yggdrasil.Listen,
			"inputs": obj.Yggdrasil.Inputs,
			"peers": map[string]any{
				"url": obj.Yggdrasil.Peers.Url,
			},
			"if": map[string]any{
				"mtu": obj.Yggdrasil.If.Mtu,
			},
			"core_stop_timeout": obj.Yggdrasil.CoreStopTimeout.String(),
			"rst_queue_size":    obj.Yggdrasil.RstQueueSize,
		},
		"log": map[string]any{
			"level": map[string]any{
				"console": obj.Log.Level.Console.String(),
				"file":    obj.Log.Level.File.String(),
			},
			"output": obj.Log.Output.String(),
		},
	}
}

// //

func saveWithComments(data any, path string) error {
	var raw []byte
	var err error

	m, isMap := data.(map[string]any)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if isMap {
			raw, err = marshalOrderedJSON(m, "")
		} else {
			raw, err = json.MarshalIndent(data, "", "  ")
		}
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		raw = append(raw, '\n')
	case ".yml", ".yaml":
		raw, err = marshalYAMLWithComments(data)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
	case ".hjson", ".conf":
		if isMap {
			raw, err = marshalOrderedHJSON(m, "")
		} else {
			raw, err = hjson.MarshalWithOptions(data, hjson.DefaultOptions())
		}
		if err != nil {
			return fmt.Errorf("marshal hjson: %w", err)
		}
		raw = injectHJSONComments(raw)
	default:
		return fmt.Errorf("unsupported format: %s", filepath.Ext(path))
	}

	raw = msettings.StripRootKey(raw, "config")
	return os.WriteFile(path, raw, 0644)
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

func marshalHJSONWithComments(data any) ([]byte, error) {
	raw, err := hjson.MarshalWithOptions(data, hjson.DefaultOptions())
	if err != nil {
		return nil, err
	}
	return injectHJSONComments(raw), nil
}

// //

func injectHJSONComments(raw []byte) []byte {
	lines := strings.Split(string(raw), "\n")
	var stack []string
	result := make([]string, 0, len(lines)+len(gsettings.Comments))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Closing brace — pop stack
		if strings.HasPrefix(trimmed, "}") {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			result = append(result, line)
			continue
		}

		// Skip empty lines, comments, opening brace
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

			// Blank line before groups unless first in block
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

// //

func confFormatExt(format gsettings.GoConfExportFormatEnum) string {
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

// //

func marshalNodeConfig(cfg *yggconfig.NodeConfig, ext string) ([]byte, error) {
	switch ext {
	case ".json":
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		return append(data, '\n'), nil
	case ".conf":
		data, err := hjson.MarshalWithOptions(cfg, hjson.DefaultOptions())
		if err != nil {
			return nil, fmt.Errorf("marshal hjson: %w", err)
		}
		return append(data, '\n'), nil
	default:
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		return append(data, '\n'), nil
	}
}

// //

func validateDir(path string) (string, error) {
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
