package gocmd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const defaultPeerInfoTimeout = 10 * time.Second

// //

func peerInfoCmd(cfg *gsettings.GoPeerInfoObj) (bool, error) {
	if len(cfg.Peer) == 0 {
		return false, nil
	}
	return true, peerInfo(cfg)
}

// //

func peerInfo(cfg *gsettings.GoPeerInfoObj) error {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultPeerInfoTimeout
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

	// Wait for connections or timeout
	frame := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

wait:
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
			if connected >= len(cfg.Peer) {
				clearLine()
				break wait
			}
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c probing %d/%d peers... %s remaining  ",
				spinnerFrames[frame%len(spinnerFrames)], connected, len(cfg.Peer), formatRemaining(remaining))
			frame++
		case <-ctx.Done():
			clearLine()
			break wait
		}
	}

	snap := node.Snapshot()
	return outputPeerInfo(snap.Peers, cfg.Format)
}

// //

type peerInfoJSON struct {
	URI       string  `json:"uri"`
	Up        bool    `json:"up"`
	Inbound   bool    `json:"inbound,omitempty"`
	Key       string  `json:"key,omitempty"`
	LatencyMs float64 `json:"latency_ms,omitempty"`
	RXBytes   uint64  `json:"rx_bytes,omitempty"`
	TXBytes   uint64  `json:"tx_bytes,omitempty"`
	LastError string  `json:"last_error,omitempty"`
}

// //

func outputPeerInfo(peers []ratatoskr.PeerSnapshotObj, format gsettings.GoAskFormatEnum) error {
	if format == gsettings.GoAskFormatJson {
		out := make([]peerInfoJSON, len(peers))
		for i, p := range peers {
			out[i] = peerInfoJSON{
				URI:       p.URI,
				Up:        p.Up,
				Inbound:   p.Inbound,
				Key:       p.Key,
				LatencyMs: float64(p.Latency.Microseconds()) / 1000.0,
				RXBytes:   p.RXBytes,
				TXBytes:   p.TXBytes,
				LastError: p.LastError,
			}
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// Text output
	fmt.Fprintf(os.Stderr, "%-40s %-6s %-10s %-16s %10s %10s\n",
		"URI", "STATUS", "LATENCY", "KEY", "RX", "TX")

	for _, p := range peers {
		status := "down"
		if p.Up {
			status = "up"
		}

		latency := "-"
		if p.Latency > 0 {
			latency = fmt.Sprintf("%.2fms", float64(p.Latency.Microseconds())/1000.0)
		}

		key := "-"
		if p.Key != "" {
			keyBytes, _ := hex.DecodeString(p.Key)
			if len(keyBytes) > 8 {
				key = p.Key[:16] + "..."
			}
		}

		rx := formatBytes(p.RXBytes)
		tx := formatBytes(p.TXBytes)

		fmt.Printf("%-40s %-6s %-10s %-16s %10s %10s\n",
			p.URI, status, latency, key, rx, tx)
	}

	return nil
}

// //

func formatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
