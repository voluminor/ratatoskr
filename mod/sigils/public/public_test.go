package public

import (
	"reflect"
	"strings"
	"testing"
)

// // // // // // // // // //

func TestNewValidation(t *testing.T) {
	maximum := make(map[string][]string, maxGroups)
	for i := range maxGroups {
		uris := make([]string, maxURIsPerGroup)
		for j := range uris {
			uris[j] = "tls://host" + strings.Repeat("a", i) + ":" + strings.Repeat("1", j+1)
		}
		maximum["group"+string(rune('a'+i))] = uris
	}
	tooManyGroups := clonePeers(maximum)
	tooManyGroups["extra"] = []string{"tls://x:1"}
	tooManyURIs := make([]string, maxURIsPerGroup+1)
	for i := range tooManyURIs {
		tooManyURIs[i] = "tls://x:" + strings.Repeat("1", i+1)
	}
	tests := []struct {
		name  string
		peers map[string][]string
		valid bool
	}{
		{name: "single", peers: map[string][]string{"internet": {"tls://example.com:443"}}, valid: true},
		{name: "maximum counts", peers: maximum, valid: true},
		{name: "name boundaries", peers: map[string][]string{"ab": {"tls://x:1"}, strings.Repeat("a", 16): {"tls://x:2"}}, valid: true},
		{name: "URI boundaries", peers: map[string][]string{"ab": {"tls://x:1", strings.Repeat("a", 256)}}, valid: true},
		{name: "empty", peers: map[string][]string{}},
		{name: "nil"},
		{name: "too many groups", peers: tooManyGroups},
		{name: "empty group", peers: map[string][]string{"internet": {}}},
		{name: "too many URIs", peers: map[string][]string{"internet": tooManyURIs}},
		{name: "short group", peers: map[string][]string{"a": {"tls://x:1"}}},
		{name: "long group", peers: map[string][]string{strings.Repeat("a", 17): {"tls://x:1"}}},
		{name: "uppercase group", peers: map[string][]string{"Internet": {"tls://x:1"}}},
		{name: "group punctuation", peers: map[string][]string{"inter-net": {"tls://x:1"}}},
		{name: "short URI", peers: map[string][]string{"internet": {"tcp://x"}}},
		{name: "long URI", peers: map[string][]string{"internet": {strings.Repeat("a", 257)}}},
		{name: "URI whitespace", peers: map[string][]string{"internet": {"tls://bad host:1"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			obj, err := New(test.peers)
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
	tooMany := make(map[string]any, maxGroups+1)
	for i := 0; i <= maxGroups; i++ {
		tooMany["group"+string(rune('a'+i))] = []any{"tls://x:1"}
	}
	tests := []struct {
		name     string
		nodeInfo map[string]any
		match    bool
	}{
		{name: "valid JSON map", nodeInfo: map[string]any{"public": map[string]any{"internet": []any{"tls://example.com:443"}}}, match: true},
		{name: "valid typed map", nodeInfo: map[string]any{"public": map[string][]string{"internet": {"tls://example.com:443"}}}, match: true},
		{name: "missing", nodeInfo: map[string]any{}},
		{name: "wrong top-level type", nodeInfo: map[string]any{"public": []any{}}},
		{name: "empty map", nodeInfo: map[string]any{"public": map[string]any{}}},
		{name: "invalid group", nodeInfo: map[string]any{"public": map[string]any{"Internet": []any{"tls://x:1"}}}},
		{name: "group value not array", nodeInfo: map[string]any{"public": map[string]any{"internet": "tls://x:1"}}},
		{name: "array element not string", nodeInfo: map[string]any{"public": map[string]any{"internet": []any{1.0}}}},
		{name: "empty group", nodeInfo: map[string]any{"public": map[string]any{"internet": []any{}}}},
		{name: "invalid URI", nodeInfo: map[string]any{"public": map[string]any{"internet": []any{"bad"}}}},
		{name: "nil value", nodeInfo: map[string]any{"public": nil}},
		{name: "too many groups", nodeInfo: map[string]any{"public": tooMany}},
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
	nodeInfo := map[string]any{"public": map[string]any{"internet": []any{"tls://example.com:443"}}}
	obj, err := Parse(nodeInfo)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string][]string{"internet": {"tls://example.com:443"}}
	if got := obj.Peers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Peers() = %v", got)
	}
	if _, err := Parse(map[string]any{}); err == nil {
		t.Fatal("Parse accepted missing public data")
	}
	if _, err := Parse(map[string]any{"public": map[string]any{"internet": []any{"bad"}}}); err == nil {
		t.Fatal("Parse accepted invalid URI")
	}

	current, err := New(map[string][]string{"old": {"tls://old:1"}})
	if err != nil {
		t.Fatal(err)
	}
	current.ParseParams(nodeInfo)
	if got := current.Peers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("object was not updated: %v", got)
	}
	current.ParseParams(map[string]any{"public": map[string]any{"internet": []any{"bad"}}})
	if got := current.Peers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid update changed object: %v", got)
	}
}

func TestOwnershipAndMerge(t *testing.T) {
	input := map[string][]string{"internet": {"tls://example.com:443"}}
	obj, err := New(input)
	if err != nil {
		t.Fatal(err)
	}
	input["internet"][0] = "tls://changed:1"
	peers := obj.Peers()
	peers["internet"][0] = "tls://changed:2"
	params := obj.Params()
	params["public"].(map[string][]string)["internet"][0] = "tls://changed:3"
	clone := obj.Clone().(*Obj)
	clone.peers["internet"][0] = "tls://changed:4"
	if got := obj.Peers()["internet"][0]; got != "tls://example.com:443" {
		t.Fatalf("mutable alias changed object: %q", got)
	}

	base := map[string]any{"other": "value"}
	merged, err := obj.SetParams(base)
	if err != nil {
		t.Fatalf("SetParams: %v", err)
	}
	if _, exists := base["public"]; exists {
		t.Fatal("SetParams mutated input")
	}
	if got := merged["public"].(map[string][]string)["internet"][0]; got != "tls://example.com:443" {
		t.Fatalf("merged URI = %q", got)
	}
	if _, err := obj.SetParams(map[string]any{"public": "occupied"}); err == nil {
		t.Fatal("SetParams accepted a key conflict")
	}
	if obj.GetName() != Name() || !reflect.DeepEqual(obj.GetParams(), Keys()) {
		t.Fatal("interface identity does not match package identity")
	}
}
