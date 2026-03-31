package ninfo

import (
	"encoding/json"
	"testing"

	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //
// Parse

func TestParse_noRatatoskrKey(t *testing.T) {
	m := map[string]any{"custom": "data"}
	p := Parse(m)
	if p.Version != "" {
		t.Fatal("expected empty Version for non-ratatoskr nodeinfo")
	}
	if p.Extra["custom"] != "data" {
		t.Fatal("custom key should be in Extra")
	}
	if p.Sigils != nil {
		t.Fatal("expected nil Sigils")
	}
}

func TestParse_withRatatoskrKey(t *testing.T) {
	m := map[string]any{
		target.GlobalName: "[] v1.0",
		"extra_key":       "extra_val",
	}
	p := Parse(m)
	if p.Version != "v1.0" {
		t.Fatalf("expected version v1.0, got %s", p.Version)
	}
	if _, ok := p.Extra[target.GlobalName]; ok {
		t.Fatal("ratatoskr key should be removed from Extra")
	}
	if p.Extra["extra_key"] != "extra_val" {
		t.Fatal("extra key should be preserved")
	}
}

func TestParse_invalidRatatoskrString(t *testing.T) {
	m := map[string]any{
		target.GlobalName: "invalid format",
	}
	p := Parse(m)
	if p.Version != "" {
		t.Fatal("expected empty Version for invalid format")
	}
	if _, ok := p.Extra[target.GlobalName]; !ok {
		t.Fatal("ratatoskr key should remain in Extra on parse failure")
	}
}

func TestParse_nonStringRatatoskrKey(t *testing.T) {
	m := map[string]any{
		target.GlobalName: 12345,
	}
	p := Parse(m)
	if p.Version != "" {
		t.Fatal("expected empty Version for non-string value")
	}
}

func TestParse_doesNotMutateInput(t *testing.T) {
	m := map[string]any{
		target.GlobalName: "[] v1",
		"keep":            "me",
	}
	Parse(m)
	if _, ok := m[target.GlobalName]; !ok {
		t.Fatal("Parse should not mutate the input map")
	}
	if m["keep"] != "me" {
		t.Fatal("Parse should not mutate input values")
	}
}

// // // // // // // // // //
// NodeInfo

func TestParsedObj_NodeInfo_plain(t *testing.T) {
	m := map[string]any{"custom": "data"}
	p := Parse(m)
	ni := p.NodeInfo()
	if ni["custom"] != "data" {
		t.Fatal("expected custom key in NodeInfo")
	}
}

func TestParsedObj_NodeInfo_withSigils(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("aaa", "key1"))
	m := map[string]any{
		target.GlobalName: "[aaa] " + target.GlobalVersion,
		"key1":            "test",
		"extra":           "val",
	}
	p := Parse(m, obj.sigilSlice()...)
	ni := p.NodeInfo()
	if ni["extra"] != "val" {
		t.Fatal("extra key should be in NodeInfo")
	}
	if ni["key1"] != "test" {
		t.Fatal("sigil key should be reassembled")
	}
	if _, ok := ni[target.GlobalName]; !ok {
		t.Fatal("ratatoskr metadata key should be present")
	}
}

func TestParsedObj_NodeInfo_noVersion(t *testing.T) {
	p := &ParsedObj{Extra: map[string]any{"foo": "bar"}}
	ni := p.NodeInfo()
	if _, ok := ni[target.GlobalName]; ok {
		t.Fatal("ratatoskr key should not be present without Version")
	}
}

// // // // // // // // // //
// String

func TestParsedObj_String_validJSON(t *testing.T) {
	m := map[string]any{"name": "test", "version": "1.0"}
	p := Parse(m)
	s := p.String()
	var check map[string]any
	if err := json.Unmarshal([]byte(s), &check); err != nil {
		t.Fatalf("String() returned invalid JSON: %v", err)
	}
	if check["name"] != "test" {
		t.Fatalf("unexpected name: %v", check["name"])
	}
}

func TestParsedObj_String_empty(t *testing.T) {
	p := &ParsedObj{Extra: map[string]any{}}
	s := p.String()
	if s != "{}" {
		t.Fatalf("expected {}, got %s", s)
	}
}

// // // // // // // // // //

func BenchmarkParse_withRatatoskr(b *testing.B) {
	m := map[string]any{
		target.GlobalName: "[inet,info] v0.1.3",
		"buildname":       "yggdrasil",
		"buildversion":    "0.5.13",
		"extra":           "data",
	}
	for b.Loop() {
		Parse(m)
	}
}

func BenchmarkParse_plain(b *testing.B) {
	m := map[string]any{
		"name":    "test",
		"version": "1.0",
	}
	for b.Loop() {
		Parse(m)
	}
}
