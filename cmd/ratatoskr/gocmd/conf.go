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

	ext := confFormatExt(cfg.Format, ".yml")
	outPath := filepath.Join(outDir, configBaseName+ext)

	nodeCfg := yggconfig.GenerateConfig()
	obj := msettings.FromNodeConfig(nodeCfg, gsettings.NewDefault())
	base := msettings.Obj(obj)

	applyPreset(base, cfg.Preset)

	if err := msettings.SaveFile(base, outPath); err != nil {
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

	ext := confFormatExt(cfg.Format, ".yml")
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

	ext := confFormatExt(cfg.Format, ".json")
	outPath := filepath.Join(outDir, "yggdrasil.conf")

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

func applyPreset(obj *gsettings.Obj, preset gsettings.GoConfGeneratePresetEnum) {
	switch preset {
	case gsettings.GoConfGeneratePresetBasic:
		obj.Yggdrasil.Listen = nil
		obj.Yggdrasil.Inputs = nil
		obj.Yggdrasil.AllowedPublicKeys = nil
		obj.Yggdrasil.AdminListen = ""
		obj.Yggdrasil.If = gsettings.YggdrasilIfObj{}
		obj.Yggdrasil.Node = gsettings.YggdrasilNodeObj{}
		obj.Yggdrasil.LogLookups = false
		obj.Yggdrasil.CoreStopTimeout = 0
		obj.Yggdrasil.RstQueueSize = 0
		obj.Yggdrasil.Multicast = gsettings.YggdrasilMulticastObj{}
		obj.Yggdrasil.Socks = gsettings.YggdrasilSocksObj{}
		obj.Yggdrasil.Peers.Interface = nil
		obj.Yggdrasil.Peers.Manager = gsettings.YggdrasilPeersManagerObj{}
		obj.Log = gsettings.LogObj{}

	case gsettings.GoConfGeneratePresetMedium:
		obj.Yggdrasil.Inputs = nil
		obj.Yggdrasil.AllowedPublicKeys = nil
		obj.Yggdrasil.AdminListen = ""
		obj.Yggdrasil.Node = gsettings.YggdrasilNodeObj{}
		obj.Yggdrasil.Socks = gsettings.YggdrasilSocksObj{}
		obj.Yggdrasil.Peers.Interface = nil
		obj.Yggdrasil.Peers.Manager = gsettings.YggdrasilPeersManagerObj{}

	case gsettings.GoConfGeneratePresetFull:
		// keep everything
	}
}

// //

func confFormatExt(format gsettings.GoConfExportFormatEnum, defaultExt string) string {
	switch format {
	case gsettings.GoConfExportFormatJson:
		return ".json"
	case gsettings.GoConfExportFormatYml:
		return ".yml"
	case gsettings.GoConfExportFormatConf:
		return ".conf"
	default:
		return defaultExt
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
