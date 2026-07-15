package sigils

import (
	"reflect"
	"strings"
	"testing"
)

// // // // // // // // // //

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{name: "abc", valid: true},
		{name: strings.Repeat("a", 32), valid: true},
		{name: "my.sigil", valid: true},
		{name: "my-sigil", valid: true},
		{name: "my_sigil", valid: true},
		{name: "123", valid: true},
		{name: ""},
		{name: "ab"},
		{name: strings.Repeat("a", 33)},
		{name: "ABC"},
		{name: "foo bar"},
		{name: "foo/bar"},
		{name: "foo:bar"},
		{name: "foo@bar"},
		{name: "фоо"},
		{name: "abc\t"},
		{name: "abc\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateName(test.name); got != test.valid {
				t.Fatalf("ValidateName(%q) = %v, want %v", test.name, got, test.valid)
			}
		})
	}
}

func TestMergeParamsOwnsTopLevelMapAndRejectsConflicts(t *testing.T) {
	nodeInfo := map[string]any{"base": "value"}
	params := map[string]any{"sigil": "data"}
	merged, err := MergeParams(nodeInfo, params)
	if err != nil {
		t.Fatalf("MergeParams: %v", err)
	}
	merged["base"] = "changed"
	if !reflect.DeepEqual(nodeInfo, map[string]any{"base": "value"}) {
		t.Fatalf("input map changed: %#v", nodeInfo)
	}
	if _, err := MergeParams(nodeInfo, map[string]any{"base": "conflict"}); err == nil {
		t.Fatal("MergeParams accepted a key conflict")
	}
}
