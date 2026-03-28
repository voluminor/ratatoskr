package traceroute

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"testing"

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
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongType(t *testing.T) {
	_, err := parseRemotePeersResponse("not a map")
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestParseRemotePeersResponse_invalidHex(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"zzzz_not_hex"}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, err := parseRemotePeersResponse(resp)
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
	peers, _ := parseRemotePeersResponse(resp)
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for wrong key length, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_empty(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{}
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_nonRawMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{"n": "string value"}
	peers, err := parseRemotePeersResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

// // // // // // // // // //
// adminCaptureObj

func TestAdminCapture(t *testing.T) {
	cap := &adminCaptureObj{handlers: make(map[string]yggcore.AddHandlerFunc)}
	fn := func(json.RawMessage) (interface{}, error) { return nil, nil }
	if err := cap.AddHandler("test_fn", "description", nil, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.handlers["test_fn"] == nil {
		t.Fatal("handler not captured")
	}
	if cap.handlers["missing"] != nil {
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
		parseRemotePeersResponse(resp)
	}
}
