package ninfo

import (
	"testing"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //
// ParseRatatoskrInfo

func TestParseRatatoskrInfo_valid(t *testing.T) {
	ri, err := ParseRatatoskrInfo("[foo,bar] v1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.Version != "v1.0" {
		t.Fatalf("expected version v1.0, got %s", ri.Version)
	}
	if len(ri.Sigils) != 2 || ri.Sigils[0] != "foo" || ri.Sigils[1] != "bar" {
		t.Fatalf("unexpected sigils: %v", ri.Sigils)
	}
}

func TestParseRatatoskrInfo_withPrefix(t *testing.T) {
	ri, err := ParseRatatoskrInfo("ratatoskr [a] v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.Version != "v2" {
		t.Fatalf("expected version v2, got %s", ri.Version)
	}
	if len(ri.Sigils) != 1 || ri.Sigils[0] != "a" {
		t.Fatalf("unexpected sigils: %v", ri.Sigils)
	}
}

func TestParseRatatoskrInfo_empty_sigils(t *testing.T) {
	ri, err := ParseRatatoskrInfo("[] v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ri.Sigils) != 0 {
		t.Fatalf("expected no sigils, got %v", ri.Sigils)
	}
}

func TestParseRatatoskrInfo_missingBrackets(t *testing.T) {
	_, err := ParseRatatoskrInfo("no brackets v1")
	if err == nil {
		t.Fatal("expected error for missing brackets")
	}
}

func TestParseRatatoskrInfo_missingVersion(t *testing.T) {
	_, err := ParseRatatoskrInfo("[foo]")
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestParseRatatoskrInfo_missingVersion_whitespace(t *testing.T) {
	_, err := ParseRatatoskrInfo("[foo]   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only version")
	}
}

// // // // // // // // // //
// compileRatatoskrInfo

func TestCompileRatatoskrInfo_sorted(t *testing.T) {
	s := compileRatatoskrInfo(map[string]sigils.Interface{
		"zzz": newMockSigil("zzz"),
		"aaa": newMockSigil("aaa"),
	})
	expected := "[aaa,zzz] " + target.GlobalVersion
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestCompileRatatoskrInfo_empty(t *testing.T) {
	s := compileRatatoskrInfo(nil)
	expected := "[] " + target.GlobalVersion
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

// // // // // // // // // //

func BenchmarkParseRatatoskrInfo(b *testing.B) {
	raw := "[inet,info,public,services] v0.1.3"
	for b.Loop() {
		ParseRatatoskrInfo(raw)
	}
}
