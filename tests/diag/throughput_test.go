package main

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// // // // // // // // // //

const testThroughputRunID = "00112233445566778899aabbccddeeff"

type expiredDeadlineContextObj struct{}

func (expiredDeadlineContextObj) Deadline() (time.Time, bool) {
	return time.Now().Add(-time.Second), true
}
func (expiredDeadlineContextObj) Done() <-chan struct{} { return nil }
func (expiredDeadlineContextObj) Err() error            { return nil }
func (expiredDeadlineContextObj) Value(any) any         { return nil }

type partialErrorWriterObj struct {
	err error
}

func (w partialErrorWriterObj) Write(data []byte) (int, error) {
	return len(data) / 2, w.err
}

func TestThroughputHeaderRoundTrip(t *testing.T) {
	for _, udp := range []bool{false, true} {
		size := throughputTCPHeaderSize
		if udp {
			size = throughputUDPHeaderSize
		}
		buffer := make([]byte, size)
		if err := encodeThroughputHeader(buffer, testThroughputRunID, 17, 42, udp); err != nil {
			t.Fatalf("encode: %v", err)
		}
		header, err := decodeThroughputHeader(buffer, udp)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if header.RunID != testThroughputRunID || header.StreamID != 17 {
			t.Fatalf("header = %+v", header)
		}
		if udp && header.Sequence != 42 {
			t.Fatalf("sequence = %d, want 42", header.Sequence)
		}
	}
}

func TestThroughputHeaderRejectsInvalidInput(t *testing.T) {
	if err := encodeThroughputHeader(make([]byte, throughputTCPHeaderSize), "not-hex", 0, 0, false); !errors.Is(err, errInvalidThroughputRunID) {
		t.Fatalf("encode error = %v", err)
	}
	bad := make([]byte, throughputTCPHeaderSize)
	if _, err := decodeThroughputHeader(bad, false); err == nil {
		t.Fatal("invalid magic accepted")
	}
}

func TestUDPSequenceWindow(t *testing.T) {
	var tracker udpSequenceObj
	if !tracker.record(0) || !tracker.record(2) || !tracker.record(1) {
		t.Fatal("unique packets rejected")
	}
	if tracker.record(1) {
		t.Fatal("duplicate accepted")
	}
	if tracker.unique != 3 || tracker.reordered != 1 || tracker.duplicates != 1 {
		t.Fatalf("tracker = %+v", tracker)
	}
	if !tracker.record(throughputSequenceWindow + 10) {
		t.Fatal("window advance rejected")
	}
	if tracker.record(0) || tracker.tooOld != 1 {
		t.Fatalf("old packet accounting = %+v", tracker)
	}
}

func TestThroughputRegistryLifecycle(t *testing.T) {
	var registry throughputRegistryObj
	if err := registry.create(testThroughputRunID, "direct", "udp"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := registry.create(testThroughputRunID, "direct", "udp"); !errors.Is(err, errThroughputRunExists) {
		t.Fatalf("duplicate create = %v", err)
	}
	run := registry.lookup(testThroughputRunID, "direct", "udp")
	if run == nil {
		t.Fatal("run not found")
	}
	run.recordUDP(0, 0, 1200)
	result, err := registry.finish(testThroughputRunID)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !result.OK || result.ReceivedBytes != 1200 || result.ReceivedPackets != 1 {
		t.Fatalf("result = %+v", result)
	}
	if _, err = registry.finish(testThroughputRunID); !errors.Is(err, errThroughputRunNotFound) {
		t.Fatalf("second finish = %v", err)
	}
}

func TestNormalizeThroughputRequest(t *testing.T) {
	req := throughputSenderRequestObj{
		ID:        testThroughputRunID,
		Transport: "ygg",
		Network:   "udp",
		Address:   "[200::1]:19081",
	}
	if err := normalizeThroughputRequest(&req); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if req.Seconds != defaultThroughputSeconds || req.Streams != 1 || req.PayloadBytes != defaultThroughputUDPPayload {
		t.Fatalf("defaults = %+v", req)
	}
	req.Streams = maxThroughputStreams + 1
	if err := normalizeThroughputRequest(&req); err == nil {
		t.Fatal("excessive stream count accepted")
	}
}

func TestHandleThroughputTCPCountsPayload(t *testing.T) {
	var registry throughputRegistryObj
	if err := registry.create(testThroughputRunID, "direct", "tcp"); err != nil {
		t.Fatal(err)
	}
	server := &serverObj{throughput: &registry}
	client, sink := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleThroughputTCP(sink, "direct")
		close(done)
	}()
	header := make([]byte, throughputTCPHeaderSize)
	if err := encodeThroughputHeader(header, testThroughputRunID, 0, 0, false); err != nil {
		t.Fatal(err)
	}
	payload := pattern(4096)
	if _, err := client.Write(append(header, payload...)); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("TCP sink did not finish")
	}
	result, err := registry.finish(testThroughputRunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReceivedBytes != uint64(len(payload)) || result.Connections != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRecordThroughputErrorIgnoresDeadlineBoundary(t *testing.T) {
	var count atomic.Uint64
	var last atomic.Value
	recordThroughputError(expiredDeadlineContextObj{}, context.DeadlineExceeded, &count, &last)
	if count.Load() != 0 || last.Load() != nil {
		t.Fatalf("deadline completion counted as error: count=%d last=%v", count.Load(), last.Load())
	}
}

func TestWriteThroughputFullCountsPartialWrite(t *testing.T) {
	wantErr := errors.New("write stopped")
	written, err := writeThroughputFull(partialErrorWriterObj{err: wantErr}, make([]byte, 1024))
	if written != 512 || !errors.Is(err, wantErr) {
		t.Fatalf("write = (%d, %v), want (512, %v)", written, err, wantErr)
	}
}
