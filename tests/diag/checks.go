package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// // // // // // // // // //

const (
	defaultCheckTimeout = 5 * time.Second
	defaultLoadSeconds  = 10
	defaultLoadStreams  = 1
	defaultPayloadSize  = 256

	maxLoadStreams      = 64
	maxLoadSeconds      = 60
	maxLoadPayloadSize  = 1 << 20
	maxCheckPayloadSize = 1 << 20
	maxTimeoutMS        = 60_000
)

type checkRequestObj struct {
	Address   string `json:"address"`
	Payload   string `json:"payload"`
	Size      int    `json:"size"`
	TimeoutMS int    `json:"timeout_ms"`
}

type checkResponseObj struct {
	OK         bool    `json:"ok"`
	Network    string  `json:"network"`
	Address    string  `json:"address"`
	SentBytes  int     `json:"sent_bytes"`
	ReadBytes  int     `json:"read_bytes"`
	Matched    bool    `json:"matched"`
	DurationMS float64 `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
}

type loadRequestObj struct {
	Address   string `json:"address"`
	Size      int    `json:"size"`
	Seconds   int    `json:"seconds"`
	Streams   int    `json:"streams"`
	TimeoutMS int    `json:"timeout_ms"`
}

type loadResponseObj struct {
	OK             bool    `json:"ok"`
	Network        string  `json:"network"`
	Address        string  `json:"address"`
	Streams        int     `json:"streams"`
	Seconds        int     `json:"seconds"`
	PayloadBytes   int     `json:"payload_bytes"`
	Operations     int64   `json:"operations"`
	Errors         int64   `json:"errors"`
	Bytes          int64   `json:"bytes"`
	OperationsPerS float64 `json:"operations_per_s"`
	MebibytesPerS  float64 `json:"mebibytes_per_s"`
	DurationMS     float64 `json:"duration_ms"`
	LastError      string  `json:"last_error,omitempty"`
	Error          string  `json:"error,omitempty"`
}

func makePayload(req checkRequestObj) []byte {
	if req.Payload != "" {
		return []byte(req.Payload)
	}
	size := req.Size
	if size <= 0 {
		size = defaultPayloadSize
	}
	size = clampInt(size, maxCheckPayloadSize)
	return pattern(size)
}

func pattern(size int) []byte {
	if size < 0 {
		size = 0
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}

func timeoutFromMS(ms int) time.Duration {
	if ms <= 0 {
		return defaultCheckTimeout
	}
	ms = clampInt(ms, maxTimeoutMS)
	return time.Duration(ms) * time.Millisecond
}

func normalizeLoad(req *loadRequestObj) {
	if req.Size <= 0 {
		req.Size = defaultPayloadSize
	}
	if req.Seconds <= 0 {
		req.Seconds = defaultLoadSeconds
	}
	if req.Streams <= 0 {
		req.Streams = defaultLoadStreams
	}
	req.Size = clampInt(req.Size, maxLoadPayloadSize)
	req.Seconds = clampInt(req.Seconds, maxLoadSeconds)
	req.Streams = clampInt(req.Streams, maxLoadStreams)
}

func (s *serverObj) handleTCPCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, checkResponseObj{Network: "tcp", Error: err.Error()})
		return
	}
	resp := s.runTCPCheck(r.Context(), req)
	code := http.StatusOK
	if !resp.OK {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, resp)
}

func (s *serverObj) handleUDPCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, checkResponseObj{Network: "udp", Error: err.Error()})
		return
	}
	resp := s.runUDPCheck(r.Context(), req)
	code := http.StatusOK
	if !resp.OK {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, resp)
}

func (s *serverObj) runTCPCheck(parent context.Context, req checkRequestObj) checkResponseObj {
	payload := makePayload(req)
	ctx, cancel := context.WithTimeout(parent, timeoutFromMS(req.TimeoutMS))
	defer cancel()
	start := time.Now()
	conn, err := s.node.DialContext(ctx, "tcp", req.Address)
	if err != nil {
		return checkResponseObj{Network: "tcp", Address: req.Address, SentBytes: len(payload), Error: err.Error(), DurationMS: msSince(start)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeoutFromMS(req.TimeoutMS)))
	if _, err = conn.Write(payload); err != nil {
		return checkResponseObj{Network: "tcp", Address: req.Address, SentBytes: len(payload), Error: err.Error(), DurationMS: msSince(start)}
	}
	got := make([]byte, len(payload))
	n, err := io.ReadFull(conn, got)
	matched := err == nil && bytes.Equal(payload, got)
	resp := checkResponseObj{OK: matched, Network: "tcp", Address: req.Address, SentBytes: len(payload), ReadBytes: n, Matched: matched, DurationMS: msSince(start)}
	if err != nil {
		resp.Error = err.Error()
	}
	return resp
}

func (s *serverObj) runUDPCheck(parent context.Context, req checkRequestObj) checkResponseObj {
	payload := makePayload(req)
	ctx, cancel := context.WithTimeout(parent, timeoutFromMS(req.TimeoutMS))
	defer cancel()
	start := time.Now()
	conn, err := s.node.DialContext(ctx, "udp", req.Address)
	if err != nil {
		return checkResponseObj{Network: "udp", Address: req.Address, SentBytes: len(payload), Error: err.Error(), DurationMS: msSince(start)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeoutFromMS(req.TimeoutMS)))
	if _, err = conn.Write(payload); err != nil {
		return checkResponseObj{Network: "udp", Address: req.Address, SentBytes: len(payload), Error: err.Error(), DurationMS: msSince(start)}
	}
	got := make([]byte, maxInt(udpEchoBufferSize, len(payload)))
	n, err := conn.Read(got)
	matched := err == nil && n == len(payload) && bytes.Equal(payload, got[:n])
	resp := checkResponseObj{OK: matched, Network: "udp", Address: req.Address, SentBytes: len(payload), ReadBytes: n, Matched: matched, DurationMS: msSince(start)}
	if err != nil {
		resp.Error = err.Error()
	}
	return resp
}

func (s *serverObj) handleTCPLoad(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	var req loadRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, loadResponseObj{Network: "tcp", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.runLoad(r.Context(), "tcp", req))
}

func (s *serverObj) handleUDPLoad(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	var req loadRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, loadResponseObj{Network: "udp", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.runLoad(r.Context(), "udp", req))
}

func (s *serverObj) runLoad(parent context.Context, network string, req loadRequestObj) loadResponseObj {
	normalizeLoad(&req)
	payload := pattern(req.Size)
	ctx, cancel := context.WithTimeout(parent, time.Duration(req.Seconds)*time.Second)
	defer cancel()
	var ops atomic.Int64
	var errs atomic.Int64
	var bytesDone atomic.Int64
	var lastErr atomic.Value
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < req.Streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runLoadStream(ctx, network, req.Address, payload, timeoutFromMS(req.TimeoutMS), &ops, &errs, &bytesDone, &lastErr)
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	resp := loadResponseObj{
		OK:             errs.Load() == 0 && ops.Load() > 0,
		Network:        network,
		Address:        req.Address,
		Streams:        req.Streams,
		Seconds:        req.Seconds,
		PayloadBytes:   len(payload),
		Operations:     ops.Load(),
		Errors:         errs.Load(),
		Bytes:          bytesDone.Load(),
		DurationMS:     float64(elapsed.Microseconds()) / 1000.0,
		OperationsPerS: float64(ops.Load()) / elapsed.Seconds(),
		MebibytesPerS:  float64(bytesDone.Load()) / elapsed.Seconds() / 1024.0 / 1024.0,
	}
	if v := lastErr.Load(); v != nil {
		resp.LastError = v.(string)
	}
	return resp
}

func (s *serverObj) runLoadStream(ctx context.Context, network string, address string, payload []byte, timeout time.Duration, ops *atomic.Int64, errs *atomic.Int64, bytesDone *atomic.Int64, lastErr *atomic.Value) {
	conn, err := s.node.DialContext(ctx, network, address)
	if err != nil {
		errs.Add(1)
		lastErr.Store(err.Error())
		return
	}
	defer func() { _ = conn.Close() }()
	buf := make([]byte, maxInt(len(payload), udpEchoBufferSize))
	for ctx.Err() == nil {
		_ = conn.SetDeadline(time.Now().Add(timeout))
		if _, err = conn.Write(payload); err != nil {
			errs.Add(1)
			lastErr.Store(err.Error())
			return
		}
		n, err := readEcho(conn, network, buf, len(payload))
		if err != nil {
			errs.Add(1)
			lastErr.Store(err.Error())
			return
		}
		if n != len(payload) || !bytes.Equal(payload, buf[:n]) {
			errs.Add(1)
			lastErr.Store(fmt.Sprintf("echo mismatch: got %d bytes, expected %d", n, len(payload)))
			return
		}
		ops.Add(1)
		bytesDone.Add(int64(n))
	}
}

func readEcho(conn net.Conn, network string, buf []byte, size int) (int, error) {
	if network == "tcp" {
		return io.ReadFull(conn, buf[:size])
	}
	return conn.Read(buf)
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Microseconds()) / 1000.0
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v int, max int) int {
	if v > max {
		return max
	}
	return v
}
