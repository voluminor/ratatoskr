package main

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/voluminor/ratatoskr"
)

// // // // // // // // // //

// maxBodyBytes caps decoded request bodies so an unbounded payload cannot
// exhaust memory even if this tool is bound to a reachable interface.
const maxBodyBytes = 64 * 1024

// healthObj is the small readiness response used by scripts and humans.
type healthObj struct {
	Status            string `json:"status"`
	Name              string `json:"name"`
	Address           string `json:"address"`
	PublicKey         string `json:"public_key"`
	MTU               uint64 `json:"mtu"`
	TCPEchoAddr       string `json:"tcp_echo_addr"`
	UDPEchoAddr       string `json:"udp_echo_addr"`
	TCPThroughputAddr string `json:"tcp_throughput_addr"`
	UDPThroughputAddr string `json:"udp_throughput_addr"`
	Uptime            string `json:"uptime"`
}

// statusObj is a generic small response for imperative endpoints.
type statusObj struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	defer func() { _ = r.Body.Close() }()
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func (s *serverObj) handleHealth(w http.ResponseWriter, _ *http.Request) {
	pk := s.node.PublicKey()
	resp := healthObj{
		Status:            "ok",
		Name:              s.cfg.Name,
		Address:           s.node.Address().String(),
		PublicKey:         hex.EncodeToString(pk),
		MTU:               s.node.MTU(),
		TCPEchoAddr:       s.yggAddress(s.cfg.TCPEchoPort),
		UDPEchoAddr:       s.yggAddress(s.cfg.UDPEchoPort),
		TCPThroughputAddr: s.yggAddress(s.cfg.TCPThroughputPort),
		UDPThroughputAddr: s.yggAddress(s.cfg.UDPThroughputPort),
		Uptime:            time.Since(s.startedAt).String(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *serverObj) handleSnapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.node.Snapshot())
}

func (s *serverObj) handleSOCKSEnable(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	err := s.node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
		Addr:           s.cfg.SOCKSListen,
		MaxConnections: s.cfg.SOCKSMaxConns,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, statusObj{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, statusObj{OK: true})
}

func (s *serverObj) handleSOCKSDisable(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	if err := s.node.DisableSOCKS(); err != nil {
		writeJSON(w, http.StatusInternalServerError, statusObj{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, statusObj{OK: true})
}

func (s *serverObj) handleClose(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, statusObj{OK: true})
	go s.shutdown()
}
