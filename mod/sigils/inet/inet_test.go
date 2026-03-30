package inet

import (
	"strings"
	"testing"
)

// // // // // // // // // //
// Name / Keys

func TestName(t *testing.T) {
	if Name() != "inet" {
		t.Fatalf("expected inet, got %s", Name())
	}
}

func TestKeys(t *testing.T) {
	k := Keys()
	if len(k) != 1 || k[0] != "inet" {
		t.Fatalf("unexpected keys: %v", k)
	}
}

// // // // // // // // // //
// New — validation

func TestNew_valid_single(t *testing.T) {
	obj, err := New([]string{"192.168.1.1:8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetName() != "inet" {
		t.Fatal("wrong name")
	}
}

func TestNew_valid_max(t *testing.T) {
	addrs := make([]string, 32)
	for i := range addrs {
		addrs[i] = "addr" + strings.Repeat("x", i)
	}
	_, err := New(addrs)
	if err != nil {
		t.Fatalf("unexpected error with 32 addrs: %v", err)
	}
}

func TestNew_empty(t *testing.T) {
	_, err := New([]string{})
	if err == nil {
		t.Fatal("expected error for empty addrs")
	}
}

func TestNew_nil(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil addrs")
	}
}

func TestNew_tooMany(t *testing.T) {
	addrs := make([]string, 33)
	for i := range addrs {
		addrs[i] = "addr" + strings.Repeat("x", i)
	}
	_, err := New(addrs)
	if err == nil {
		t.Fatal("expected error for >32 addrs")
	}
}

func TestNew_duplicate(t *testing.T) {
	_, err := New([]string{"1.2.3.4:80", "1.2.3.4:80"})
	if err == nil {
		t.Fatal("expected error for duplicate addrs")
	}
}

func TestNew_invalidAddr_tooShort(t *testing.T) {
	_, err := New([]string{"abc"}) // 3 chars, min is 4
	if err == nil {
		t.Fatal("expected error for too short addr")
	}
}

func TestNew_invalidAddr_tooLong(t *testing.T) {
	_, err := New([]string{strings.Repeat("a", 257)})
	if err == nil {
		t.Fatal("expected error for too long addr")
	}
}

func TestNew_invalidAddr_specialChars(t *testing.T) {
	invalid := []string{
		"addr space",
		"addr@host",
		"addr#host",
		"addr!host",
		"addr?host",
	}
	for _, addr := range invalid {
		_, err := New([]string{addr})
		if err == nil {
			t.Fatalf("expected error for addr %q", addr)
		}
	}
}

func TestNew_validAddr_specialChars(t *testing.T) {
	valid := []string{
		"1.2.3.4:80",
		"host/path",
		"a-b_c.d:80",
	}
	for _, addr := range valid {
		_, err := New([]string{addr})
		if err != nil {
			t.Fatalf("unexpected error for addr %q: %v", addr, err)
		}
	}
}

func TestNew_invalidAddr_brackets(t *testing.T) {
	// reAddr does not allow brackets
	_, err := New([]string{"[::1]:8080"})
	if err == nil {
		t.Fatal("expected error for brackets in addr")
	}
}

func TestNew_exactBoundary4(t *testing.T) {
	_, err := New([]string{"abcd"}) // exactly 4 chars
	if err != nil {
		t.Fatalf("unexpected error for 4-char addr: %v", err)
	}
}

func TestNew_exactBoundary256(t *testing.T) {
	_, err := New([]string{strings.Repeat("a", 256)}) // exactly 256 chars
	if err != nil {
		t.Fatalf("unexpected error for 256-char addr: %v", err)
	}
}

// // // // // // // // // //
// Match

func TestMatch_valid(t *testing.T) {
	ni := map[string]any{"inet": []any{"1.2.3.4:80", "5.6.7.8:443"}}
	if !Match(ni) {
		t.Fatal("expected match")
	}
}

func TestMatch_missingKey(t *testing.T) {
	ni := map[string]any{"other": "data"}
	if Match(ni) {
		t.Fatal("expected no match for missing key")
	}
}

func TestMatch_wrongType(t *testing.T) {
	ni := map[string]any{"inet": "not-an-array"}
	if Match(ni) {
		t.Fatal("expected no match for wrong type")
	}
}

func TestMatch_emptyArray(t *testing.T) {
	ni := map[string]any{"inet": []any{}}
	if Match(ni) {
		t.Fatal("expected no match for empty array")
	}
}

func TestMatch_nonStringElement(t *testing.T) {
	ni := map[string]any{"inet": []any{"valid", 123}}
	if Match(ni) {
		t.Fatal("expected no match with non-string element")
	}
}

func TestMatch_intValue(t *testing.T) {
	ni := map[string]any{"inet": 42}
	if Match(ni) {
		t.Fatal("expected no match for int value")
	}
}

func TestMatch_nilValue(t *testing.T) {
	ni := map[string]any{"inet": nil}
	if Match(ni) {
		t.Fatal("expected no match for nil value")
	}
}

func TestMatch_mapValue(t *testing.T) {
	ni := map[string]any{"inet": map[string]any{}}
	if Match(ni) {
		t.Fatal("expected no match for map value")
	}
}

func TestMatch_arrayWithNil(t *testing.T) {
	ni := map[string]any{"inet": []any{nil}}
	if Match(ni) {
		t.Fatal("expected no match for array with nil")
	}
}

// // // // // // // // // //
// Parse

func TestParse_valid(t *testing.T) {
	ni := map[string]any{"inet": []any{"1.2.3.4:80"}}
	obj, err := Parse(ni)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetName() != "inet" {
		t.Fatal("wrong name")
	}
	params := obj.Params()
	addrs, ok := params["inet"].([]string)
	if !ok || len(addrs) != 1 || addrs[0] != "1.2.3.4:80" {
		t.Fatalf("unexpected params: %v", params)
	}
}

func TestParse_noMatch(t *testing.T) {
	ni := map[string]any{"other": "data"}
	_, err := Parse(ni)
	if err == nil {
		t.Fatal("expected error for non-matching NodeInfo")
	}
}

func TestParse_multipleAddrs(t *testing.T) {
	ni := map[string]any{"inet": []any{"a.b:80", "c.d:443"}}
	obj, err := Parse(ni)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	addrs := obj.Params()["inet"].([]string)
	if len(addrs) != 2 {
		t.Fatalf("expected 2 addrs, got %d", len(addrs))
	}
}

// // // // // // // // // //
// ParseParams

func TestParseParams_present(t *testing.T) {
	ni := map[string]any{"inet": []any{"x"}, "other": "y"}
	pp := ParseParams(ni)
	if _, ok := pp["inet"]; !ok {
		t.Fatal("expected inet key")
	}
	if _, ok := pp["other"]; ok {
		t.Fatal("unexpected other key")
	}
}

func TestParseParams_absent(t *testing.T) {
	ni := map[string]any{"other": "y"}
	pp := ParseParams(ni)
	if len(pp) != 0 {
		t.Fatalf("expected empty, got %v", pp)
	}
}

// // // // // // // // // //
// SetParams

func TestSetParams_noConflict(t *testing.T) {
	obj, _ := New([]string{"1.2.3.4:80"})
	ni := map[string]any{"other": "data"}
	result, err := obj.SetParams(ni)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["other"] != "data" {
		t.Fatal("original data lost")
	}
	if result["inet"] == nil {
		t.Fatal("inet key not set")
	}
}

func TestSetParams_conflict(t *testing.T) {
	obj, _ := New([]string{"1.2.3.4:80"})
	ni := map[string]any{"inet": "existing"}
	_, err := obj.SetParams(ni)
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestSetParams_doesNotMutateInput(t *testing.T) {
	obj, _ := New([]string{"1.2.3.4:80"})
	ni := map[string]any{"other": "data"}
	obj.SetParams(ni)
	if _, ok := ni["inet"]; ok {
		t.Fatal("SetParams should not mutate input")
	}
}

// // // // // // // // // //
// Obj.ParseParams

func TestObjParseParams(t *testing.T) {
	obj, _ := New([]string{"original:80"})
	ni := map[string]any{"inet": []any{"foreign:80", "foreign:443"}}
	parsed := obj.ParseParams(ni)
	if _, ok := parsed["inet"]; !ok {
		t.Fatal("expected inet in parsed")
	}
	// obj.addrs should be updated
	addrs := obj.Params()["inet"].([]string)
	if len(addrs) != 2 || addrs[0] != "foreign:80" {
		t.Fatalf("unexpected addrs after ParseParams: %v", addrs)
	}
}

func TestObjParseParams_noInet(t *testing.T) {
	obj, _ := New([]string{"original:80"})
	ni := map[string]any{"other": "data"}
	obj.ParseParams(ni)
	// addrs should remain unchanged since inet key missing
	addrs := obj.Params()["inet"].([]string)
	if len(addrs) != 1 || addrs[0] != "original:80" {
		t.Fatal("addrs should not change when inet key missing")
	}
}

// // // // // // // // // //
// Params

func TestParams_empty(t *testing.T) {
	obj := &Obj{}
	p := obj.Params()
	if len(p) != 0 {
		t.Fatalf("expected empty params, got %v", p)
	}
}

func TestParams_populated(t *testing.T) {
	obj, _ := New([]string{"a.b:80"})
	p := obj.Params()
	if _, ok := p["inet"]; !ok {
		t.Fatal("expected inet key")
	}
}

// // // // // // // // // //
// GetParams

func TestGetParams(t *testing.T) {
	obj, _ := New([]string{"a.b:80"})
	params := obj.GetParams()
	if len(params) != 1 || params[0] != "inet" {
		t.Fatalf("unexpected GetParams: %v", params)
	}
}

// // // // // // // // // //

func BenchmarkNew(b *testing.B) {
	addrs := []string{"192.168.1.1:8080", "10.0.0.1:443", "example.com:80"}
	for b.Loop() {
		New(addrs)
	}
}

func BenchmarkMatch(b *testing.B) {
	ni := map[string]any{"inet": []any{"1.2.3.4:80", "5.6.7.8:443"}}
	for b.Loop() {
		Match(ni)
	}
}

func BenchmarkParse(b *testing.B) {
	ni := map[string]any{"inet": []any{"1.2.3.4:80", "5.6.7.8:443"}}
	for b.Loop() {
		Parse(ni)
	}
}
