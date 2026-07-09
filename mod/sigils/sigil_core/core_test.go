package sigil_core

import (
	"strings"
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
	expected := "[aaa,zzz] " + target.Version
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestCompileInfo_empty(t *testing.T) {
	s := CompileInfo(nil)
	expected := "[] " + target.Version
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestCompileInfo_single(t *testing.T) {
	s := CompileInfo(map[string]sigils.Interface{
		"info": &mockSigilObj{name: "info"},
	})
	expected := "[info] " + target.Version
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
	ver, names, err := ParseInfo("ratatoskr [abc] v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "v2" {
		t.Fatalf("expected version v2, got %s", ver)
	}
	if len(names) != 1 || names[0] != "abc" {
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

func TestParseInfo_deduplicates(t *testing.T) {
	_, names, err := ParseInfo("[foo,bar,foo] v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 || names[0] != "foo" || names[1] != "bar" {
		t.Fatalf("unexpected sigils: %v", names)
	}
}

func TestParseInfo_invalidName(t *testing.T) {
	_, _, err := ParseInfo("[ok,bad name] v1")
	if err == nil {
		t.Fatal("expected error for invalid sigil name")
	}
}

func TestParseInfo_tooManyNames(t *testing.T) {
	body := "sig"
	for i := 0; i < maxInfoSigils; i++ {
		body += ",sig" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/26/26%26))
	}
	_, _, err := ParseInfo("[" + body + "] v1")
	if err == nil {
		t.Fatal("expected error for too many sigil names")
	}
}

func TestParseInfo_rejectsLongVersion(t *testing.T) {
	_, _, err := ParseInfo("[abc] " + strings.Repeat("x", maxInfoVersionLength+1))
	if err == nil {
		t.Fatal("expected error for long version")
	}
}

func TestParseInfo_rejectsControlVersion(t *testing.T) {
	_, _, err := ParseInfo("[abc] v1\x1b[31m")
	if err == nil {
		t.Fatal("expected error for control version")
	}
}

// // // // // // // // // //

func BenchmarkParseInfo(b *testing.B) {
	raw := "[inet,info,public,services] v0.1.3"
	for b.Loop() {
		if _, _, err := ParseInfo(raw); err != nil {
			b.Fatalf("ParseInfo: %v", err)
		}
	}
}
