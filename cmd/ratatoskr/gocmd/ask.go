package gocmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const defaultAskTimeout = 30 * time.Second

// //

func askCmd(cfg *gsettings.GoAskObj) (bool, error) {
	if cfg.Addr == "" {
		return false, nil
	}
	return true, askRun(cfg)
}

// //

func askRun(cfg *gsettings.GoAskObj) error {
	if len(cfg.Peer) == 0 {
		return fmt.Errorf("at least one peer is required (-go.ask.peer)")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultAskTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	nodeCfg := yggconfig.GenerateConfig()
	nodeCfg.AdminListen = "none"
	nodeCfg.IfName = "none"

	logger := &cliLoggerObj{}

	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx:             ctx,
		Config:          nodeCfg,
		Logger:          logger,
		CoreStopTimeout: 5 * time.Second,
		Peers: &peermgr.ConfigObj{
			Peers:     cfg.Peer,
			BatchSize: len(cfg.Peer),
		},
	})
	if err != nil {
		return fmt.Errorf("start node: %w", err)
	}
	defer func() { go node.Close() }()

	if err := askWaitPeers(ctx, node, len(cfg.Peer)); err != nil {
		return err
	}

	// Query NodeInfo
	var result *ninfo.AskResultObj
	var askErr error

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, askErr = node.AskAddr(ctx, cfg.Addr)
	}()

	frame := 0
	ticker := time.NewTicker(100 * time.Millisecond)

query:
	for {
		select {
		case <-done:
			clearLine()
			break query
		case <-ticker.C:
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c querying %s... %s remaining  ",
				spinnerFrames[frame%len(spinnerFrames)], cfg.Addr, formatRemaining(remaining))
			frame++
		case <-ctx.Done():
			clearLine()
			break query
		}
	}

	ticker.Stop()
	<-done

	if askErr != nil {
		return fmt.Errorf("ask failed: %w", askErr)
	}
	if result == nil {
		return fmt.Errorf("ask returned no result")
	}

	return outputAsk(cfg.Addr, result, cfg.Format)
}

// //

func askWaitPeers(ctx context.Context, node *ratatoskr.Obj, total int) error {
	frame := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			snap := node.Snapshot()
			connected := 0
			for _, p := range snap.Peers {
				if p.Up {
					connected++
				}
			}
			if connected > 0 {
				clearLine()
				return nil
			}
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c connecting %d/%d peers... %s remaining  ",
				spinnerFrames[frame%len(spinnerFrames)], connected, total, formatRemaining(remaining))
			frame++
		case <-ctx.Done():
			clearLine()
			return fmt.Errorf("timeout waiting for peer connection")
		}
	}
}

// // // // // // // // // //

type askResultJSON struct {
	Target   string         `json:"target"`
	RTT      float64        `json:"rtt_ms"`
	Version  string         `json:"version,omitempty"`
	Software *askSoftJSON   `json:"software,omitempty"`
	Sigils   map[string]any `json:"sigils,omitempty"`
	Extra    map[string]any `json:"extra,omitempty"`
}

type askSoftJSON struct {
	Name     string `json:"name,omitempty"`
	Version  string `json:"version,omitempty"`
	Platform string `json:"platform,omitempty"`
	Arch     string `json:"arch,omitempty"`
}

// //

func outputAsk(target string, result *ninfo.AskResultObj, format gsettings.GoAskFormatEnum) error {
	if format == gsettings.GoAskFormatJson {
		return outputAskJSON(target, result)
	}
	return outputAskText(target, result)
}

// //

func outputAskJSON(target string, result *ninfo.AskResultObj) error {
	out := askResultJSON{
		Target:  target,
		RTT:     float64(result.RTT.Microseconds()) / 1000.0,
		Version: result.Node.Version,
		Extra:   result.Node.Extra,
	}

	if result.Software != nil {
		out.Software = &askSoftJSON{
			Name:     result.Software.Name,
			Version:  result.Software.Version,
			Platform: result.Software.Platform,
			Arch:     result.Software.Arch,
		}
	}

	if len(result.Node.Sigils) > 0 {
		out.Sigils = make(map[string]any, len(result.Node.Sigils))
		for name, sg := range result.Node.Sigils {
			out.Sigils[name] = sg.Params()
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// //

func outputAskText(target string, result *ninfo.AskResultObj) error {
	rtt := fmt.Sprintf("%.2fms", float64(result.RTT.Microseconds())/1000.0)

	fmt.Printf("Target:   %s\n", target)
	fmt.Printf("RTT:      %s\n", rtt)

	if result.Node.Version != "" {
		fmt.Printf("Version:  %s\n", result.Node.Version)
	}

	if result.Software != nil {
		fmt.Println()
		fmt.Println("Software:")
		if result.Software.Name != "" {
			fmt.Printf("  Name:     %s\n", result.Software.Name)
		}
		if result.Software.Version != "" {
			fmt.Printf("  Version:  %s\n", result.Software.Version)
		}
		if result.Software.Platform != "" {
			fmt.Printf("  Platform: %s\n", result.Software.Platform)
		}
		if result.Software.Arch != "" {
			fmt.Printf("  Arch:     %s\n", result.Software.Arch)
		}
	}

	if len(result.Node.Sigils) > 0 {
		fmt.Println()
		fmt.Println("Sigils:")
		for name, sg := range result.Node.Sigils {
			val, _ := json.Marshal(sg.Params())
			fmt.Printf("  %s: %s\n", name, val)
		}
	}

	if len(result.Node.Extra) > 0 {
		fmt.Println()
		fmt.Println("Extra:")
		for k, v := range result.Node.Extra {
			val, _ := json.Marshal(v)
			fmt.Printf("  %s: %s\n", k, val)
		}
	}

	return nil
}
