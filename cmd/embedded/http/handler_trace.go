package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/voluminor/ratatoskr/mod/probe"
)

// // // // // // // // // //

type traceNodeJSONObj struct {
	Key         string              `json:"key"`
	Parent      string              `json:"parent,omitempty"`
	Depth       int                 `json:"depth"`
	Sequence    uint64              `json:"sequence,omitempty"`
	RTT         float64             `json:"rtt_ms,omitempty"`
	Unreachable bool                `json:"unreachable,omitempty"`
	Children    []*traceNodeJSONObj `json:"children,omitempty"`
}

type traceHopJSONObj struct {
	Key   string `json:"key,omitempty"`
	Port  uint64 `json:"port"`
	Index int    `json:"index"`
}

type traceResponseJSONObj struct {
	Target   string              `json:"target"`
	Path     []*traceNodeJSONObj `json:"path,omitempty"`
	Hops     []traceHopJSONObj   `json:"hops,omitempty"`
	Duration float64             `json:"duration_ms"`
	Error    string              `json:"error,omitempty"`
}

// //

func nodeToJSON(n *probe.NodeObj) *traceNodeJSONObj {
	if n == nil {
		return nil
	}
	j := &traceNodeJSONObj{
		Key:         hex.EncodeToString(n.Key),
		Parent:      hex.EncodeToString(n.Parent),
		Depth:       n.Depth,
		Sequence:    n.Sequence,
		RTT:         float64(n.RTT.Microseconds()) / 1000.0,
		Unreachable: n.Unreachable,
	}
	if len(n.Children) > 0 {
		j.Children = make([]*traceNodeJSONObj, len(n.Children))
		for i, ch := range n.Children {
			j.Children[i] = nodeToJSON(ch)
		}
	}
	return j
}

func nodeToJSONFlat(n *probe.NodeObj) *traceNodeJSONObj {
	return &traceNodeJSONObj{
		Key:      hex.EncodeToString(n.Key),
		Parent:   hex.EncodeToString(n.Parent),
		Depth:    n.Depth,
		Sequence: n.Sequence,
		RTT:      float64(n.RTT.Microseconds()) / 1000.0,
	}
}

// //

func newTraceHandler(tr *probe.Obj) http.Handler {
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

		resp := traceResponseJSONObj{
			Target:   keyHex,
			Duration: float64(elapsed.Microseconds()) / 1000.0,
		}

		if err != nil {
			resp.Error = err.Error()
		}

		if result != nil {
			if result.TreePath != nil {
				resp.Path = make([]*traceNodeJSONObj, len(result.TreePath))
				for i, n := range result.TreePath {
					resp.Path[i] = nodeToJSONFlat(n)
				}
			}

			if result.Hops != nil {
				resp.Hops = make([]traceHopJSONObj, len(result.Hops))
				for i, h := range result.Hops {
					hop := traceHopJSONObj{Port: h.Port, Index: h.Index}
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

func newTreeHandler(tr *probe.Obj) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
		concurrency, _ := strconv.Atoi(r.URL.Query().Get("concurrency"))

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
	resp := traceResponseJSONObj{Error: msg}
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(data)
}
