package main

import (
	"context"
	"net/http"
	"time"

	"github.com/voluminor/ratatoskr/mod/traceroute"
	"golang.org/x/net/websocket"
)

// // // // // // // // // //

// treeWSReqJSON is a client request to start a new tree scan.
type treeWSReqJSON struct {
	Depth int `json:"depth"`
}

// treeWSMsgJSON is a server message: ack, progress, result, or error.
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

// newTreeWSHandler creates a persistent WebSocket handler for tree scans.
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

// treeWSLoop processes tree scan requests from the WebSocket connection.
// A dedicated reader goroutine detects disconnect immediately and cancels ctx,
// so in-flight BFS scans abort without waiting for the next progress Send.
func treeWSLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, tr *traceroute.Obj) {
	requests := make(chan treeWSReqJSON, 1)
	go func() {
		defer cancel()
		for {
			var req treeWSReqJSON
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				return
			}
			select {
			case requests <- req:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case req := <-requests:
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

			if disconnected := streamTreeProgress(ws, ctx, ch, done); disconnected {
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
}

// //

// streamTreeProgress reads progress from ch until Done=true, ctx cancellation, or goroutine exit.
// Returns true if the connection was lost (ctx cancelled by reader goroutine).
func streamTreeProgress(ws *websocket.Conn, ctx context.Context, ch <-chan traceroute.TreeProgressObj, done <-chan struct{}) bool {
	for {
		select {
		case <-ctx.Done():
			<-done
			return true
		case p := <-ch:
			if p.Found > 0 {
				if err := websocket.JSON.Send(ws, treeWSMsgJSON{
					Type:  "progress",
					Depth: p.Depth,
					Found: p.Found,
					Total: p.Total,
				}); err != nil {
					<-done
					return true
				}
			}
			if p.Done {
				<-done
				return false
			}
		case <-done:
			for len(ch) > 0 {
				<-ch
			}
			return false
		}
	}
}
