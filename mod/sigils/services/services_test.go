package services

import (
	"math"
	"strings"
	"testing"
)

// // // // // // // // // //
// Name / Keys

func TestName(t *testing.T) {
	if Name() != "services" {
		t.Fatalf("expected services, got %s", Name())
	}
}

func TestKeys(t *testing.T) {
	k := Keys()
	if len(k) != 1 || k[0] != "services" {
		t.Fatalf("unexpected keys: %v", k)
	}
	k[0] = "changed"
	if Keys()[0] != "services" {
		t.Fatal("Keys leaked internal slice")
	}
}

// // // // // // // // // //
// New — validation

func TestNew_valid(t *testing.T) {
	obj, err := New(map[string]uint16{"http": 80, "ssh": 22})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetName() != "services" {
		t.Fatal("wrong name")
	}
}

func TestNew_singleService(t *testing.T) {
	_, err := New(map[string]uint16{"http": 80})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_empty(t *testing.T) {
	_, err := New(map[string]uint16{})
	if err == nil {
		t.Fatal("expected error for empty services")
	}
}

func TestNew_nil(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil services")
	}
}

func TestNew_tooMany(t *testing.T) {
	svc2 := make(map[string]uint16)
	for i := range 257 {
		name := "svc" + string(rune('a'+i/26/26%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
		svc2[name] = uint16(i%65535 + 1)
	}
	_, err := New(svc2)
	if err == nil {
		t.Fatal("expected error for >256 services")
	}
}

func TestNew_exactMax(t *testing.T) {
	svc := make(map[string]uint16)
	for i := range 256 {
		name := "svc" + string(rune('a'+i/26/26%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
		svc[name] = uint16(i%65535 + 1)
	}
	_, err := New(svc)
	if err != nil {
		t.Fatalf("unexpected error for 256 services: %v", err)
	}
}

func TestNew_portZero(t *testing.T) {
	_, err := New(map[string]uint16{"http": 0})
	if err == nil {
		t.Fatal("expected error for port 0")
	}
}

func TestNew_port1(t *testing.T) {
	_, err := New(map[string]uint16{"http": 1})
	if err != nil {
		t.Fatalf("unexpected error for port 1: %v", err)
	}
}

func TestNew_port65535(t *testing.T) {
	_, err := New(map[string]uint16{"http": 65535})
	if err != nil {
		t.Fatalf("unexpected error for port 65535: %v", err)
	}
}

func TestNew_invalidName_tooShort(t *testing.T) {
	_, err := New(map[string]uint16{"a": 80}) // 1 char, min 2
	if err == nil {
		t.Fatal("expected error for too short name")
	}
}

func TestNew_invalidName_tooLong(t *testing.T) {
	_, err := New(map[string]uint16{strings.Repeat("a", 33): 80})
	if err == nil {
		t.Fatal("expected error for too long name")
	}
}

func TestNew_invalidName_uppercase(t *testing.T) {
	_, err := New(map[string]uint16{"HTTP": 80})
	if err == nil {
		t.Fatal("expected error for uppercase name")
	}
}

func TestNew_invalidName_dot(t *testing.T) {
	_, err := New(map[string]uint16{"http.server": 80}) // dot not in regex
	if err == nil {
		t.Fatal("expected error for dot in name")
	}
}

func TestNew_validName_boundary(t *testing.T) {
	_, err := New(map[string]uint16{
		"ab":                    80,
		strings.Repeat("a", 32): 443,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_validName_dashUnderscore(t *testing.T) {
	_, err := New(map[string]uint16{"my-svc": 80, "my_svc": 443})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// // // // // // // // // //
// Match

func TestMatch_valid(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(80),
			"ssh":  float64(22),
		},
	}
	if !Match(ni) {
		t.Fatal("expected match")
	}
}

func TestMatch_missingKey(t *testing.T) {
	if Match(map[string]any{"other": "data"}) {
		t.Fatal("expected no match")
	}
}

func TestMatch_wrongType(t *testing.T) {
	if Match(map[string]any{"services": "string"}) {
		t.Fatal("expected no match for string")
	}
}

func TestMatch_emptyMap(t *testing.T) {
	if Match(map[string]any{"services": map[string]any{}}) {
		t.Fatal("expected no match for empty map")
	}
}

func TestMatch_invalidServiceName(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"HTTP": float64(80), // uppercase
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for invalid name")
	}
}

func TestMatch_portNotFloat(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": "80",
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for string port")
	}
}

func TestMatch_portZero(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(0),
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for port 0")
	}
}

func TestMatch_portNegative(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(-1),
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for negative port")
	}
}

func TestMatch_portTooHigh(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(65536),
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for port >65535")
	}
}

func TestMatch_portFractional(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(80.5),
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for fractional port")
	}
}

func TestMatch_portNonFinite(t *testing.T) {
	for _, port := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		ni := map[string]any{
			"services": map[string]any{
				"http": port,
			},
		}
		if Match(ni) {
			t.Fatalf("expected no match for non-finite port %v", port)
		}
	}
}

func TestMatch_port65535(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(65535),
		},
	}
	if !Match(ni) {
		t.Fatal("expected match for port 65535")
	}
}

func TestMatch_port1(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(1),
		},
	}
	if !Match(ni) {
		t.Fatal("expected match for port 1")
	}
}

func TestMatch_nilValue(t *testing.T) {
	if Match(map[string]any{"services": nil}) {
		t.Fatal("expected no match for nil")
	}
}

func TestMatch_portIsInt(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": 80, // int, not float64
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for int port (JSON uses float64)")
	}
}

func TestMatch_portNil(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": nil,
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for nil port")
	}
}

// // // // // // // // // //
// Parse

func TestParse_valid(t *testing.T) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(80),
			"ssh":  float64(22),
		},
	}
	obj, err := Parse(ni)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := obj.Params()
	svc := p["services"].(map[string]uint16)
	if svc["http"] != 80 || svc["ssh"] != 22 {
		t.Fatalf("unexpected services: %v", svc)
	}
}

func TestParse_noMatch(t *testing.T) {
	_, err := Parse(map[string]any{"other": "data"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_rejectsTooManyServices(t *testing.T) {
	svc := make(map[string]any, maxServices+1)
	for i := range maxServices + 1 {
		name := "svc" + string(rune('a'+i/26/26%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
		svc[name] = float64(i%65535 + 1)
	}
	_, err := Parse(map[string]any{"services": svc})
	if err == nil {
		t.Fatal("expected error for too many parsed services")
	}
}

// // // // // // // // // //
// ParseParams

func TestParseParams_present(t *testing.T) {
	ni := map[string]any{"services": map[string]any{"http": float64(80)}, "other": "y"}
	pp := ParseParams(ni)
	if _, ok := pp["services"]; !ok {
		t.Fatal("expected services key")
	}
	if _, ok := pp["other"]; ok {
		t.Fatal("unexpected other key")
	}
}

func TestParseParams_absent(t *testing.T) {
	pp := ParseParams(map[string]any{"other": "y"})
	if len(pp) != 0 {
		t.Fatalf("expected empty, got %v", pp)
	}
}

// // // // // // // // // //
// SetParams

func TestSetParams_noConflict(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	result, err := obj.SetParams(map[string]any{"other": "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["services"] == nil {
		t.Fatal("services key not set")
	}
	if result["other"] != "data" {
		t.Fatal("other lost")
	}
}

func TestSetParams_conflict(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	_, err := obj.SetParams(map[string]any{"services": "existing"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestSetParams_doesNotMutateInput(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	ni := map[string]any{"other": "data"}
	if _, err := obj.SetParams(ni); err != nil {
		t.Fatalf("SetParams: %v", err)
	}
	if _, ok := ni["services"]; ok {
		t.Fatal("SetParams should not mutate input")
	}
}

func TestNew_clonesInput(t *testing.T) {
	services := map[string]uint16{"http": 80}
	obj, err := New(services)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	services["http"] = 81
	if got := obj.Services()["http"]; got != 80 {
		t.Fatalf("input mutation changed object: %d", got)
	}
}

func TestAccessorsReturnCopy(t *testing.T) {
	obj, err := New(map[string]uint16{"http": 80})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := obj.Services()
	out["http"] = 81
	if got := obj.Services()["http"]; got != 80 {
		t.Fatalf("accessor must return a copy, internal data leaked: %d", got)
	}
}

func TestParams_returnsIndependentCopy(t *testing.T) {
	obj, err := New(map[string]uint16{"http": 80})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Mutating a returned fragment must not reach internal state.
	obj.Params()["services"].(map[string]uint16)["http"] = 81
	if got := obj.Params()["services"].(map[string]uint16)["http"]; got != 80 {
		t.Fatalf("Params leaked internal map: %d", got)
	}
	// Two calls must yield independent maps.
	a := obj.Params()["services"].(map[string]uint16)
	b := obj.Params()["services"].(map[string]uint16)
	a["http"] = 8080
	if b["http"] == 8080 {
		t.Fatal("Params returned aliased maps across calls")
	}
}

// // // // // // // // // //
// Obj.ParseParams

func TestObjParseParams(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	ni := map[string]any{
		"services": map[string]any{
			"ssh":  float64(22),
			"xmpp": float64(5222),
		},
	}
	obj.ParseParams(ni)
	svc := obj.Params()["services"].(map[string]uint16)
	if svc["ssh"] != 22 {
		t.Fatal("expected ssh=22")
	}
	if svc["xmpp"] != 5222 {
		t.Fatal("expected xmpp=5222")
	}
}

func TestObjParseParams_rejectsInvalidSet(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	ni := map[string]any{
		"services": map[string]any{
			"valid":    float64(22),
			"zero":     float64(0),
			"negative": float64(-1),
			"too-high": float64(65536),
			"fraction": float64(80.5),
			"string":   "not-a-port",
		},
	}
	obj.ParseParams(ni)
	svc := obj.Params()["services"].(map[string]uint16)
	if len(svc) != 1 || svc["http"] != 80 {
		t.Fatalf("invalid parse should keep previous services, got %v", svc)
	}
}

func TestObjParseParams_noServices(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	ni := map[string]any{"other": "data"}
	obj.ParseParams(ni)
	// services should remain unchanged
	svc := obj.Params()["services"].(map[string]uint16)
	if svc["http"] != 80 {
		t.Fatal("services should not change when key missing")
	}
}

// // // // // // // // // //
// Params

func TestParams_empty(t *testing.T) {
	obj := &Obj{}
	p := obj.Params()
	if len(p) != 0 {
		t.Fatalf("expected empty, got %v", p)
	}
}

func TestParams_populated(t *testing.T) {
	obj, _ := New(map[string]uint16{"http": 80})
	p := obj.Params()
	if _, ok := p["services"]; !ok {
		t.Fatal("expected services key")
	}
}

// // // // // // // // // //

func BenchmarkNew(b *testing.B) {
	svc := map[string]uint16{"http": 80, "ssh": 22, "xmpp": 5222}
	for b.Loop() {
		if _, err := New(svc); err != nil {
			b.Fatalf("New: %v", err)
		}
	}
}

func BenchmarkMatch(b *testing.B) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(80),
			"ssh":  float64(22),
			"xmpp": float64(5222),
		},
	}
	for b.Loop() {
		Match(ni)
	}
}

func BenchmarkParse(b *testing.B) {
	ni := map[string]any{
		"services": map[string]any{
			"http": float64(80),
			"ssh":  float64(22),
		},
	}
	for b.Loop() {
		if _, err := Parse(ni); err != nil {
			b.Fatalf("Parse: %v", err)
		}
	}
}
