package gocmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hjson/hjson-go/v4"
	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	msettings "github.com/voluminor/ratatoskr/mod/settings"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

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

	outPath, err := msettings.SaveUnsafePretty(data, cfg.Path, cfg.Format)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

// //

func confImport(cfg *gsettings.GoConfImportObj) error {
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

	outPath, err := msettings.SaveFilePretty(obj, cfg.To, cfg.Format)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

// //

func confExport(cfg *gsettings.GoConfExportObj) error {
	outDir, err := msettings.ValidateDir(cfg.To)
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

	ext := msettings.FormatExt(cfg.Format)
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

func marshalNodeConfig(cfg *yggconfig.NodeConfig, ext string) ([]byte, error) {
	var data []byte
	var err error

	if ext == ".conf" {
		data, err = hjson.MarshalWithOptions(cfg, hjson.DefaultOptions())
	} else {
		data, err = json.MarshalIndent(cfg, "", "  ")
	}
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	return append(data, '\n'), nil
}
