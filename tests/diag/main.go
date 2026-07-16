package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
)

// // // // // // // // // //

func buildNodeConfig(cfg configObj) *yggconfig.NodeConfig {
	nodeCfg := yggconfig.GenerateConfig()
	nodeCfg.AdminListen = "none"
	nodeCfg.Peers = append([]string(nil), cfg.Peers...)
	if cfg.IfMTU > 0 {
		nodeCfg.IfMTU = cfg.IfMTU
	}
	nodeCfg.NodeInfo = map[string]interface{}{
		"name": cfg.Name,
		"role": "ratatoskr diagnostic node",
	}
	return nodeCfg
}

func run(ctx context.Context, cfg configObj, timeout time.Duration) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	log := newLogger(cfg.Name)
	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx:          runCtx,
		Config:       buildNodeConfig(cfg),
		Logger:       log,
		CloseTimeout: timeout,
	})
	if err != nil {
		return fmt.Errorf("start ratatoskr: %w", err)
	}
	defer func() { _ = node.Close() }()

	server := newServer(cfg, node, log, cancel)
	if err = server.start(runCtx); err != nil {
		return err
	}
	defer server.close()

	<-runCtx.Done()
	return nil
}

func main() {
	configPath := flag.String("config", "/data/config.json", "path to diagnostic node config")
	flag.Parse()

	cfg, timeout, err := loadConfig(*configPath)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	ctx, cancel := notifyContext(context.Background())
	defer cancel()

	if err = run(ctx, cfg, timeout); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
