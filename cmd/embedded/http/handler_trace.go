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

// traceNodeJSON — JSON-представление узла spanning tree (рекурсивное)
type traceNodeJSON struct {
	Key      string           `json:"key"`
	Parent   string           `json:"parent"`
	Depth    int              `json:"depth"`
	Sequence uint64           `json:"sequence"`
	Children []*traceNodeJSON `json:"children,omitempty"`
}

// traceHopJSON — JSON-представление одного хопа из pathfinder
type traceHopJSON struct {
	Key   string `json:"key,omitempty"`
	Port  uint64 `json:"port"`
	Depth int    `json:"depth"`
}

// traceResponseJSON — ответ эндпоинта /traceroute.json
type traceResponseJSON struct {
	Target   string           `json:"target"`
	Path     []*traceNodeJSON `json:"path,omitempty"`
	Hops     []traceHopJSON   `json:"hops,omitempty"`
	Subtree  *traceNodeJSON   `json:"subtree,omitempty"`
	Duration float64          `json:"duration_ms"`
	Error    string           `json:"error,omitempty"`
}

// //

func nodeToJSON(n *traceroute.NodeObj) *traceNodeJSON {
	if n == nil {
		return nil
	}
	j := &traceNodeJSON{
		Key:      hex.EncodeToString(n.Key),
		Parent:   hex.EncodeToString(n.Parent),
		Depth:    n.Depth,
		Sequence: n.Sequence,
	}
	if len(n.Children) > 0 {
		j.Children = make([]*traceNodeJSON, len(n.Children))
		for i, ch := range n.Children {
			j.Children[i] = nodeToJSON(ch)
		}
	}
	return j
}

// //

// newTraceHandler — создаёт HTTP-хендлер для трассировки.
// Принимает GET-параметр "key" (hex-публичный ключ целевой ноды).
// Возвращает JSON с данными из двух источников:
// - path: цепочка по spanning tree (если ключ найден в дереве)
// - hops: маршрут из pathfinder (port→key, после lookup)
// - subtree: поддерево целевой ноды (если есть children)
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

		// Trace: пробует tree + paths, если не найдено — SendLookup + poll до 6 секунд
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()

		result, err := tr.Trace(ctx, pubKey)
		elapsed := time.Since(start)

		if err != nil {
			writeTraceError(w, err.Error())
			return
		}

		resp := traceResponseJSON{
			Target:   keyHex,
			Duration: float64(elapsed.Microseconds()) / 1000.0,
		}

		// spanning tree path
		if result.TreePath != nil {
			resp.Path = make([]*traceNodeJSON, len(result.TreePath))
			for i, n := range result.TreePath {
				resp.Path[i] = &traceNodeJSON{
					Key:      hex.EncodeToString(n.Key),
					Parent:   hex.EncodeToString(n.Parent),
					Depth:    n.Depth,
					Sequence: n.Sequence,
				}
			}
			// поддерево последнего узла (целевого)
			last := result.TreePath[len(result.TreePath)-1]
			if len(last.Children) > 0 {
				resp.Subtree = nodeToJSON(last)
			}
		}

		// pathfinder hops
		if result.Hops != nil {
			resp.Hops = make([]traceHopJSON, len(result.Hops))
			for i, h := range result.Hops {
				hop := traceHopJSON{Port: h.Port, Depth: h.Depth}
				if len(h.Key) > 0 {
					hop.Key = hex.EncodeToString(h.Key)
				}
				resp.Hops[i] = hop
			}
		}

		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}

// //

// newTreeHandler — отдаёт spanning tree целиком как JSON.
// GET-параметр "depth" ограничивает глубину (0 = без ограничения, по умолчанию 0).
func newTreeHandler(tr *traceroute.Obj) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		depth := 0
		if d := r.URL.Query().Get("depth"); d != "" {
			fmt.Sscanf(d, "%d", &depth)
		}
		root := tr.Tree(depth)
		if root == nil {
			data, _ := json.Marshal(map[string]string{"error": "tree is empty"})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(data)
			return
		}
		data, _ := json.Marshal(nodeToJSON(root))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=10")
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
