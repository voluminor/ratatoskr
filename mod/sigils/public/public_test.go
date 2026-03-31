package public

import (
	"strings"
	"testing"
)

// // // // // // // // // //
// Name / Keys

func TestName(t *testing.T) {
	if Name() != "public" {
		t.Fatalf("expected public, got %s", Name())
	}
}

func TestKeys(t *testing.T) {
	k := Keys()
	if len(k) != 1 || k[0] != "public" {
		t.Fatalf("unexpected keys: %v", k)
	}
}

// // // // // // // // // //
// New — validation

func TestNew_valid(t *testing.T) {
	obj, err := New(map[string][]string{
		"europe": {"tls://1.2.3.4:12345", "tls://5.6.7.8:12345"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetName() != "public" {
		t.Fatal("wrong name")
	}
}

func TestNew_multipleGroups(t *testing.T) {
	peers := make(map[string][]string)
	for i := range 8 {
		name := "gr" + strings.Repeat("x", i)
		peers[name] = []string{"tls://1.2.3.4:12345"}
	}
	_, err := New(peers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_empty(t *testing.T) {
	_, err := New(map[string][]string{})
	if err == nil {
		t.Fatal("expected error for empty peers")
	}
}

func TestNew_nil(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil peers")
	}
}

func TestNew_tooManyGroups(t *testing.T) {
	peers := make(map[string][]string)
	for i := range 9 {
		name := "gr" + strings.Repeat("x", i)
		peers[name] = []string{"tls://1.2.3.4:12345"}
	}
	_, err := New(peers)
	if err == nil {
		t.Fatal("expected error for >8 groups")
	}
}

func TestNew_emptyGroup(t *testing.T) {
	_, err := New(map[string][]string{"europe": {}})
	if err == nil {
		t.Fatal("expected error for empty group")
	}
}

func TestNew_tooManyURIs(t *testing.T) {
	uris := make([]string, 17)
	for i := range uris {
		uris[i] = "tls://1.2.3.4:" + strings.Repeat("1", 5) + strings.Repeat("x", i)
	}
	_, err := New(map[string][]string{"europe": uris})
	if err == nil {
		t.Fatal("expected error for >16 URIs per group")
	}
}

func TestNew_exactMaxURIs(t *testing.T) {
	uris := make([]string, 16)
	for i := range uris {
		uris[i] = "tls://host" + strings.Repeat("x", i) + ":12345"
	}
	_, err := New(map[string][]string{"europe": uris})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_invalidGroupName_tooShort(t *testing.T) {
	_, err := New(map[string][]string{"a": {"tls://1.2.3.4:12345"}})
	if err == nil {
		t.Fatal("expected error for 1-char group name")
	}
}

func TestNew_invalidGroupName_tooLong(t *testing.T) {
	_, err := New(map[string][]string{
		strings.Repeat("a", 17): {"tls://1.2.3.4:12345"},
	})
	if err == nil {
		t.Fatal("expected error for 17-char group name")
	}
}

func TestNew_invalidGroupName_uppercase(t *testing.T) {
	_, err := New(map[string][]string{"Europe": {"tls://1.2.3.4:12345"}})
	if err == nil {
		t.Fatal("expected error for uppercase group name")
	}
}

func TestNew_invalidGroupName_specialChar(t *testing.T) {
	_, err := New(map[string][]string{"eu-west": {"tls://1.2.3.4:12345"}}) // dash not in regex
	if err == nil {
		t.Fatal("expected error for dash in group name")
	}
}

func TestNew_validGroupName_boundary(t *testing.T) {
	_, err := New(map[string][]string{
		"ab":                    {"tls://1.2.3.4:12345"}, // min 2
		strings.Repeat("a", 16): {"tls://5.6.7.8:12345"}, // max 16
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_invalidURI_tooShort(t *testing.T) {
	_, err := New(map[string][]string{"eu": {"tls://a"}}) // 7 chars, min 8
	if err == nil {
		t.Fatal("expected error for too short URI")
	}
}

func TestNew_invalidURI_tooLong(t *testing.T) {
	_, err := New(map[string][]string{"eu": {strings.Repeat("a", 257)}})
	if err == nil {
		t.Fatal("expected error for too long URI")
	}
}

func TestNew_validURI_boundary(t *testing.T) {
	_, err := New(map[string][]string{
		"eu": {"tls://ab"}, // 8 chars
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_invalidURI_specialChars(t *testing.T) {
	invalid := []string{
		"tls://a b:80",  // space
		"tls://a\tb:80", // tab
		"tls://a#b:80",  // hash
		"tls://a?b:80",  // question mark
	}
	for _, uri := range invalid {
		_, err := New(map[string][]string{"eu": {uri}})
		if err == nil {
			t.Fatalf("expected error for URI %q", uri)
		}
	}
}

// // // // // // // // // //
// Match

func TestMatch_valid(t *testing.T) {
	ni := map[string]any{
		"public": map[string]any{
			"eu": []any{"tls://1.2.3.4:12345"},
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
	if Match(map[string]any{"public": "string"}) {
		t.Fatal("expected no match for string")
	}
}

func TestMatch_emptyMap(t *testing.T) {
	if Match(map[string]any{"public": map[string]any{}}) {
		t.Fatal("expected no match for empty map")
	}
}

func TestMatch_invalidGroupName(t *testing.T) {
	ni := map[string]any{
		"public": map[string]any{
			"EU": []any{"tls://1.2.3.4:12345"}, // uppercase
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for invalid group name")
	}
}

func TestMatch_valueNotArray(t *testing.T) {
	ni := map[string]any{
		"public": map[string]any{
			"eu": "not-array",
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for non-array value")
	}
}

func TestMatch_arrayElementNotString(t *testing.T) {
	ni := map[string]any{
		"public": map[string]any{
			"eu": []any{123},
		},
	}
	if Match(ni) {
		t.Fatal("expected no match for non-string element")
	}
}

func TestMatch_nilValue(t *testing.T) {
	if Match(map[string]any{"public": nil}) {
		t.Fatal("expected no match for nil")
	}
}

func TestMatch_intValue(t *testing.T) {
	if Match(map[string]any{"public": 42}) {
		t.Fatal("expected no match for int")
	}
}

// // // // // // // // // //
// Parse

func TestParse_valid(t *testing.T) {
	ni := map[string]any{
		"public": map[string]any{
			"eu": []any{"tls://1.2.3.4:12345"},
			"us": []any{"tls://5.6.7.8:12345"},
		},
	}
	obj, err := Parse(ni)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := obj.Params()
	peers := p["public"].(map[string][]string)
	if len(peers) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(peers))
	}
	if peers["eu"][0] != "tls://1.2.3.4:12345" {
		t.Fatalf("unexpected URI: %s", peers["eu"][0])
	}
}

func TestParse_noMatch(t *testing.T) {
	_, err := Parse(map[string]any{"other": "data"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// // // // // // // // // //
// ParseParams

func TestParseParams_present(t *testing.T) {
	ni := map[string]any{"public": map[string]any{"eu": []any{"x"}}, "other": "y"}
	pp := ParseParams(ni)
	if _, ok := pp["public"]; !ok {
		t.Fatal("expected public key")
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
	obj, _ := New(map[string][]string{"eu": {"tls://1.2.3.4:12345"}})
	result, err := obj.SetParams(map[string]any{"other": "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["public"] == nil {
		t.Fatal("public key not set")
	}
	if result["other"] != "data" {
		t.Fatal("other lost")
	}
}

func TestSetParams_conflict(t *testing.T) {
	obj, _ := New(map[string][]string{"eu": {"tls://1.2.3.4:12345"}})
	_, err := obj.SetParams(map[string]any{"public": "existing"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestSetParams_doesNotMutateInput(t *testing.T) {
	obj, _ := New(map[string][]string{"eu": {"tls://1.2.3.4:12345"}})
	ni := map[string]any{"other": "data"}
	obj.SetParams(ni)
	if _, ok := ni["public"]; ok {
		t.Fatal("SetParams should not mutate input")
	}
}

// // // // // // // // // //
// Obj.ParseParams

func TestObjParseParams(t *testing.T) {
	obj, _ := New(map[string][]string{"eu": {"tls://original:12345"}})
	ni := map[string]any{
		"public": map[string]any{
			"us": []any{"tls://foreign:12345"},
		},
	}
	obj.ParseParams(ni)
	peers := obj.Params()["public"].(map[string][]string)
	if len(peers) != 1 {
		t.Fatalf("expected 1 group, got %d", len(peers))
	}
	if peers["us"][0] != "tls://foreign:12345" {
		t.Fatalf("unexpected URI: %s", peers["us"][0])
	}
}

func TestObjParseParams_noPublic(t *testing.T) {
	obj, _ := New(map[string][]string{"eu": {"tls://original:12345"}})
	ni := map[string]any{"other": "data"}
	obj.ParseParams(ni)
	// peers should remain unchanged
	peers := obj.Params()["public"].(map[string][]string)
	if peers["eu"][0] != "tls://original:12345" {
		t.Fatal("peers should not change when public key missing")
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
	obj, _ := New(map[string][]string{"eu": {"tls://1.2.3.4:12345"}})
	p := obj.Params()
	if _, ok := p["public"]; !ok {
		t.Fatal("expected public key")
	}
}

// // // // // // // // // //

func BenchmarkNew(b *testing.B) {
	peers := map[string][]string{
		"eu": {"tls://1.2.3.4:12345", "tls://5.6.7.8:12345"},
		"us": {"tls://10.0.0.1:12345"},
	}
	for b.Loop() {
		New(peers)
	}
}

func BenchmarkMatch(b *testing.B) {
	ni := map[string]any{
		"public": map[string]any{
			"eu": []any{"tls://1.2.3.4:12345", "tls://5.6.7.8:12345"},
			"us": []any{"tls://10.0.0.1:12345"},
		},
	}
	for b.Loop() {
		Match(ni)
	}
}

func BenchmarkParse(b *testing.B) {
	ni := map[string]any{
		"public": map[string]any{
			"eu": []any{"tls://1.2.3.4:12345"},
		},
	}
	for b.Loop() {
		Parse(ni)
	}
}
