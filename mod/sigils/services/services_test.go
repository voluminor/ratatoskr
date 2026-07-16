package services

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// // // // // // // // // //

func TestNewValidation(t *testing.T) {
	maximum := make(map[string]uint16, maxServices)
	for i := range maxServices {
		maximum["svc_"+strings.Repeat("a", i/26)+string(rune('a'+i%26))] = uint16(i + 1)
	}
	tooMany := cloneServices(maximum)
	tooMany["extra"] = 1
	tests := []struct {
		name     string
		services map[string]uint16
		valid    bool
	}{
		{name: "single", services: map[string]uint16{"http": 80}, valid: true},
		{name: "maximum count", services: maximum, valid: true},
		{name: "minimum port", services: map[string]uint16{"http": 1}, valid: true},
		{name: "maximum port", services: map[string]uint16{"http": 65535}, valid: true},
		{name: "name boundaries", services: map[string]uint16{"ab": 1, strings.Repeat("a", 32): 2}, valid: true},
		{name: "name punctuation", services: map[string]uint16{"http_alt-1": 80}, valid: true},
		{name: "empty", services: map[string]uint16{}},
		{name: "nil"},
		{name: "too many", services: tooMany},
		{name: "zero port", services: map[string]uint16{"http": 0}},
		{name: "short name", services: map[string]uint16{"a": 1}},
		{name: "long name", services: map[string]uint16{strings.Repeat("a", 33): 1}},
		{name: "uppercase name", services: map[string]uint16{"HTTP": 80}},
		{name: "dot in name", services: map[string]uint16{"http.alt": 80}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			obj, err := New(test.services)
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
	tooMany := make(map[string]any, maxServices+1)
	for i := 0; i <= maxServices; i++ {
		tooMany["svc_"+strings.Repeat("a", i/26)+string(rune('a'+i%26))] = float64(i + 1)
	}
	tests := []struct {
		name     string
		nodeInfo map[string]any
		match    bool
	}{
		{name: "valid JSON map", nodeInfo: map[string]any{"services": map[string]any{"http": float64(80)}}, match: true},
		{name: "valid typed map", nodeInfo: map[string]any{"services": map[string]uint16{"http": 80}}, match: true},
		{name: "missing", nodeInfo: map[string]any{}},
		{name: "wrong top-level type", nodeInfo: map[string]any{"services": []any{}}},
		{name: "empty map", nodeInfo: map[string]any{"services": map[string]any{}}},
		{name: "invalid name", nodeInfo: map[string]any{"services": map[string]any{"HTTP": float64(80)}}},
		{name: "integer Go type", nodeInfo: map[string]any{"services": map[string]any{"http": 80}}},
		{name: "zero", nodeInfo: map[string]any{"services": map[string]any{"http": float64(0)}}},
		{name: "negative", nodeInfo: map[string]any{"services": map[string]any{"http": float64(-1)}}},
		{name: "too high", nodeInfo: map[string]any{"services": map[string]any{"http": float64(65536)}}},
		{name: "fractional", nodeInfo: map[string]any{"services": map[string]any{"http": 80.5}}},
		{name: "NaN", nodeInfo: map[string]any{"services": map[string]any{"http": math.NaN()}}},
		{name: "infinity", nodeInfo: map[string]any{"services": map[string]any{"http": math.Inf(1)}}},
		{name: "nil port", nodeInfo: map[string]any{"services": map[string]any{"http": nil}}},
		{name: "too many", nodeInfo: map[string]any{"services": tooMany}},
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
	nodeInfo := map[string]any{"services": map[string]any{"http": float64(80), "ssh": float64(22)}}
	obj, err := Parse(nodeInfo)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := obj.Services(); !reflect.DeepEqual(got, map[string]uint16{"http": 80, "ssh": 22}) {
		t.Fatalf("Services() = %v", got)
	}
	if _, err := Parse(map[string]any{}); err == nil {
		t.Fatal("Parse accepted missing services")
	}
	if _, err := Parse(map[string]any{"services": map[string]any{"http": 80.5}}); err == nil {
		t.Fatal("Parse accepted invalid port")
	}

	current, err := New(map[string]uint16{"old": 1})
	if err != nil {
		t.Fatal(err)
	}
	current.ParseParams(nodeInfo)
	if got := current.Services(); !reflect.DeepEqual(got, map[string]uint16{"http": 80, "ssh": 22}) {
		t.Fatalf("object was not updated: %v", got)
	}
	current.ParseParams(map[string]any{"services": map[string]any{"http": 80.5}})
	if got := current.Services(); !reflect.DeepEqual(got, map[string]uint16{"http": 80, "ssh": 22}) {
		t.Fatalf("invalid update changed object: %v", got)
	}
}

func TestOwnershipAndMerge(t *testing.T) {
	input := map[string]uint16{"http": 80}
	obj, err := New(input)
	if err != nil {
		t.Fatal(err)
	}
	input["http"] = 81
	services := obj.Services()
	services["http"] = 82
	params := obj.Params()
	params["services"].(map[string]uint16)["http"] = 83
	clone := obj.Clone().(*Obj)
	clone.services["http"] = 84
	if got := obj.Services()["http"]; got != 80 {
		t.Fatalf("mutable alias changed object: %d", got)
	}

	base := map[string]any{"other": "value"}
	merged, err := obj.SetParams(base)
	if err != nil {
		t.Fatalf("SetParams: %v", err)
	}
	if _, exists := base["services"]; exists {
		t.Fatal("SetParams mutated input")
	}
	if got := merged["services"].(map[string]uint16)["http"]; got != 80 {
		t.Fatalf("merged service port = %d", got)
	}
	if _, err := obj.SetParams(map[string]any{"services": "occupied"}); err == nil {
		t.Fatal("SetParams accepted a key conflict")
	}
	if obj.GetName() != Name() || !reflect.DeepEqual(obj.GetParams(), Keys()) {
		t.Fatal("interface identity does not match package identity")
	}
}
