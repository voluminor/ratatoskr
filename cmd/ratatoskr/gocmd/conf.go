package gocmd

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hjson/hjson-go/v4"
	"gopkg.in/yaml.v3"

	cmdsettings "github.com/voluminor/ratatoskr/cmd/ratatoskr/gsettings"
	rsettings "github.com/voluminor/ratatoskr/cmd/ratatoskr/target/settings"
	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// // // // // // // // // //

func confCmd(cfg *cmdsettings.GoConfObj) (bool, error) {
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

func confGenerate(cfg *cmdsettings.GoConfGenerateObj) error {
	runtimeCfg := runtimeConfigFromNode(yggconfig.GenerateConfig())
	var data any = runtimeCfg
	switch cfg.Preset {
	case cmdsettings.GoConfGeneratePresetBasic:
		data = presetBasic(runtimeCfg)
	case cmdsettings.GoConfGeneratePresetMedium:
		data = presetMedium(runtimeCfg)
	}
	outPath, err := saveRatatoskrConfig(data, cfg.Path, cfg.Format)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

func confImport(cfg *cmdsettings.GoConfImportObj) error {
	file, err := os.Open(cfg.From)
	if err != nil {
		return fmt.Errorf("open yggdrasil config: %w", err)
	}
	defer func() { _ = file.Close() }()
	nodeCfg := &yggconfig.NodeConfig{}
	if _, err = nodeCfg.ReadFrom(file); err != nil {
		return fmt.Errorf("parse yggdrasil config: %w", err)
	}
	outPath, err := saveRatatoskrConfig(runtimeConfigFromNode(nodeCfg), cfg.To, cfg.Format)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

func confExport(cfg *cmdsettings.GoConfExportObj) error {
	runtimeCfg, err := rsettings.ParseFile(cfg.From)
	if err != nil {
		return fmt.Errorf("read ratatoskr config: %w", err)
	}
	nodeCfg, err := nodeConfigFromRuntime(runtimeCfg)
	if err != nil {
		return err
	}
	dir, err := validateOutputDir(cfg.To)
	if err != nil {
		return err
	}
	ext := formatExt(cfg.Format)
	outPath := filepath.Join(dir, "yggdrasil"+ext)
	data, err := marshalNodeConfig(nodeCfg, ext)
	if err != nil {
		return err
	}
	if err = os.WriteFile(outPath, data, 0o600); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Fprintf(os.Stderr, "written: %s\n", outPath)
	return nil
}

func runtimeConfigFromNode(node *yggconfig.NodeConfig) *rsettings.ConfigObj {
	cfg := rsettings.New()
	if node == nil {
		return &cfg
	}
	cfg.Yggdrasil.Key.Text = hex.EncodeToString(node.PrivateKey)
	cfg.Yggdrasil.Key.Path = node.PrivateKeyPath
	cfg.Yggdrasil.Listen = append([]string(nil), node.Listen...)
	cfg.Yggdrasil.Inputs = nil
	cfg.Yggdrasil.Peers.Url = append([]string(nil), node.Peers...)
	cfg.Yggdrasil.Peers.Interface = node.InterfacePeers
	cfg.Yggdrasil.AllowedPublicKeys = append([]string(nil), node.AllowedPublicKeys...)
	cfg.Yggdrasil.AdminListen = node.AdminListen
	cfg.Yggdrasil.If.Name = node.IfName
	cfg.Yggdrasil.If.Mtu = node.IfMTU
	cfg.Yggdrasil.Node.Info = node.NodeInfo
	cfg.Yggdrasil.Node.Privacy = node.NodeInfoPrivacy
	cfg.Yggdrasil.LogLookups = node.LogLookups
	if len(node.MulticastInterfaces) > 0 {
		entry := node.MulticastInterfaces[0]
		cfg.Yggdrasil.Multicast.Regex = entry.Regex
		cfg.Yggdrasil.Multicast.Beacon = entry.Beacon
		cfg.Yggdrasil.Multicast.Listen = entry.Listen
		cfg.Yggdrasil.Multicast.Port = entry.Port
		cfg.Yggdrasil.Multicast.Priority = uint16(entry.Priority)
		cfg.Yggdrasil.Multicast.Password = entry.Password
	}
	return &cfg
}

func nodeConfigFromRuntime(cfg *rsettings.ConfigObj) (*yggconfig.NodeConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	node := yggconfig.GenerateConfig()
	if cfg.Yggdrasil.Key.Text != "" {
		key, err := hex.DecodeString(cfg.Yggdrasil.Key.Text)
		if err != nil || len(key) != 64 {
			return nil, fmt.Errorf("invalid yggdrasil.key.text: expected 128 hex characters")
		}
		node.PrivateKey = key
	}
	node.PrivateKeyPath = cfg.Yggdrasil.Key.Path
	node.Listen = append([]string(nil), cfg.Yggdrasil.Listen...)
	node.Peers = append([]string(nil), cfg.Yggdrasil.Peers.Url...)
	node.InterfacePeers = cfg.Yggdrasil.Peers.Interface
	node.AllowedPublicKeys = append([]string(nil), cfg.Yggdrasil.AllowedPublicKeys...)
	node.AdminListen = cfg.Yggdrasil.AdminListen
	node.IfName = cfg.Yggdrasil.If.Name
	node.IfMTU = cfg.Yggdrasil.If.Mtu
	node.NodeInfo = cfg.Yggdrasil.Node.Info
	node.NodeInfoPrivacy = cfg.Yggdrasil.Node.Privacy
	node.LogLookups = cfg.Yggdrasil.LogLookups
	node.MulticastInterfaces = []yggconfig.MulticastInterfaceConfig{{
		Regex: cfg.Yggdrasil.Multicast.Regex, Beacon: cfg.Yggdrasil.Multicast.Beacon,
		Listen: cfg.Yggdrasil.Multicast.Listen, Port: cfg.Yggdrasil.Multicast.Port,
		Priority: uint64(cfg.Yggdrasil.Multicast.Priority), Password: cfg.Yggdrasil.Multicast.Password,
	}}
	return node, nil
}

func presetBasic(obj *rsettings.ConfigObj) map[string]any {
	return map[string]any{"yggdrasil": map[string]any{
		"key":   map[string]any{"text": obj.Yggdrasil.Key.Text},
		"peers": map[string]any{"url": obj.Yggdrasil.Peers.Url},
	}}
}

func presetMedium(obj *rsettings.ConfigObj) map[string]any {
	return map[string]any{
		"yggdrasil": map[string]any{
			"key": map[string]any{"text": obj.Yggdrasil.Key.Text}, "listen": obj.Yggdrasil.Listen,
			"inputs": obj.Yggdrasil.Inputs, "peers": map[string]any{"url": obj.Yggdrasil.Peers.Url},
			"if":                map[string]any{"mtu": obj.Yggdrasil.If.Mtu},
			"core_stop_timeout": obj.Yggdrasil.CoreStopTimeout.String(), "rst_queue_size": obj.Yggdrasil.RstQueueSize,
		},
		"log": map[string]any{
			"level":  map[string]any{"console": obj.Log.Level.Console.String(), "file": obj.Log.Level.File.String()},
			"output": obj.Log.Output.String(),
		},
	}
}

func saveRatatoskrConfig(value any, dirText string, format cmdsettings.GoConfFormatEnum) (string, error) {
	dir, err := validateOutputDir(dirText)
	if err != nil {
		return "", err
	}
	ext := formatExt(format)
	path := filepath.Join(dir, "ratatoskr-config"+ext)
	var data []byte
	if cfg, ok := value.(*rsettings.ConfigObj); ok {
		switch ext {
		case ".yml":
			data, err = cfg.RenderYAML(false)
		case ".json":
			data, err = cfg.RenderJSON(false)
		default:
			data, err = cfg.RenderHJSON(false)
		}
	} else {
		switch ext {
		case ".yml":
			data, err = yaml.Marshal(value)
		case ".json":
			data, err = json.MarshalIndent(value, "", "  ")
		default:
			data, err = hjson.MarshalWithOptions(value, hjson.DefaultOptions())
		}
	}
	if err != nil {
		return "", fmt.Errorf("render config: %w", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

func validateOutputDir(path string) (string, error) {
	if path == "" {
		path = "."
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("output path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("output path is not a directory: %s", path)
	}
	return filepath.Abs(path)
}

func formatExt(format cmdsettings.GoConfFormatEnum) string {
	switch format {
	case "json":
		return ".json"
	case "conf":
		return ".conf"
	default:
		return ".yml"
	}
}

func marshalNodeConfig(cfg *yggconfig.NodeConfig, ext string) ([]byte, error) {
	var data []byte
	var err error
	switch ext {
	case ".conf":
		data, err = hjson.MarshalWithOptions(cfg, hjson.DefaultOptions())
	case ".yml":
		var jsonValue any
		data, err = json.Marshal(cfg)
		if err == nil {
			err = json.Unmarshal(data, &jsonValue)
		}
		if err == nil {
			data, err = yaml.Marshal(jsonValue)
		}
	default:
		data, err = json.MarshalIndent(cfg, "", "  ")
	}
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return append(data, '\n'), nil
}
