package main

import (
	"context"
	"net/http"
	"time"

	"github.com/voluminor/ratatoskr/mod/probe"
	"golang.org/x/net/websocket"
)

// // // // // // // // // //

type treeWSReqJSONObj struct {
	Depth int `json:"depth"`
}

type treeWSMsgJSONObj struct {
	Type     string            `json:"type"`
	Depth    int               `json:"depth,omitempty"`
	Found    int               `json:"found,omitempty"`
	Total    int               `json:"total,omitempty"`
	Root     *traceNodeJSONObj `json:"root,omitempty"`
	Duration float64           `json:"duration_ms,omitempty"`
	Message  string            `json:"message,omitempty"`
}

// // // // // // // // // //

func newTreeWSHandler(tr *probe.Obj) http.Handler {
	return websocket.Server{
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

func treeWSLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, tr *probe.Obj) {
	requests := make(chan treeWSReqJSONObj, 1)
	go func() {
		defer cancel()
		for {
			var req treeWSReqJSONObj
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
				_ = websocket.JSON.Send(ws, treeWSMsgJSONObj{Type: "error", Message: "depth must be between 1 and 65535"})
				continue
			}

			if err := websocket.JSON.Send(ws, treeWSMsgJSONObj{Type: "ack"}); err != nil {
				return
			}

			ch := make(chan probe.TreeProgressObj, 16)
			start := time.Now()
			done := make(chan struct{})

			var result *probe.TreeResultObj
			var scanErr error
			go func() {
				defer close(done)
				result, scanErr = tr.TreeChan(ctx, uint16(req.Depth), 0, ch)
			}()

			if disconnected := streamTreeProgress(ws, ctx, ch, done); disconnected {
				return
			}

			if scanErr != nil {
				if err := websocket.JSON.Send(ws, treeWSMsgJSONObj{Type: "error", Message: scanErr.Error()}); err != nil {
					return
				}
				continue
			}

			elapsed := time.Since(start)
			if err := websocket.JSON.Send(ws, treeWSMsgJSONObj{
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

func streamTreeProgress(ws *websocket.Conn, ctx context.Context, ch <-chan probe.TreeProgressObj, done <-chan struct{}) bool {
	for {
		select {
		case <-ctx.Done():
			<-done
			return true
		case p := <-ch:
			if p.Found > 0 {
				if err := websocket.JSON.Send(ws, treeWSMsgJSONObj{
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
