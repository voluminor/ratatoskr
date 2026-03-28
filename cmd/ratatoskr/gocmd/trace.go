package gocmd

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/core"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/traceroute"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const (
	defaultTraceTimeout     = 5 * time.Minute
	minTraceTimeout         = 100 * time.Millisecond
	defaultTraceMaxDepth    = 3
	defaultTraceConcurrency = 64
)

// //

func traceCmd(cfg *gsettings.TracerouteObj) error {
	if err := validateTraceParams(cfg); err != nil {
		return err
	}
	applyTraceDefaults(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	fmt.Fprintf(os.Stderr, "  booting node...\n")
	node, tr, err := bootNode(ctx, cfg.Peer)
	if err != nil {
		return err
	}
	defer func() {
		go func() {
			tr.Close()
			node.Close()
		}()
	}()

	if err := waitForPeers(ctx, node); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  peer connected\n")

	if cfg.Scan {
		return runScan(ctx, tr, cfg)
	}
	return runTrace(ctx, tr, cfg)
}

// //

func validateTraceParams(cfg *gsettings.TracerouteObj) error {
	if cfg.Scan && cfg.Trace != "" {
		return fmt.Errorf("specify either -go.traceroute.scan or -go.traceroute.trace, not both")
	}
	if !cfg.Scan && cfg.Trace == "" {
		return fmt.Errorf("specify -go.traceroute.scan or -go.traceroute.trace <pubkey>")
	}

	if cfg.Peer == "" {
		return fmt.Errorf("missing -go.traceroute.peer (yggdrasil peer URI)")
	}
	if _, errs := peermgr.ValidatePeers([]string{cfg.Peer}); len(errs) > 0 {
		return fmt.Errorf("invalid peer: %w", errs[0])
	}

	if cfg.Timeout != 0 && cfg.Timeout < minTraceTimeout {
		return fmt.Errorf("timeout must be >= %s", minTraceTimeout)
	}

	if cfg.Trace != "" {
		raw, err := hex.DecodeString(cfg.Trace)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return fmt.Errorf("invalid target key: must be 64-char hex (32 bytes)")
		}
	}

	return nil
}

func applyTraceDefaults(cfg *gsettings.TracerouteObj) {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTraceTimeout
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = defaultTraceMaxDepth
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = defaultTraceConcurrency
	}
}

// //

func bootNode(ctx context.Context, peerURI string) (*ratatoskr.Obj, *traceroute.Obj, error) {
	nodeCfg := yggconfig.GenerateConfig()
	nodeCfg.AdminListen = "none"

	logger := &noopLoggerObj{}

	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx:    ctx,
		Config: nodeCfg,
		Logger: logger,
		Peers: &peermgr.ConfigObj{
			Peers:     []string{peerURI},
			BatchSize: 1,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start node: %w", err)
	}

	coreNode := node.Interface.(*core.Obj)
	tr, err := traceroute.New(coreNode.UnsafeCore(), logger)
	if err != nil {
		node.Close()
		return nil, nil, fmt.Errorf("init traceroute: %w", err)
	}

	return node, tr, nil
}

// //

func waitForPeers(ctx context.Context, node *ratatoskr.Obj) error {
	frame := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if hasUpPeer(node) {
				clearLine()
				return nil
			}
			fmt.Fprintf(os.Stderr, "\r  %c connecting to peer...  ",
				spinnerFrames[frame%len(spinnerFrames)])
			frame++
		case <-ctx.Done():
			clearLine()
			return fmt.Errorf("timeout waiting for peer connection")
		}
	}
}

func hasUpPeer(node *ratatoskr.Obj) bool {
	for _, p := range node.GetPeers() {
		if p.Up {
			return true
		}
	}
	return false
}

// //

func runScan(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.TracerouteObj) error {
	ch := make(chan traceroute.TreeProgressObj, 16)

	var result *traceroute.TreeResultObj
	var scanErr error

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, scanErr = tr.TreeChan(ctx, cfg.MaxDepth, cfg.Concurrency, ch)
	}()

	frame := 0
	ticker := time.NewTicker(100 * time.Millisecond)

	lastTotal := 0
	lastDepth := 0
	printedDepth := 0

	loop := true
	for loop {
		select {
		case p, ok := <-ch:
			if !ok || p.Done {
				clearLine()
				loop = false
				continue
			}
			if p.Found > 0 {
				lastTotal = p.Total
				if p.Depth > lastDepth {
					clearLine()
					fmt.Fprintf(os.Stderr, "  depth %d: %d nodes found\n", lastDepth, lastTotal)
					printedDepth = lastDepth
				}
				lastDepth = p.Depth
			}
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "\r  %c scanning depth %d... (%d nodes)  ",
				spinnerFrames[frame%len(spinnerFrames)], lastDepth, lastTotal)
			frame++
		case <-ctx.Done():
			clearLine()
			loop = false
		}
	}

	ticker.Stop()
	<-done

	if lastDepth > printedDepth && lastTotal > 0 {
		fmt.Fprintf(os.Stderr, "  depth %d: %d nodes total\n", lastDepth, lastTotal)
	}
	if scanErr != nil {
		return scanErr
	}
	if result == nil {
		return fmt.Errorf("scan returned no result")
	}
	fmt.Fprintf(os.Stderr, "  scan complete: %d nodes\n", result.Total)
	return outputScan(result, cfg.Format)
}

// //

func runTrace(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.TracerouteObj) error {
	keyBytes, _ := hex.DecodeString(cfg.Trace)
	pubKey := ed25519.PublicKey(keyBytes)

	var result *traceroute.TraceResultObj
	var traceErr error

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, traceErr = tr.Trace(ctx, pubKey)
	}()

	frame := 0
	ticker := time.NewTicker(100 * time.Millisecond)

	loop := true
	for loop {
		select {
		case <-done:
			clearLine()
			loop = false
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "\r  %c tracing %s...  ",
				spinnerFrames[frame%len(spinnerFrames)], cfg.Trace[:16])
			frame++
		case <-ctx.Done():
			clearLine()
			loop = false
		}
	}

	ticker.Stop()
	<-done

	if traceErr != nil {
		return traceErr
	}
	if result == nil {
		return fmt.Errorf("trace returned no result")
	}
	fmt.Fprintf(os.Stderr, "  trace complete\n")
	return outputTrace(cfg.Trace, result, cfg.Format)
}

// //

func clearLine() {
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 80))
}

// //

type scanNodeJSON struct {
	Key         string          `json:"key"`
	Parent      string          `json:"parent,omitempty"`
	Depth       int             `json:"depth"`
	RTT         float64         `json:"rtt_ms,omitempty"`
	Unreachable bool            `json:"unreachable,omitempty"`
	Children    []*scanNodeJSON `json:"children,omitempty"`
}

type traceHopJSON struct {
	Key   string `json:"key,omitempty"`
	Port  uint64 `json:"port"`
	Index int    `json:"index"`
}

// //

func nodeToScanJSON(n *traceroute.NodeObj) *scanNodeJSON {
	if n == nil {
		return nil
	}
	j := &scanNodeJSON{
		Key:         hex.EncodeToString(n.Key),
		Parent:      hex.EncodeToString(n.Parent),
		Depth:       n.Depth,
		RTT:         float64(n.RTT.Microseconds()) / 1000.0,
		Unreachable: n.Unreachable,
	}
	if len(n.Children) > 0 {
		j.Children = make([]*scanNodeJSON, len(n.Children))
		for i, ch := range n.Children {
			j.Children[i] = nodeToScanJSON(ch)
		}
	}
	return j
}

// //

func outputScan(result *traceroute.TreeResultObj, format gsettings.GoTracerouteFormatEnum) error {
	if format == gsettings.GoTracerouteFormatJson {
		data, err := json.MarshalIndent(nodeToScanJSON(result.Root), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	nodes := result.Root.Flatten()
	for _, n := range nodes {
		indent := strings.Repeat("  ", n.Depth)
		key := hex.EncodeToString(n.Key)
		rtt := fmt.Sprintf("%.1fms", float64(n.RTT.Microseconds())/1000.0)
		status := ""
		if n.Unreachable {
			status = " [unreachable]"
		}
		fmt.Printf("%s%s  %s%s\n", indent, key[:16]+"...", rtt, status)
	}
	fmt.Fprintf(os.Stderr, "total: %d nodes\n", result.Total)
	return nil
}

func outputTrace(target string, result *traceroute.TraceResultObj, format gsettings.GoTracerouteFormatEnum) error {
	if format == gsettings.GoTracerouteFormatJson {
		out := struct {
			Target string          `json:"target"`
			Path   []*scanNodeJSON `json:"path,omitempty"`
			Hops   []traceHopJSON  `json:"hops,omitempty"`
		}{Target: target}

		if result.TreePath != nil {
			out.Path = make([]*scanNodeJSON, len(result.TreePath))
			for i, n := range result.TreePath {
				out.Path[i] = &scanNodeJSON{
					Key:    hex.EncodeToString(n.Key),
					Parent: hex.EncodeToString(n.Parent),
					Depth:  n.Depth,
					RTT:    float64(n.RTT.Microseconds()) / 1000.0,
				}
			}
		}
		if result.Hops != nil {
			out.Hops = make([]traceHopJSON, len(result.Hops))
			for i, h := range result.Hops {
				hop := traceHopJSON{Port: h.Port, Index: h.Index}
				if len(h.Key) > 0 {
					hop.Key = hex.EncodeToString(h.Key)
				}
				out.Hops[i] = hop
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
	if result.TreePath != nil {
		fmt.Fprintln(os.Stderr, "path:")
		for i, n := range result.TreePath {
			key := hex.EncodeToString(n.Key)
			rtt := fmt.Sprintf("%.1fms", float64(n.RTT.Microseconds())/1000.0)
			fmt.Printf("  %d  %s  %s\n", i, key[:16]+"...", rtt)
		}
	}
	if result.Hops != nil {
		fmt.Fprintln(os.Stderr, "hops:")
		for _, h := range result.Hops {
			key := "???"
			if len(h.Key) > 0 {
				key = hex.EncodeToString(h.Key)[:16] + "..."
			}
			fmt.Printf("  %d  port:%-5d  %s\n", h.Index, h.Port, key)
		}
	}
	return nil
}
