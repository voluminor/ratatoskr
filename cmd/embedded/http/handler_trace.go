package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/voluminor/ratatoskr/mod/traceroute"
)

// // // // // // // // // //

// traceNodeJSON is a recursive JSON representation of NodeObj.
// Unreachable is true when the node did not respond to a peer query in Tree().
type traceNodeJSON struct {
	Key         string           `json:"key"`
	Parent      string           `json:"parent,omitempty"`
	Depth       int              `json:"depth"`
	Sequence    uint64           `json:"sequence,omitempty"`
	RTT         float64          `json:"rtt_ms,omitempty"`
	Unreachable bool             `json:"unreachable,omitempty"`
	Children    []*traceNodeJSON `json:"children,omitempty"`
}

// traceHopJSON is a single pathfinder hop.
type traceHopJSON struct {
	Key   string `json:"key,omitempty"`
	Port  uint64 `json:"port"`
	Index int    `json:"index"`
}

// traceResponseJSON is the /traceroute.json response.
type traceResponseJSON struct {
	Target   string           `json:"target"`
	Path     []*traceNodeJSON `json:"path,omitempty"`
	Hops     []traceHopJSON   `json:"hops,omitempty"`
	Duration float64          `json:"duration_ms"`
	Error    string           `json:"error,omitempty"`
}

// //

func nodeToJSON(n *traceroute.NodeObj) *traceNodeJSON {
	if n == nil {
		return nil
	}
	j := &traceNodeJSON{
		Key:         hex.EncodeToString(n.Key),
		Parent:      hex.EncodeToString(n.Parent),
		Depth:       n.Depth,
		Sequence:    n.Sequence,
		RTT:         float64(n.RTT.Microseconds()) / 1000.0,
		Unreachable: n.Unreachable,
	}
	if len(n.Children) > 0 {
		j.Children = make([]*traceNodeJSON, len(n.Children))
		for i, ch := range n.Children {
			j.Children[i] = nodeToJSON(ch)
		}
	}
	return j
}

// nodeToJSONFlat converts a NodeObj to JSON without children or unreachable flag.
// Used for path serialization where only the linear chain matters.
func nodeToJSONFlat(n *traceroute.NodeObj) *traceNodeJSON {
	return &traceNodeJSON{
		Key:      hex.EncodeToString(n.Key),
		Parent:   hex.EncodeToString(n.Parent),
		Depth:    n.Depth,
		Sequence: n.Sequence,
		RTT:      float64(n.RTT.Microseconds()) / 1000.0,
	}
}

// //

// newTraceHandler traces a route to the given key.
// GET ?key=<hex>. Returns path (spanning tree) with RTT, hops (pathfinder), subtree.
func newTraceHandler(tr *traceroute.Obj) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyHex := r.URL.Query().Get("key")
		if keyHex == "" {
			writeTraceError(w, "missing 'key' query parameter")
			return
		}

		keyBytes, err := hex.DecodeString(keyHex)
		if err != nil || len(keyBytes) != ed25519.PublicKeySize {
			writeTraceError(w, "invalid public key: must be 64-char hex string (32 bytes)")
			return
		}
		pubKey := ed25519.PublicKey(keyBytes)

		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()

		result, err := tr.Trace(ctx, pubKey)
		elapsed := time.Since(start)

		resp := traceResponseJSON{
			Target:   keyHex,
			Duration: float64(elapsed.Microseconds()) / 1000.0,
		}

		if err != nil {
			resp.Error = err.Error()
		}

		if result != nil {
			if result.TreePath != nil {
				resp.Path = make([]*traceNodeJSON, len(result.TreePath))
				for i, n := range result.TreePath {
					resp.Path[i] = nodeToJSONFlat(n)
				}
			}

			if result.Hops != nil {
				resp.Hops = make([]traceHopJSON, len(result.Hops))
				for i, h := range result.Hops {
					hop := traceHopJSON{Port: h.Port, Index: h.Index}
					if len(h.Key) > 0 {
						hop.Key = hex.EncodeToString(h.Key)
					}
					resp.Hops[i] = hop
				}
			}
		}

		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}

// //

// newTreeHandler returns the BFS peer topology tree.
// GET ?depth=N&concurrency=N. depth is required and must be > 0.
// 30s timeout — BFS can take a while on large networks.
func newTreeHandler(tr *traceroute.Obj) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var depth int
		var concurrency int
		fmt.Sscanf(r.URL.Query().Get("depth"), "%d", &depth)
		fmt.Sscanf(r.URL.Query().Get("concurrency"), "%d", &concurrency)

		if depth <= 0 || depth > 65535 {
			data, _ := json.Marshal(map[string]string{"error": "depth must be between 1 and 65535"})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(data)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		result, err := tr.Tree(ctx, uint16(depth), concurrency)
		if err != nil || result == nil {
			msg := "tree unavailable"
			if err != nil {
				msg = err.Error()
			}
			data, _ := json.Marshal(map[string]string{"error": msg})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(data)
			return
		}

		data, _ := json.Marshal(nodeToJSON(result.Root))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}

// //

func writeTraceError(w http.ResponseWriter, msg string) {
	resp := traceResponseJSON{Error: msg}
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(data)
}
