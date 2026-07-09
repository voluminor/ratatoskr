package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	peers, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongType(t *testing.T) {
	_, err := parseRemotePeersResponse("not a map", DefaultMaxPeersPerNode)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestParseRemotePeersResponse_invalidHex(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"zzzz_not_hex"}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
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
	peers, _ := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for wrong key length, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_empty(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{}
	peers, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_nonRawMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{"n": "string value"}
	peers, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_peerLimitExceeded(t *testing.T) {
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
	_, err := parseRemotePeersResponse(resp, 2)
	if !errors.Is(err, ErrPeersPerNodeLimitExceeded) {
		t.Fatalf("expected ErrPeersPerNodeLimitExceeded, got %v", err)
	}
}

func TestParseRemotePeersResponse_rejectsOversizedMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(`{"keys":["` + string(make([]byte, maxRemotePeerMessageBytes)) + `"]}`),
	}
	_, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if !errors.Is(err, ErrRemoteResponseTooLarge) {
		t.Fatalf("expected ErrRemoteResponseTooLarge, got %v", err)
	}
}

func TestParseRemotePeersResponse_countsInvalidKeysAgainstLimit(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"bad-1", "bad-2", "bad-3"}})
	resp := yggcore.DebugGetPeersResponse{"node1": json.RawMessage(inner)}
	_, err := parseRemotePeersResponse(resp, 2)
	if !errors.Is(err, ErrPeersPerNodeLimitExceeded) {
		t.Fatalf("expected ErrPeersPerNodeLimitExceeded, got %v", err)
	}
}

func TestCallRemotePeers_cachesResultAfterCallerCancel(t *testing.T) {
	key := genKey(t)
	peer := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{hex.EncodeToString(peer)}})
	obj := &Obj{
		logger:          noopLoggerObj{},
		cache:           newPeerCache(time.Minute, defaultCacheMaxEntries),
		maxPeersPerNode: DefaultMaxPeersPerNode,
		remotePeers: func(json.RawMessage) (interface{}, error) {
			close(started)
			<-release
			return yggcore.DebugGetPeersResponse{"node": json.RawMessage(inner)}, nil
		},
	}
	defer obj.cache.close()

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
	close(release)

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		peers, _, ok := obj.cache.get(toKeyArray(key))
		if ok {
			if len(peers) != 1 || !peers[0].Equal(peer) {
				t.Fatalf("unexpected cached peers: %v", peers)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for remote result to enter cache")
		case <-ticker.C:
		}
	}
}

func TestCallRemotePeers_closeWaitsForDetachedCall(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	obj := &Obj{
		logger:          noopLoggerObj{},
		cache:           newPeerCache(time.Minute, defaultCacheMaxEntries),
		maxPeersPerNode: DefaultMaxPeersPerNode,
		remoteLimit:     common.NewDynamicLimit(1),
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
		if _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode); err != nil {
			b.Fatalf("parseRemotePeersResponse: %v", err)
		}
	}
}
