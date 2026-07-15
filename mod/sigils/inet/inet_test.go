package inet

import (
	"reflect"
	"strings"
	"testing"
)

// // // // // // // // // //

func TestNewValidation(t *testing.T) {
	max := make([]string, maxAddrs)
	for i := range max {
		max[i] = strings.Repeat("a", i+4)
	}
	tests := []struct {
		name  string
		addrs []string
		valid bool
	}{
		{name: "single", addrs: []string{"example.com"}, valid: true},
		{name: "maximum count", addrs: max, valid: true},
		{name: "minimum length", addrs: []string{"a.co"}, valid: true},
		{name: "maximum length", addrs: []string{strings.Repeat("a", 256)}, valid: true},
		{name: "empty", addrs: []string{}},
		{name: "nil"},
		{name: "too many", addrs: append(max, "extra.example")},
		{name: "duplicate", addrs: []string{"example.com", "example.com"}},
		{name: "too short", addrs: []string{"abc"}},
		{name: "too long", addrs: []string{strings.Repeat("a", 257)}},
		{name: "invalid character", addrs: []string{"bad?addr"}},
		{name: "brackets", addrs: []string{"[::1]"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			obj, err := New(test.addrs)
			if test.valid {
				if err != nil || obj == nil {
					t.Fatalf("New() = (%v, %v), want valid object", obj, err)
				}
				return
			}
			if err == nil || obj != nil {
				t.Fatalf("New() = (%v, %v), want validation error", obj, err)
			}
		})
	}
}

func TestMatchForeignNodeInfo(t *testing.T) {
	tooMany := make([]any, maxAddrs+1)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("a", i+4)
	}
	tests := []struct {
		name     string
		nodeInfo map[string]any
		match    bool
	}{
		{name: "valid JSON array", nodeInfo: map[string]any{"inet": []any{"example.com", "203.0.113.1"}}, match: true},
		{name: "valid typed array", nodeInfo: map[string]any{"inet": []string{"example.com"}}, match: true},
		{name: "missing", nodeInfo: map[string]any{}},
		{name: "wrong top-level type", nodeInfo: map[string]any{"inet": "example.com"}},
		{name: "empty array", nodeInfo: map[string]any{"inet": []any{}}},
		{name: "non-string element", nodeInfo: map[string]any{"inet": []any{"example.com", 1.0}}},
		{name: "nil element", nodeInfo: map[string]any{"inet": []any{nil}}},
		{name: "invalid address", nodeInfo: map[string]any{"inet": []any{"bad?addr"}}},
		{name: "too many", nodeInfo: map[string]any{"inet": tooMany}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Match(test.nodeInfo); got != test.match {
				t.Fatalf("Match() = %v, want %v", got, test.match)
			}
		})
	}
}

func TestParseAndObjectUpdate(t *testing.T) {
	nodeInfo := map[string]any{"inet": []any{"example.com", "203.0.113.1"}, "other": true}
	obj, err := Parse(nodeInfo)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := obj.Addrs(); !reflect.DeepEqual(got, []string{"example.com", "203.0.113.1"}) {
		t.Fatalf("Addrs() = %v", got)
	}
	if _, err := Parse(map[string]any{}); err == nil {
		t.Fatal("Parse accepted missing inet data")
	}
	if _, err := Parse(map[string]any{"inet": []any{"bad?addr"}}); err == nil {
		t.Fatal("Parse accepted invalid inet data")
	}

	current, err := New([]string{"old.example"})
	if err != nil {
		t.Fatal(err)
	}
	parsed := current.ParseParams(nodeInfo)
	if !reflect.DeepEqual(parsed, map[string]any{"inet": nodeInfo["inet"]}) {
		t.Fatalf("ParseParams() = %#v", parsed)
	}
	if got := current.Addrs(); !reflect.DeepEqual(got, []string{"example.com", "203.0.113.1"}) {
		t.Fatalf("object was not updated: %v", got)
	}
	current.ParseParams(map[string]any{"inet": []any{"bad?addr"}})
	if got := current.Addrs(); !reflect.DeepEqual(got, []string{"example.com", "203.0.113.1"}) {
		t.Fatalf("invalid update changed object: %v", got)
	}
}

func TestOwnershipAndMerge(t *testing.T) {
	input := []string{"example.com"}
	obj, err := New(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = "changed.example"
	if got := obj.Addrs()[0]; got != "example.com" {
		t.Fatalf("constructor retained input alias: %q", got)
	}

	addrs := obj.Addrs()
	addrs[0] = "changed.example"
	params := obj.Params()
	params["inet"].([]string)[0] = "changed.example"
	clone := obj.Clone().(*Obj)
	clone.addrs[0] = "changed.example"
	if got := obj.Addrs()[0]; got != "example.com" {
		t.Fatalf("returned data retained object alias: %q", got)
	}

	base := map[string]any{"other": "value"}
	merged, err := obj.SetParams(base)
	if err != nil {
		t.Fatalf("SetParams: %v", err)
	}
	if _, exists := base["inet"]; exists {
		t.Fatal("SetParams mutated input")
	}
	if !reflect.DeepEqual(merged["inet"], []string{"example.com"}) {
		t.Fatalf("merged inet = %#v", merged["inet"])
	}
	if _, err := obj.SetParams(map[string]any{"inet": "occupied"}); err == nil {
		t.Fatal("SetParams accepted a key conflict")
	}
	if obj.GetName() != Name() || !reflect.DeepEqual(obj.GetParams(), Keys()) {
		t.Fatal("interface identity does not match package identity")
	}
}
