package sigil_core

import (
	"testing"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //
// CompileInfo

func TestCompileInfo_sorted(t *testing.T) {
	s := CompileInfo(map[string]sigils.Interface{
		"zzz": &mockSigilObj{name: "zzz"},
		"aaa": &mockSigilObj{name: "aaa"},
	})
	expected := "[aaa,zzz] " + target.GlobalVersion
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestCompileInfo_empty(t *testing.T) {
	s := CompileInfo(nil)
	expected := "[] " + target.GlobalVersion
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestCompileInfo_single(t *testing.T) {
	s := CompileInfo(map[string]sigils.Interface{
		"info": &mockSigilObj{name: "info"},
	})
	expected := "[info] " + target.GlobalVersion
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

// // // // // // // // // //
// ParseInfo

func TestParseInfo_valid(t *testing.T) {
	ver, names, err := ParseInfo("[foo,bar] v1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "v1.0" {
		t.Fatalf("expected version v1.0, got %s", ver)
	}
	if len(names) != 2 || names[0] != "foo" || names[1] != "bar" {
		t.Fatalf("unexpected sigils: %v", names)
	}
}

func TestParseInfo_withPrefix(t *testing.T) {
	ver, names, err := ParseInfo("ratatoskr [a] v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "v2" {
		t.Fatalf("expected version v2, got %s", ver)
	}
	if len(names) != 1 || names[0] != "a" {
		t.Fatalf("unexpected sigils: %v", names)
	}
}

func TestParseInfo_emptySigils(t *testing.T) {
	ver, names, err := ParseInfo("[] v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "v1" {
		t.Fatalf("expected version v1, got %s", ver)
	}
	if len(names) != 0 {
		t.Fatalf("expected no sigils, got %v", names)
	}
}

func TestParseInfo_missingBrackets(t *testing.T) {
	_, _, err := ParseInfo("no brackets v1")
	if err == nil {
		t.Fatal("expected error for missing brackets")
	}
}

func TestParseInfo_missingVersion(t *testing.T) {
	_, _, err := ParseInfo("[foo]")
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestParseInfo_missingVersion_whitespace(t *testing.T) {
	_, _, err := ParseInfo("[foo]   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only version")
	}
}

// // // // // // // // // //

func BenchmarkParseInfo(b *testing.B) {
	raw := "[inet,info,public,services] v0.1.3"
	for b.Loop() {
		ParseInfo(raw)
	}
}
