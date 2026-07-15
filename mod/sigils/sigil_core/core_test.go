package sigil_core

import (
	"reflect"
	"strings"
	"testing"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

func TestCompileInfo(t *testing.T) {
	tests := []struct {
		name   string
		sigils map[string]sigils.Interface
		want   string
	}{
		{name: "empty", want: "[] " + target.Version},
		{name: "single", sigils: map[string]sigils.Interface{"info": &mockSigilObj{name: "info"}}, want: "[info] " + target.Version},
		{name: "sorted", sigils: map[string]sigils.Interface{
			"zzz": &mockSigilObj{name: "zzz"},
			"aaa": &mockSigilObj{name: "aaa"},
		}, want: "[aaa,zzz] " + target.Version},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CompileInfo(test.sigils); got != test.want {
				t.Fatalf("CompileInfo() = %q, want %q", got, test.want)
			}
		})
	}

	names := []string{"zzz", "aaa", "zzz"}
	if got := CompileInfoNames(names, "v1"); got != "[aaa,zzz] v1" {
		t.Fatalf("CompileInfoNames() = %q", got)
	}
	if !reflect.DeepEqual(names, []string{"zzz", "aaa", "zzz"}) {
		t.Fatalf("CompileInfoNames mutated input: %v", names)
	}
}

func TestParseInfo(t *testing.T) {
	tooMany := make([]string, maxInfoSigils+1)
	for i := range tooMany {
		tooMany[i] = "sig" + string(rune('a'+i%26)) + string(rune('a'+i/26%26))
	}
	tests := []struct {
		name      string
		raw       string
		version   string
		sigils    []string
		wantError bool
	}{
		{name: "valid", raw: "[foo,bar] v1.0", version: "v1.0", sigils: []string{"foo", "bar"}},
		{name: "prefix", raw: "ratatoskr [abc] v2", version: "v2", sigils: []string{"abc"}},
		{name: "empty sigils", raw: "[] v1", version: "v1"},
		{name: "deduplicate", raw: "[foo,bar,foo] v1", version: "v1", sigils: []string{"foo", "bar"}},
		{name: "missing brackets", raw: "no brackets v1", wantError: true},
		{name: "missing version", raw: "[foo]", wantError: true},
		{name: "blank version", raw: "[foo]   ", wantError: true},
		{name: "invalid name", raw: "[ok,bad name] v1", wantError: true},
		{name: "too many names", raw: "[" + strings.Join(tooMany, ",") + "] v1", wantError: true},
		{name: "long version", raw: "[abc] " + strings.Repeat("x", maxInfoVersionLength+1), wantError: true},
		{name: "control version", raw: "[abc] v1\x1b[31m", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, names, err := ParseInfo(test.raw)
			if test.wantError {
				if err == nil {
					t.Fatalf("ParseInfo(%q) succeeded", test.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseInfo(%q): %v", test.raw, err)
			}
			if version != test.version || !reflect.DeepEqual(names, test.sigils) {
				t.Fatalf("ParseInfo(%q) = (%q, %v), want (%q, %v)", test.raw, version, names, test.version, test.sigils)
			}
		})
	}
}
