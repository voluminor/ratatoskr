package main

import (
	"context"
	"net/http"
	"time"

	"github.com/voluminor/ratatoskr/mod/traceroute"
	"golang.org/x/net/websocket"
)

// // // // // // // // // //

// treeWSReqJSON — client request: start a new tree scan on the existing connection.
type treeWSReqJSON struct {
	Depth int `json:"depth"`
}

// treeWSMsgJSON — server message: ack, progress, result, or error.
type treeWSMsgJSON struct {
	Type     string         `json:"type"`
	Depth    int            `json:"depth,omitempty"`
	Found    int            `json:"found,omitempty"`
	Total    int            `json:"total,omitempty"`
	Root     *traceNodeJSON `json:"root,omitempty"`
	Duration float64        `json:"duration_ms,omitempty"`
	Message  string         `json:"message,omitempty"`
}

// // // // // // // // // //

// newTreeWSHandler — persistent WebSocket handler for tree scans.
// One connection per modal session; closed when the modal closes.
// Each Refresh sends a new scan request over the same connection.
func newTreeWSHandler(tr *traceroute.Obj) http.Handler {
	return websocket.Server{
		// Accept connections from any origin — embedded server serves its own UI.
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			cfg.Origin = r.URL
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			treeWSLoop(ctx, cancel, ws, tr)
		},
	}
}

// //

// treeWSLoop — main read loop for a WebSocket connection.
// Blocks on Receive; runs one BFS scan per request; repeats until disconnect.
func treeWSLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, tr *traceroute.Obj) {
	for {
		var req treeWSReqJSON
		if err := websocket.JSON.Receive(ws, &req); err != nil {
			return // connection closed
		}

		if req.Depth <= 0 || req.Depth > 65535 {
			_ = websocket.JSON.Send(ws, treeWSMsgJSON{Type: "error", Message: "depth must be between 1 and 65535"})
			continue
		}

		if err := websocket.JSON.Send(ws, treeWSMsgJSON{Type: "ack"}); err != nil {
			return
		}

		ch := make(chan traceroute.TreeProgressObj, 16)
		start := time.Now()
		done := make(chan struct{})

		var result *traceroute.TreeResultObj
		var scanErr error
		go func() {
			defer close(done)
			result, scanErr = tr.TreeChan(ctx, uint16(req.Depth), 0, ch)
		}()

		if disconnected := streamTreeProgress(ws, ch, done, cancel); disconnected {
			return
		}

		if scanErr != nil {
			if err := websocket.JSON.Send(ws, treeWSMsgJSON{Type: "error", Message: scanErr.Error()}); err != nil {
				return
			}
			continue
		}

		elapsed := time.Since(start)
		if err := websocket.JSON.Send(ws, treeWSMsgJSON{
			Type:     "result",
			Root:     nodeToJSON(result.Root),
			Total:    result.Total,
			Duration: float64(elapsed.Microseconds()) / 1000.0,
		}); err != nil {
			return
		}
	}
}

// //

// streamTreeProgress reads progress from ch until Done=true or the goroutine exits.
// Returns true if the WebSocket connection dropped during streaming.
func streamTreeProgress(ws *websocket.Conn, ch <-chan traceroute.TreeProgressObj, done <-chan struct{}, cancel context.CancelFunc) bool {
	for {
		select {
		case p := <-ch:
			if p.Found > 0 {
				if err := websocket.JSON.Send(ws, treeWSMsgJSON{
					Type:  "progress",
					Depth: p.Depth,
					Found: p.Found,
					Total: p.Total,
				}); err != nil {
					cancel()
					<-done
					return true
				}
			}
			if p.Done {
				<-done
				return false
			}
		case <-done:
			// Goroutine exited early (context cancelled or error before Done).
			for len(ch) > 0 {
				<-ch
			}
			return false
		}
	}
}
