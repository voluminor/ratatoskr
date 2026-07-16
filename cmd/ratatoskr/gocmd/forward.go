package gocmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
	gsettings "github.com/voluminor/ratatoskr/cmd/ratatoskr/gsettings"
	"github.com/voluminor/ratatoskr/mod/forward"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

const defaultUDPSessionTimeout = 120 * time.Second

// //

func forwardCmd(cfg *gsettings.GoForwardObj) (bool, error) {
	if cfg.From == "" && cfg.To == "" {
		return false, nil
	}
	return true, forwardRun(cfg)
}

// //

func forwardRun(cfg *gsettings.GoForwardObj) error {
	if cfg.From == "" {
		return fmt.Errorf("missing -go.forward.from (local listen address)")
	}
	if cfg.To == "" {
		return fmt.Errorf("missing -go.forward.to (remote Yggdrasil address:port)")
	}
	if len(cfg.Peer) == 0 {
		return fmt.Errorf("missing -go.forward.peer (yggdrasil peer URIs)")
	}

	if _, errs := peermgr.ValidatePeers(cfg.Peer); len(errs) > 0 {
		return fmt.Errorf("invalid peer: %w", errs[0])
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	nodeCfg := yggconfig.GenerateConfig()
	nodeCfg.AdminListen = "none"
	nodeCfg.IfName = "none"
	nodeCfg.NodeInfoPrivacy = false
	nodeCfg.NodeInfo = map[string]interface{}{
		"type":      "forward",
		"ratatoskr": target.Version,
	}

	logger := &cliLoggerObj{}

	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx:          ctx,
		Config:       nodeCfg,
		Logger:       logger,
		CloseTimeout: 5 * time.Second,
		Peers: &peermgr.ConfigObj{
			Peers:     cfg.Peer,
			BatchSize: len(cfg.Peer),
		},
	})
	if err != nil {
		return fmt.Errorf("start node: %w", err)
	}
	defer func() { _ = node.Close() }()

	forwardCfg := forward.ConfigObj{
		Logger:     logger,
		Node:       node.Core(),
		UDPTimeout: defaultUDPSessionTimeout,
	}

	useTCP := cfg.Proto == gsettings.GoForwardProtoTcp || cfg.Proto == ""
	if useTCP {
		listenAddr, err := net.ResolveTCPAddr("tcp", cfg.From)
		if err != nil {
			return fmt.Errorf("invalid listen address: %w", err)
		}
		mappedAddr, err := net.ResolveTCPAddr("tcp", cfg.To)
		if err != nil {
			return fmt.Errorf("invalid remote address: %w", err)
		}
		forwardCfg.LocalTCP = []forward.TCPMappingObj{{Listen: listenAddr, Mapped: mappedAddr}}
		fmt.Fprintf(os.Stderr, "forwarding tcp %s → %s\n", cfg.From, cfg.To)
	} else {
		listenAddr, err := net.ResolveUDPAddr("udp", cfg.From)
		if err != nil {
			return fmt.Errorf("invalid listen address: %w", err)
		}
		mappedAddr, err := net.ResolveUDPAddr("udp", cfg.To)
		if err != nil {
			return fmt.Errorf("invalid remote address: %w", err)
		}
		forwardCfg.LocalUDP = []forward.UDPMappingObj{{Listen: listenAddr, Mapped: mappedAddr}}
		fmt.Fprintf(os.Stderr, "forwarding udp %s → %s\n", cfg.From, cfg.To)
	}

	mgr, err := forward.New(forwardCfg)
	if err != nil {
		return fmt.Errorf("start forwarding: %w", err)
	}

	fmt.Fprintln(os.Stderr, "running (Ctrl+C to stop)")
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "shutting down...")
	return mgr.Close()
}
