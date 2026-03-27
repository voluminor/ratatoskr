package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/traceroute"
)

// // // // // // // // // //

type peerJSON struct {
	URI           string     `json:"uri"`
	Up            bool       `json:"up"`
	RxBytes       uint64     `json:"rx_bytes"`
	TxBytes       uint64     `json:"tx_bytes"`
	LatencyMs     float64    `json:"latency_ms"`
	LastError     string     `json:"last_error,omitempty"`
	LastErrorTime *time.Time `json:"last_error_time,omitempty"`
}

type bandwidthJSON struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type sessionJSON struct {
	Key       string  `json:"key"`
	RxBytes   uint64  `json:"rx_bytes"`
	TxBytes   uint64  `json:"tx_bytes"`
	UptimeSec float64 `json:"uptime_sec"`
}

type infoJSON struct {
	PublicKey     string        `json:"public_key"`
	YggAddress    string        `json:"ygg_address"`
	Addresses     []string      `json:"addresses"`
	YggPorts      []int         `json:"ygg_ports"`
	IsYggdrasil   bool          `json:"is_yggdrasil"`
	UptimeSeconds float64       `json:"uptime_seconds"`
	Bandwidth     bandwidthJSON `json:"bandwidth"`
	Peers         []peerJSON    `json:"peers"`
	Sessions      []sessionJSON `json:"sessions"`
	CachedAt      time.Time     `json:"cached_at"`
}

// //

type cachedMetricsObj struct {
	snap ratatoskr.SnapshotObj
	bw   bandwidthJSON
	at   time.Time
}

type InfoHandlerObj struct {
	node      *ratatoskr.Obj
	tr        *traceroute.Obj
	cfg       *ConfigObj
	log       yggcore.Logger
	startTime time.Time
	mu        sync.Mutex
	cached    *cachedMetricsObj
}

// //

func newInfoHandler(node *ratatoskr.Obj, tr *traceroute.Obj, cfg *ConfigObj, log yggcore.Logger) *InfoHandlerObj {
	return &InfoHandlerObj{node: node, tr: tr, cfg: cfg, log: log, startTime: time.Now()}
}

func (h *InfoHandlerObj) refreshMetrics() *cachedMetricsObj {
	snap := h.node.Snapshot()

	var bw bandwidthJSON
	for _, p := range snap.Peers {
		bw.RxBytes += p.RXBytes
		bw.TxBytes += p.TXBytes
		if !p.Up && p.LastError != "" {
			h.log.Warnf("peer down: %s — %s", p.URI, p.LastError)
		}
	}

	return &cachedMetricsObj{snap: snap, bw: bw, at: time.Now()}
}

func (h *InfoHandlerObj) getMetrics() *cachedMetricsObj {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached == nil || time.Since(h.cached.at) >= 10*time.Second {
		h.cached = h.refreshMetrics()
	}
	return h.cached
}

func (h *InfoHandlerObj) buildAddresses() []string {
	if h.cfg.Hostname == "localhost" {
		addrs := make([]string, len(h.cfg.HTTPPorts))
		for i, p := range h.cfg.HTTPPorts {
			addrs[i] = fmt.Sprintf("localhost:%d", p)
		}
		return addrs
	}
	return []string{h.cfg.Hostname}
}

func (h *InfoHandlerObj) Handler(isYggdrasil bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := h.getMetrics()

		peers := make([]peerJSON, len(m.snap.Peers))
		for i, p := range m.snap.Peers {
			entry := peerJSON{
				URI:       p.URI,
				Up:        p.Up,
				RxBytes:   p.RXBytes,
				TxBytes:   p.TXBytes,
				LatencyMs: float64(p.Latency.Microseconds()) / 1000.0,
				LastError: p.LastError,
			}
			if !p.LastErrorTime.IsZero() {
				t := p.LastErrorTime
				entry.LastErrorTime = &t
			}
			peers[i] = entry
		}

		// сессии из traceroute модуля (кешируются вместе с остальным)
		rawSessions := h.tr.Sessions()
		sessions := make([]sessionJSON, len(rawSessions))
		for i, s := range rawSessions {
			sessions[i] = sessionJSON{
				Key:       hex.EncodeToString(s.Key),
				RxBytes:   s.RXBytes,
				TxBytes:   s.TXBytes,
				UptimeSec: s.Uptime.Seconds(),
			}
		}

		resp := infoJSON{
			PublicKey:     m.snap.PublicKey,
			YggAddress:    m.snap.Address,
			Addresses:     h.buildAddresses(),
			YggPorts:      h.cfg.YggPorts,
			IsYggdrasil:   isYggdrasil,
			UptimeSeconds: time.Since(h.startTime).Seconds(),
			Bandwidth:     m.bw,
			Peers:         peers,
			Sessions:      sessions,
			CachedAt:      m.at,
		}
		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=10")
		w.Write(data)
	})
}
