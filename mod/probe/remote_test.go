package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //
// parseRemotePeersResponse

func TestParseRemotePeersResponse_valid(t *testing.T) {
	keys := genKeyN(t, 2)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{
		Keys: []string{
			hex.EncodeToString(keys[0]),
			hex.EncodeToString(keys[1]),
		},
	})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongType(t *testing.T) {
	_, _, err := parseRemotePeersResponse("not a map", DefaultMaxPeersPerNode)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestParseRemotePeersResponse_invalidHex(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"zzzz_not_hex"}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for invalid hex, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongKeyLength(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{hex.EncodeToString(make([]byte, 10))}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, _, _ := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for wrong key length, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_empty(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_nonRawMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{"n": "string value"}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_truncatesOverLimit(t *testing.T) {
	keys := genKeyN(t, 3)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{
		Keys: []string{
			hex.EncodeToString(keys[0]),
			hex.EncodeToString(keys[1]),
			hex.EncodeToString(keys[2]),
		},
	})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	peers, truncated, err := parseRemotePeersResponse(resp, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated flag for over-limit peer set")
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers after truncation, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_rejectsOversizedMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(`{"keys":["` + string(make([]byte, maxRemotePeerMessageBytes)) + `"]}`),
	}
	_, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if !errors.Is(err, ErrRemoteResponseTooLarge) {
		t.Fatalf("expected ErrRemoteResponseTooLarge, got %v", err)
	}
}

func TestParseRemotePeersResponse_invalidKeysDoNotConsumeLimit(t *testing.T) {
	valid := genKeyN(t, 2)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"bad-1", hex.EncodeToString(valid[0]), "bad-2", hex.EncodeToString(valid[1])}})
	resp := yggcore.DebugGetPeersResponse{"node1": json.RawMessage(inner)}
	peers, truncated, err := parseRemotePeersResponse(resp, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("valid keys within the cap must not truncate")
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 valid peers, got %d", len(peers))
	}
}

func TestCallRemotePeers_detachedCallSurvivesCallerCancel(t *testing.T) {
	key := genKey(t)
	peer := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{hex.EncodeToString(peer)}})
	obj := &Obj{
		logger: noopLoggerObj{},
		remotePeers: func(json.RawMessage) (interface{}, error) {
			close(started)
			<-release
			defer close(finished)
			return yggcore.DebugGetPeersResponse{"node": json.RawMessage(inner)}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(ctx, key)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote handler did not start")
	}
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation, got %v", err)
	}
	// The detached call keeps running to completion after the caller left.
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("detached remote call did not finish after caller cancel")
	}
}

func TestCallRemotePeers_closeWaitsForDetachedCall(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	obj := &Obj{
		logger: noopLoggerObj{},
		remotePeers: func(json.RawMessage) (interface{}, error) {
			close(started)
			<-release
			return yggcore.DebugGetPeersResponse{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(ctx, key)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("remote handler did not start")
	}
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("expected caller cancellation, got %v", err)
	}

	closed := make(chan struct{})
	go func() {
		obj.Close()
		close(closed)
	}()
	select {
	case <-closed:
		close(release)
		t.Fatal("Close returned before detached call finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after detached call finished")
	}
}

func TestCloseContextTimesOutWithoutAbandoningAcceptedCall(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	obj := &Obj{
		logger: noopLoggerObj{},
		remotePeers: func(json.RawMessage) (interface{}, error) {
			close(started)
			<-release
			return yggcore.DebugGetPeersResponse{}, nil
		},
	}

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	callerDone := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(callerCtx, key)
		callerDone <- err
	}()
	<-started
	cancelCaller()
	if err := <-callerDone; !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("caller error = %v, want context.Canceled", err)
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := obj.CloseContext(closeCtx)
	cancelClose()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("CloseContext error = %v, want context.DeadlineExceeded", err)
	}
	if _, _, err = obj.callRemotePeers(context.Background(), key); !errors.Is(err, ErrClosed) {
		close(release)
		t.Fatalf("call after CloseContext error = %v, want ErrClosed", err)
	}

	closed := make(chan struct{})
	go func() {
		obj.Close()
		close(closed)
	}()
	select {
	case <-closed:
		close(release)
		t.Fatal("Close returned before the accepted call finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the accepted call returned")
	}
}

func TestCloseContextReturnsSuccessWhenAlreadyClosed(t *testing.T) {
	obj := &Obj{}
	obj.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := obj.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext after completed Close = %v, want nil", err)
	}
}

func TestCallRemotePeers_sameKeySingleFlight(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	obj := &Obj{
		logger:        noopLoggerObj{},
		ctx:           context.Background(),
		remoteSem:     make(chan struct{}, 4),
		remoteFlights: make(map[[ed25519.PublicKeySize]byte]*remoteFlightObj),
		remotePeers: func(json.RawMessage) (interface{}, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return yggcore.DebugGetPeersResponse{}, nil
		},
	}
	firstCtx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(firstCtx, key)
		firstDone <- err
	}()
	<-started
	secondDone := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(context.Background(), key)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller error = %v", err)
	}
	close(release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second caller: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying calls = %d, want 1", got)
	}
}

// // // // // // // // // //
// AdminCaptureObj

func TestAdminCapture(t *testing.T) {
	capture := common.NewAdminCapture()
	fn := func(json.RawMessage) (interface{}, error) { return nil, nil }
	if err := capture.AddHandler("test_fn", "description", nil, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.Handlers["test_fn"] == nil {
		t.Fatal("handler not captured")
	}
	if capture.Handlers["missing"] != nil {
		t.Fatal("unexpected handler for missing key")
	}
}

// // // // // // // // // //

func BenchmarkParseRemotePeersResponse(b *testing.B) {
	keys := make([]string, 20)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = hex.EncodeToString(pk)
	}
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: keys})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	for b.Loop() {
		if _, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode); err != nil {
			b.Fatalf("parseRemotePeersResponse: %v", err)
		}
	}
}
