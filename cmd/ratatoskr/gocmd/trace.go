package gocmd

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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
	defaultTraceTimeout     = 40 * time.Second
	minTraceTimeout         = 100 * time.Millisecond
	defaultTraceMaxDepth    = 3
	defaultTraceConcurrency = 64
)

// //

func traceCmd(cfg *gsettings.GoTracerouteObj) error {
	if err := validateTraceParams(cfg); err != nil {
		return err
	}
	applyTraceDefaults(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	fmt.Fprintln(os.Stderr, "booting node...")
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

	if err := waitForPeers(ctx, tr); err != nil {
		return err
	}

	if err := waitForRouting(ctx, tr); err != nil {
		return err
	}
	tr.FlushCache()
	fmt.Fprintln(os.Stderr, "ready")

	if cfg.Scan {
		return runScan(ctx, tr, cfg)
	}
	if cfg.Ping != "" {
		return runPing(ctx, tr, cfg)
	}
	return runTrace(ctx, tr, cfg)
}

// //

func validateTraceParams(cfg *gsettings.GoTracerouteObj) error {
	modes := boolToInt(cfg.Scan) + boolToInt(cfg.Trace != "") + boolToInt(cfg.Ping != "")
	if modes != 1 {
		return fmt.Errorf("specify exactly one of -go.traceroute.scan, -go.traceroute.trace, or -go.traceroute.ping")
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

	for _, key := range []string{cfg.Trace, cfg.Ping} {
		if key != "" {
			raw, err := hex.DecodeString(key)
			if err != nil || len(raw) != ed25519.PublicKeySize {
				return fmt.Errorf("invalid target key: must be 64-char hex (32 bytes)")
			}
		}
	}

	return nil
}

const defaultPingCount = 4

// //

func applyTraceDefaults(cfg *gsettings.GoTracerouteObj) {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTraceTimeout
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = defaultTraceMaxDepth
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = defaultTraceConcurrency
	}
	if cfg.Count == 0 {
		cfg.Count = defaultPingCount
	}
}

// //

func bootNode(ctx context.Context, peerURI string) (*ratatoskr.Obj, *traceroute.Obj, error) {
	nodeCfg := yggconfig.GenerateConfig()
	nodeCfg.AdminListen = "none"

	logger := &cliLoggerObj{}

	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx:             ctx,
		Config:          nodeCfg,
		Logger:          logger,
		CoreStopTimeout: 5 * time.Second,
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

func waitForPeers(ctx context.Context, tr *traceroute.Obj) error {
	frame := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	peerUp := false
	prevTree := 0
	stableTicks := 0

	for {
		select {
		case <-ticker.C:
			if !peerUp {
				for _, p := range tr.Peers() {
					if p.Up {
						peerUp = true
						break
					}
				}
			}

			if peerUp {
				n := len(tr.SpanningTree())
				if n > 1 && n == prevTree {
					stableTicks++
				} else {
					stableTicks = 0
				}
				prevTree = n
				if stableTicks >= 3 {
					clearLine()
					return nil
				}
			}

			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c connecting... %s remaining | tree: %d nodes  ",
				spinnerFrames[frame%len(spinnerFrames)], formatRemaining(remaining), prevTree)
			frame++
		case <-ctx.Done():
			clearLine()
			return fmt.Errorf("timeout waiting for peer connection")
		}
	}
}

// waitForRouting probes the direct peer with Tree(depth=2) until
// debug_remoteGetPeers responds. Sends Lookups to stimulate DHT convergence.
func waitForRouting(ctx context.Context, tr *traceroute.Obj) error {
	var peerKey ed25519.PublicKey
	for _, p := range tr.Peers() {
		if p.Up && len(p.Key) == ed25519.PublicKeySize {
			peerKey = p.Key
			break
		}
	}
	if peerKey == nil {
		return fmt.Errorf("no active peer for routing probe")
	}

	frame := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	tr.Lookup(peerKey)
	probing := false
	probeDone := make(chan bool, 1)

	for {
		select {
		case ok := <-probeDone:
			probing = false
			if ok {
				clearLine()
				return nil
			}
			tr.Lookup(peerKey)

		case <-ticker.C:
			if !probing {
				probing = true
				go func() {
					tr.FlushCache()
					result, _ := tr.Tree(ctx, 2, 1)
					probeDone <- result != nil && result.Total > 0 && !hasUnreachable(result.Root)
				}()
			}
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c establishing route... %s remaining  ",
				spinnerFrames[frame%len(spinnerFrames)], formatRemaining(remaining))
			frame++

		case <-ctx.Done():
			clearLine()
			return fmt.Errorf("timeout waiting for route establishment")
		}
	}
}

func hasUnreachable(root *traceroute.NodeObj) bool {
	if root == nil {
		return false
	}
	for _, ch := range root.Children {
		if ch.Unreachable {
			return true
		}
	}
	return false
}

// //

func runScan(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.GoTracerouteObj) error {
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

	currentDepth := 0
	depthFound := 0
	totalNodes := 0

scan:
	for {
		select {
		case p := <-ch:
			if p.Done {
				clearLine()
				break scan
			}
			if p.Found > 0 {
				if p.Depth > currentDepth {
					if depthFound > 0 {
						clearLine()
						fmt.Fprintf(os.Stderr, "depth %d: %d nodes found\n", currentDepth, depthFound)
					}
					currentDepth = p.Depth
					depthFound = p.Found
				} else {
					depthFound += p.Found
				}
				totalNodes = p.Total
			}
		case <-ticker.C:
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c scanning depth %d... %s remaining | %d nodes  ",
				spinnerFrames[frame%len(spinnerFrames)], currentDepth, formatRemaining(remaining), totalNodes)
			frame++
		case <-ctx.Done():
			clearLine()
			break scan
		}
	}

	ticker.Stop()
	<-done

	if depthFound > 0 {
		fmt.Fprintf(os.Stderr, "depth %d: %d nodes found\n", currentDepth, depthFound)
	}
	if scanErr != nil {
		return scanErr
	}
	if result == nil {
		return fmt.Errorf("scan returned no result")
	}
	fmt.Fprintf(os.Stderr, "scan complete: %d nodes\n", result.Total)
	return outputScan(result, cfg.Format)
}

// //

func runTrace(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.GoTracerouteObj) error {
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

trace:
	for {
		select {
		case <-done:
			clearLine()
			break trace
		case <-ticker.C:
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c tracing %s... %s remaining  ",
				spinnerFrames[frame%len(spinnerFrames)], cfg.Trace[:16], formatRemaining(remaining))
			frame++
		case <-ctx.Done():
			clearLine()
			break trace
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
	fmt.Fprintln(os.Stderr, "trace complete")
	return outputTrace(cfg.Trace, result, cfg.Format)
}

// //

func runPing(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.GoTracerouteObj) error {
	keyBytes, _ := hex.DecodeString(cfg.Ping)
	pubKey := ed25519.PublicKey(keyBytes)

	fmt.Fprintf(os.Stderr, "PING %s... via %s\n", cfg.Ping[:16], cfg.Peer)

	type pingResultObj struct {
		Seq int     `json:"seq"`
		RTT float64 `json:"rtt_ms"`
		Err string  `json:"error,omitempty"`
	}

	results := make([]pingResultObj, 0, cfg.Count)
	var minRTT, maxRTT, sumRTT float64

	for i := range cfg.Count {
		start := time.Now()
		_, err := tr.Trace(ctx, pubKey)
		rtt := time.Since(start)
		ms := float64(rtt.Microseconds()) / 1000.0

		pr := pingResultObj{Seq: i + 1, RTT: ms}
		if err != nil {
			pr.Err = err.Error()
			pr.RTT = 0
			if cfg.Format == gsettings.GoPeerInfoFormatText {
				fmt.Fprintf(os.Stderr, "seq %d: error %s\n", i+1, err)
			}
		} else {
			if cfg.Format == gsettings.GoPeerInfoFormatText {
				fmt.Fprintf(os.Stderr, "seq %d: %.2f ms\n", i+1, ms)
			}
			if minRTT == 0 || ms < minRTT {
				minRTT = ms
			}
			if ms > maxRTT {
				maxRTT = ms
			}
			sumRTT += ms
		}
		results = append(results, pr)

		if ctx.Err() != nil {
			break
		}
	}

	successful := 0
	for _, r := range results {
		if r.Err == "" {
			successful++
		}
	}

	avgRTT := 0.0
	if successful > 0 {
		avgRTT = sumRTT / float64(successful)
	}

	if cfg.Format == gsettings.GoPeerInfoFormatJson {
		out := struct {
			Target string          `json:"target"`
			Pings  []pingResultObj `json:"pings"`
			Stats  struct {
				Count int     `json:"count"`
				OK    int     `json:"ok"`
				Min   float64 `json:"min_ms"`
				Avg   float64 `json:"avg_ms"`
				Max   float64 `json:"max_ms"`
			} `json:"stats"`
		}{
			Target: cfg.Ping,
			Pings:  results,
		}
		out.Stats.Count = len(results)
		out.Stats.OK = successful
		out.Stats.Min = minRTT
		out.Stats.Avg = avgRTT
		out.Stats.Max = maxRTT

		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Fprintf(os.Stderr, "--- %d pings, %d ok, min/avg/max = %.2f/%.2f/%.2f ms\n",
		len(results), successful, minRTT, avgRTT, maxRTT)
	return nil
}

// //

func printTree(n *traceroute.NodeObj, prefix string, isRoot bool) {
	if n == nil {
		return
	}

	label := hex.EncodeToString(n.Key) + "    "
	if isRoot {
		label = "root"
	}

	var extra string
	if n.Unreachable {
		extra = " [unreachable]"
	} else if ms := float64(n.RTT.Microseconds()) / 1000.0; ms > 0 {
		extra = fmt.Sprintf(" %.2f ms", ms)
	}
	if len(n.Children) > 0 {
		extra += fmt.Sprintf(" (%d)", len(n.Children))
	}

	fmt.Printf("%s%s\n", label, extra)

	for i, ch := range n.Children {
		last := i == len(n.Children)-1
		if last {
			fmt.Printf("%s└── ", prefix)
			printTree(ch, prefix+"    ", false)
		} else {
			fmt.Printf("%s├── ", prefix)
			printTree(ch, prefix+"│   ", false)
		}
	}
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

func outputScan(result *traceroute.TreeResultObj, format gsettings.GoPeerInfoFormatEnum) error {
	if format == gsettings.GoPeerInfoFormatJson {
		data, err := json.MarshalIndent(nodeToScanJSON(result.Root), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	printTree(result.Root, "", true)
	fmt.Fprintf(os.Stderr, "total: %d nodes\n", result.Total)
	return nil
}

func outputTrace(target string, result *traceroute.TraceResultObj, format gsettings.GoPeerInfoFormatEnum) error {
	if format == gsettings.GoPeerInfoFormatJson {
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
				key = hex.EncodeToString(h.Key) + "    "
			}
			fmt.Printf("  %d  port:%-5d  %s\n", h.Index, h.Port, key)
		}
	}
	return nil
}
