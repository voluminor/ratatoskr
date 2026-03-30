package ninfo

import (
	"encoding/json"
	"testing"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //
// adminCaptureObj

func TestAdminCapture(t *testing.T) {
	cap := &adminCaptureObj{handlers: make(map[string]yggcore.AddHandlerFunc)}
	fn := func(json.RawMessage) (interface{}, error) { return nil, nil }
	if err := cap.AddHandler("test_fn", "desc", nil, fn); err != nil {
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
// extractBuildInfo

func TestExtractBuildInfo_allFields(t *testing.T) {
	extra := map[string]any{
		"buildname":     "yggdrasil",
		"buildversion":  "0.5.13",
		"buildplatform": "linux",
		"buildarch":     "amd64",
		"custom":        "value",
	}
	bi := extractBuildInfo(extra)
	if bi == nil {
		t.Fatal("expected non-nil BuildInfoObj")
	}
	if bi.Name != "yggdrasil" || bi.Version != "0.5.13" || bi.Platform != "linux" || bi.Arch != "amd64" {
		t.Fatalf("unexpected build info: %+v", bi)
	}
	if _, ok := extra["buildname"]; ok {
		t.Fatal("build keys should be removed from extra")
	}
	if extra["custom"] != "value" {
		t.Fatal("non-build keys should be preserved")
	}
}

func TestExtractBuildInfo_partial(t *testing.T) {
	extra := map[string]any{
		"buildname": "yggdrasil",
	}
	bi := extractBuildInfo(extra)
	if bi == nil {
		t.Fatal("expected non-nil for partial build info")
	}
	if bi.Name != "yggdrasil" {
		t.Fatalf("expected yggdrasil, got %s", bi.Name)
	}
	if bi.Version != "" || bi.Platform != "" || bi.Arch != "" {
		t.Fatal("missing fields should be empty strings")
	}
}

func TestExtractBuildInfo_nil_when_empty(t *testing.T) {
	extra := map[string]any{"custom": "value"}
	bi := extractBuildInfo(extra)
	if bi != nil {
		t.Fatalf("expected nil, got %+v", bi)
	}
}

func TestExtractBuildInfo_nonString_ignored(t *testing.T) {
	extra := map[string]any{
		"buildname": 123,
	}
	bi := extractBuildInfo(extra)
	if bi != nil {
		t.Fatal("expected nil when build fields are not strings")
	}
}

// // // // // // // // // //
// callNodeInfo response parsing

func TestCallNodeInfo_wrongType(t *testing.T) {
	obj := &Obj{
		nodeInfo: func(json.RawMessage) (interface{}, error) {
			return "not a GetNodeInfoResponse", nil
		},
	}
	_, err := obj.callNodeInfo([32]byte{})
	if err != ErrUnexpectedResponse {
		t.Fatalf("expected ErrUnexpectedResponse, got %v", err)
	}
}

func TestCallNodeInfo_emptyResponse(t *testing.T) {
	obj := &Obj{
		nodeInfo: func(json.RawMessage) (interface{}, error) {
			return yggcore.GetNodeInfoResponse{}, nil
		},
	}
	_, err := obj.callNodeInfo([32]byte{})
	if err != ErrEmptyResponse {
		t.Fatalf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestCallNodeInfo_validResponse(t *testing.T) {
	obj := &Obj{
		nodeInfo: func(json.RawMessage) (interface{}, error) {
			return yggcore.GetNodeInfoResponse{
				"abcd": json.RawMessage(`{"name":"test"}`),
			}, nil
		},
	}
	raw, err := obj.callNodeInfo([32]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != `{"name":"test"}` {
		t.Fatalf("unexpected raw: %s", raw)
	}
}

// // // // // // // // // //

func BenchmarkExtractBuildInfo(b *testing.B) {
	for b.Loop() {
		extra := map[string]any{
			"buildname":     "yggdrasil",
			"buildversion":  "0.5.13",
			"buildplatform": "linux",
			"buildarch":     "amd64",
			"custom":        "value",
		}
		extractBuildInfo(extra)
	}
}
