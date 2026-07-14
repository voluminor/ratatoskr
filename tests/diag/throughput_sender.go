package main

import (
	"context"
	"errors"
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
	defaultThroughputSeconds    = 5
	defaultThroughputStreams    = 1
	defaultThroughputTCPPayload = 256 * 1024
	defaultThroughputUDPPayload = 1200
	maxThroughputSeconds        = 30
	maxThroughputStreams        = 32
	maxThroughputTCPPayload     = 1024 * 1024
	maxThroughputUDPPayload     = udpEchoBufferSize - throughputUDPHeaderSize
)

type throughputControlRequestObj struct {
	ID        string `json:"id"`
	Transport string `json:"transport"`
	Network   string `json:"network"`
}

type throughputSenderRequestObj struct {
	ID           string `json:"id"`
	Transport    string `json:"transport"`
	Network      string `json:"network"`
	Address      string `json:"address"`
	Seconds      int    `json:"seconds"`
	Streams      int    `json:"streams"`
	PayloadBytes int    `json:"payload_bytes"`
}

type throughputSenderObj struct {
	OK            bool    `json:"ok"`
	ID            string  `json:"id"`
	Transport     string  `json:"transport"`
	Network       string  `json:"network"`
	Address       string  `json:"address"`
	Streams       int     `json:"streams"`
	PayloadBytes  int     `json:"payload_bytes"`
	SentBytes     uint64  `json:"sent_bytes"`
	SentPackets   uint64  `json:"sent_packets"`
	Errors        uint64  `json:"errors"`
	DurationMS    float64 `json:"duration_ms"`
	MebibytesPerS float64 `json:"mebibytes_per_s"`
	PacketsPerS   float64 `json:"packets_per_s"`
	LastError     string  `json:"last_error,omitempty"`
	Error         string  `json:"error,omitempty"`
}

type ioErrShortWriteObj struct {
	wrote int
	want  int
}

func normalizeThroughputRequest(req *throughputSenderRequestObj) error {
	if _, err := decodeThroughputRunID(req.ID); err != nil {
		return err
	}
	if !validThroughputPath(req.Transport, req.Network) {
		return errors.New("invalid throughput transport or network")
	}
	if req.Address == "" {
		return errors.New("throughput address is required")
	}
	if req.Seconds == 0 {
		req.Seconds = defaultThroughputSeconds
	}
	if req.Streams == 0 {
		req.Streams = defaultThroughputStreams
	}
	if req.PayloadBytes == 0 {
		if req.Network == "tcp" {
			req.PayloadBytes = defaultThroughputTCPPayload
		} else {
			req.PayloadBytes = defaultThroughputUDPPayload
		}
	}
	if req.Seconds < 1 || req.Seconds > maxThroughputSeconds {
		return fmt.Errorf("throughput seconds must be between 1 and %d", maxThroughputSeconds)
	}
	if req.Streams < 1 || req.Streams > maxThroughputStreams {
		return fmt.Errorf("throughput streams must be between 1 and %d", maxThroughputStreams)
	}
	maxPayload := maxThroughputUDPPayload
	if req.Network == "tcp" {
		maxPayload = maxThroughputTCPPayload
	}
	if req.PayloadBytes < 1 || req.PayloadBytes > maxPayload {
		return fmt.Errorf("throughput payload must be between 1 and %d bytes", maxPayload)
	}
	return nil
}

func (s *serverObj) throughputDial(ctx context.Context, transport, network, address string) (net.Conn, error) {
	if transport == "ygg" {
		return s.node.DialContext(ctx, network, address)
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// //

func (s *serverObj) handleThroughputStart(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	var req throughputControlRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, statusObj{Error: err.Error()})
		return
	}
	if err := s.throughput.create(req.ID, req.Transport, req.Network); err != nil {
		writeJSON(w, http.StatusBadRequest, statusObj{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, statusObj{OK: true})
}

func (s *serverObj) handleThroughputFinish(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	var req throughputControlRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, throughputReceiverObj{Error: err.Error()})
		return
	}
	result, err := s.throughput.finish(req.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, throughputReceiverObj{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *serverObj) handleThroughputRun(w http.ResponseWriter, r *http.Request) {
	if !s.guardMutation(w, r) {
		return
	}
	var req throughputSenderRequestObj
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, throughputSenderObj{Error: err.Error()})
		return
	}
	if err := normalizeThroughputRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, throughputSenderObj{Error: err.Error()})
		return
	}
	result := s.runThroughput(r.Context(), req)
	code := http.StatusOK
	if !result.OK {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, result)
}

func (s *serverObj) runThroughput(parent context.Context, req throughputSenderRequestObj) throughputSenderObj {
	ctx, cancel := context.WithTimeout(parent, time.Duration(req.Seconds)*time.Second)
	defer cancel()
	var sentBytes atomic.Uint64
	var sentPackets atomic.Uint64
	var errorCount atomic.Uint64
	var lastError atomic.Value
	start := time.Now()
	var wg sync.WaitGroup
	for streamID := range req.Streams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runThroughputStream(ctx, req, uint32(streamID), &sentBytes, &sentPackets, &errorCount, &lastError)
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	result := throughputSenderObj{
		OK:            errorCount.Load() == 0 && sentBytes.Load() > 0,
		ID:            req.ID,
		Transport:     req.Transport,
		Network:       req.Network,
		Address:       req.Address,
		Streams:       req.Streams,
		PayloadBytes:  req.PayloadBytes,
		SentBytes:     sentBytes.Load(),
		SentPackets:   sentPackets.Load(),
		Errors:        errorCount.Load(),
		DurationMS:    float64(elapsed.Microseconds()) / 1000,
		MebibytesPerS: float64(sentBytes.Load()) / elapsed.Seconds() / 1024 / 1024,
		PacketsPerS:   float64(sentPackets.Load()) / elapsed.Seconds(),
	}
	if value := lastError.Load(); value != nil {
		result.LastError = value.(string)
	}
	return result
}

func (s *serverObj) runThroughputStream(ctx context.Context, req throughputSenderRequestObj, streamID uint32, sentBytes, sentPackets, errorCount *atomic.Uint64, lastError *atomic.Value) {
	conn, err := s.throughputDial(ctx, req.Transport, req.Network, req.Address)
	if err != nil {
		recordThroughputError(ctx, err, errorCount, lastError)
		return
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	if req.Network == "tcp" {
		s.runThroughputTCPStream(ctx, conn, req, streamID, sentBytes, sentPackets, errorCount, lastError)
		return
	}
	s.runThroughputUDPStream(ctx, conn, req, streamID, sentBytes, sentPackets, errorCount, lastError)
}

func (s *serverObj) runThroughputTCPStream(ctx context.Context, conn net.Conn, req throughputSenderRequestObj, streamID uint32, sentBytes, sentPackets, errorCount *atomic.Uint64, lastError *atomic.Value) {
	header := make([]byte, throughputTCPHeaderSize)
	if err := encodeThroughputHeader(header, req.ID, streamID, 0, false); err != nil {
		recordThroughputError(ctx, err, errorCount, lastError)
		return
	}
	if _, err := writeThroughputFull(conn, header); err != nil {
		recordThroughputError(ctx, err, errorCount, lastError)
		return
	}
	payload := pattern(req.PayloadBytes)
	for ctx.Err() == nil {
		n, err := writeThroughputFull(conn, payload)
		if n > 0 {
			sentBytes.Add(uint64(n))
		}
		if err != nil {
			recordThroughputError(ctx, err, errorCount, lastError)
			return
		}
		sentPackets.Add(1)
	}
}

func (s *serverObj) runThroughputUDPStream(ctx context.Context, conn net.Conn, req throughputSenderRequestObj, streamID uint32, sentBytes, sentPackets, errorCount *atomic.Uint64, lastError *atomic.Value) {
	packet := make([]byte, throughputUDPHeaderSize+req.PayloadBytes)
	copy(packet[throughputUDPHeaderSize:], pattern(req.PayloadBytes))
	for sequence := uint64(0); ctx.Err() == nil; sequence++ {
		if err := encodeThroughputHeader(packet, req.ID, streamID, sequence, true); err != nil {
			recordThroughputError(ctx, err, errorCount, lastError)
			return
		}
		n, err := conn.Write(packet)
		if err != nil {
			recordThroughputError(ctx, err, errorCount, lastError)
			return
		}
		if n != len(packet) {
			recordThroughputError(ctx, ioErrShortWriteObj{wrote: n, want: len(packet)}, errorCount, lastError)
			return
		}
		sentBytes.Add(uint64(req.PayloadBytes))
		sentPackets.Add(1)
	}
}

func writeThroughputFull(writer io.Writer, data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		n, err := writer.Write(data)
		written += n
		if err != nil {
			return written, err
		}
		if n <= 0 {
			return written, ioErrShortWriteObj{wrote: written, want: written + len(data)}
		}
		data = data[n:]
	}
	return written, nil
}

func recordThroughputError(ctx context.Context, err error, errorCount *atomic.Uint64, lastError *atomic.Value) {
	if ctx.Err() != nil {
		return
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return
	}
	errorCount.Add(1)
	lastError.Store(err.Error())
}

func (e ioErrShortWriteObj) Error() string {
	return fmt.Sprintf("short throughput write: wrote %d of %d bytes", e.wrote, e.want)
}
