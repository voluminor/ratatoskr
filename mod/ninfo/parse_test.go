package ninfo

import (
	"testing"

	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //
// Parse

func TestParse_noRatatoskrKey(t *testing.T) {
	m := map[string]any{"custom": "data"}
	p := Parse(m)
	if p.Info != nil {
		t.Fatal("expected nil Info for non-ratatoskr nodeinfo")
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
	if p.Info == nil {
		t.Fatal("expected non-nil Info")
	}
	if p.Info.Version != "v1.0" {
		t.Fatalf("expected version v1.0, got %s", p.Info.Version)
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
	if p.Info != nil {
		t.Fatal("expected nil Info for invalid format")
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
	if p.Info != nil {
		t.Fatal("expected nil Info for non-string value")
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
