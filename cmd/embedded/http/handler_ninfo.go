package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	htmlsigils "github.com/voluminor/ratatoskr/mod/html/sigils"
	"github.com/voluminor/ratatoskr/mod/ninfo"
)

// // // // // // // // // //

type ninfoResponseJSON struct {
	Target     string             `json:"target"`
	RTT        float64            `json:"rtt_ms"`
	Version    string             `json:"version,omitempty"`
	Software   *ninfo.SoftwareObj `json:"software,omitempty"`
	SigilsHTML map[string]string  `json:"sigils_html,omitempty"`
	ExtraHTML  string             `json:"extra_html,omitempty"`
	CSS        string             `json:"css"`
	Error      string             `json:"error,omitempty"`
}

// //

func newNinfoHandler(ni *ninfo.Obj) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyHex := r.URL.Query().Get("key")
		if keyHex == "" {
			writeNinfoError(w, "missing 'key' query parameter")
			return
		}

		keyBytes, err := hex.DecodeString(keyHex)
		if err != nil || len(keyBytes) != ed25519.PublicKeySize {
			writeNinfoError(w, "invalid public key: must be 64-char hex string (32 bytes)")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		result, err := ni.Ask(ctx, ed25519.PublicKey(keyBytes))
		if err != nil {
			writeNinfoError(w, err.Error())
			return
		}

		resp := ninfoResponseJSON{
			Target: keyHex,
			RTT:    float64(result.RTT.Microseconds()) / 1000.0,
			CSS:    string(htmlsigils.CSS),
		}

		if result.Software != nil {
			resp.Software = result.Software
		}

		if result.Node != nil {
			resp.Version = result.Node.Version

			rendered, err := htmlsigils.Render(result.Node)
			if err == nil {
				if len(rendered.Sigils) > 0 {
					resp.SigilsHTML = make(map[string]string, len(rendered.Sigils))
					for name, buf := range rendered.Sigils {
						if buf != nil {
							resp.SigilsHTML[name] = string(buf)
						}
					}
				}
				if rendered.Extra != nil {
					resp.ExtraHTML = string(rendered.Extra)
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

func writeNinfoError(w http.ResponseWriter, msg string) {
	resp := ninfoResponseJSON{Error: msg, CSS: string(htmlsigils.CSS)}
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(data)
}
