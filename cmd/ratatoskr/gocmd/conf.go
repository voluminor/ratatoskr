package gocmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	switch cfg.Preset {
	case gsettings.GoConfGeneratePresetBasic:
		err = saveMap(presetBasic(base), outPath)
	case gsettings.GoConfGeneratePresetMedium:
		err = saveMap(presetMedium(base), outPath)
	default:
		err = msettings.SaveFile(base, outPath)
	}
	if err != nil {
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

	if err := msettings.SaveFile(base, outPath); err != nil {
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

func saveMap(data map[string]any, path string) error {
	var raw []byte
	var err error

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		raw, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		raw = append(raw, '\n')
	case ".yml", ".yaml":
		raw, err = yaml.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
	case ".hjson", ".conf":
		raw, err = hjson.MarshalWithOptions(data, hjson.DefaultOptions())
		if err != nil {
			return fmt.Errorf("marshal hjson: %w", err)
		}
		raw = append(raw, '\n')
	default:
		return fmt.Errorf("unsupported format: %s", filepath.Ext(path))
	}

	return os.WriteFile(path, raw, 0644)
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
