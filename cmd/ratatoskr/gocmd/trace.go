package gocmd

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
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

func traceCmd(cfg *gsettings.TraceObj) error {
	if err := validateTraceParams(cfg); err != nil {
		return err
	}
	applyTraceDefaults(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	node, tr, err := bootNode(ctx, cfg.Peer)
	if err != nil {
		return err
	}
	defer node.Close()
	defer tr.Close()

	if cfg.Scan {
		return runScan(ctx, tr, cfg)
	}
	return runTrace(ctx, tr, cfg)
}

// //

func validateTraceParams(cfg *gsettings.TraceObj) error {
	if cfg.Scan && cfg.Trace != "" {
		return fmt.Errorf("specify either -go.trace.scan or -go.trace.trace, not both")
	}
	if !cfg.Scan && cfg.Trace == "" {
		return fmt.Errorf("specify -go.trace.scan or -go.trace.trace <pubkey>")
	}

	if cfg.Peer == "" {
		return fmt.Errorf("missing -go.trace.peer (yggdrasil peer URI)")
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

func applyTraceDefaults(cfg *gsettings.TraceObj) {
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

func runScan(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.TraceObj) error {
	ch := make(chan traceroute.TreeProgressObj, 16)

	var result *traceroute.TreeResultObj
	var scanErr error
	var done atomic.Bool

	go func() {
		result, scanErr = tr.TreeChan(ctx, cfg.MaxDepth, cfg.Concurrency, ch)
		done.Store(true)
	}()

	frame := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	lastTotal := 0
	lastDepth := 0

	for {
		select {
		case p := <-ch:
			if p.Found > 0 {
				lastTotal = p.Total
				lastDepth = p.Depth
			}
			if p.Done {
				clearLine()
				goto finish
			}
		case <-ticker.C:
			if done.Load() {
				for len(ch) > 0 {
					<-ch
				}
				clearLine()
				goto finish
			}
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c scanning... %s remaining | depth: %d | nodes: %d  ",
				spinnerFrames[frame%len(spinnerFrames)], formatRemaining(remaining), lastDepth, lastTotal)
			frame++
		case <-ctx.Done():
			clearLine()
			goto finish
		}
	}

finish:
	if scanErr != nil {
		return scanErr
	}
	if result == nil {
		return fmt.Errorf("scan returned no result")
	}
	return outputScan(result, cfg.Format)
}

// //

func runTrace(ctx context.Context, tr *traceroute.Obj, cfg *gsettings.TraceObj) error {
	keyBytes, _ := hex.DecodeString(cfg.Trace)
	pubKey := ed25519.PublicKey(keyBytes)

	var result *traceroute.TraceResultObj
	var traceErr error
	var done atomic.Bool

	go func() {
		result, traceErr = tr.Trace(ctx, pubKey)
		done.Store(true)
	}()

	frame := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if done.Load() {
				clearLine()
				goto finish
			}
			dl, _ := ctx.Deadline()
			remaining := time.Until(dl)
			if remaining < 0 {
				remaining = 0
			}
			fmt.Fprintf(os.Stderr, "\r%c tracing... %s remaining  ",
				spinnerFrames[frame%len(spinnerFrames)], formatRemaining(remaining))
			frame++
		case <-ctx.Done():
			clearLine()
			goto finish
		}
	}

finish:
	if traceErr != nil {
		return traceErr
	}
	if result == nil {
		return fmt.Errorf("trace returned no result")
	}
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

func outputScan(result *traceroute.TreeResultObj, format gsettings.GoTraceFormatEnum) error {
	if format == gsettings.GoTraceFormatJson {
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

func outputTrace(target string, result *traceroute.TraceResultObj, format gsettings.GoTraceFormatEnum) error {
	if format == gsettings.GoTraceFormatJson {
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

// //

// noopLoggerObj discards all log output.
type noopLoggerObj struct{}

func (noopLoggerObj) Printf(string, ...interface{}) {}
func (noopLoggerObj) Println(...interface{})        {}
func (noopLoggerObj) Infof(string, ...interface{})  {}
func (noopLoggerObj) Infoln(...interface{})         {}
func (noopLoggerObj) Warnf(string, ...interface{})  {}
func (noopLoggerObj) Warnln(...interface{})         {}
func (noopLoggerObj) Errorf(string, ...interface{}) {}
func (noopLoggerObj) Errorln(...interface{})        {}
func (noopLoggerObj) Debugf(string, ...interface{}) {}
func (noopLoggerObj) Debugln(...interface{})        {}
func (noopLoggerObj) Traceln(...interface{})        {}
