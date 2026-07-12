package ninfo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/inet"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// emptyParseSigilObj always matches and claims its declared keys, but stores
// nothing on parse: it models a user sigil whose keys are all optional.
type emptyParseSigilObj struct {
	name string
	keys []string
}

var _ sigils.Interface = &emptyParseSigilObj{}

func (e *emptyParseSigilObj) GetName() string { return e.name }
func (e *emptyParseSigilObj) GetParams() []string {
	return append([]string(nil), e.keys...)
}
func (e *emptyParseSigilObj) ParseParams(map[string]any) map[string]any {
	return nil
}
func (e *emptyParseSigilObj) Match(map[string]any) bool {
	return true
}
func (e *emptyParseSigilObj) Params() map[string]any {
	return nil
}
func (e *emptyParseSigilObj) Clone() sigils.Interface {
	return &emptyParseSigilObj{name: e.name, keys: append([]string(nil), e.keys...)}
}

// nilCloneParseSigilObj is a user sigil that refuses to clone itself.
type nilCloneParseSigilObj struct {
	emptyParseSigilObj
}

func (n *nilCloneParseSigilObj) Clone() sigils.Interface {
	return nil
}

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
		target.Name: "[] v1.0",
		"extra_key": "extra_val",
	}
	p := Parse(m)
	if p.Version != "v1.0" {
		t.Fatalf("expected version v1.0, got %s", p.Version)
	}
	if _, ok := p.Extra[target.Name]; ok {
		t.Fatal("ratatoskr key should be removed from Extra")
	}
	if p.Extra["extra_key"] != "extra_val" {
		t.Fatal("extra key should be preserved")
	}
}

func TestParse_builtinSigilNameIsReserved(t *testing.T) {
	m := map[string]any{
		target.Name:  "[" + inet.Name() + "] " + target.Version,
		inet.Name():  []any{"example.org"},
		"shadow_key": "keep",
	}

	// A user sigil registered under a built-in name must never override the
	// built-in parser, which wins by resolution order.
	p := Parse(m, newMockSigil(inet.Name(), "shadow_key"))
	if p.Sigils == nil || p.Sigils[inet.Name()] == nil {
		t.Fatal("built-in sigil parser should handle reserved names")
	}
	if _, ok := p.Extra[inet.Name()]; ok {
		t.Fatal("built-in sigil key should be removed from Extra")
	}
	if p.Extra["shadow_key"] != "keep" {
		t.Fatal("custom sigil with reserved name should not override built-in parser")
	}
}

func TestParse_invalidRatatoskrString(t *testing.T) {
	m := map[string]any{
		target.Name: "invalid format",
	}
	p := Parse(m)
	if p.Version != "" {
		t.Fatal("expected empty Version for invalid format")
	}
	if _, ok := p.Extra[target.Name]; !ok {
		t.Fatal("ratatoskr key should remain in Extra on parse failure")
	}
}

func TestParse_invalidVersionLeavesMetadataInExtra(t *testing.T) {
	m := map[string]any{
		target.Name: "[abc] " + strings.Repeat("x", 65),
	}
	p := Parse(m)
	if p.Version != "" {
		t.Fatal("expected empty Version for invalid version")
	}
	if _, ok := p.Extra[target.Name]; !ok {
		t.Fatal("ratatoskr key should remain in Extra on parse failure")
	}
}

func TestParse_nonStringRatatoskrKey(t *testing.T) {
	m := map[string]any{
		target.Name: 12345,
	}
	p := Parse(m)
	if p.Version != "" {
		t.Fatal("expected empty Version for non-string value")
	}
}

func TestParse_doesNotMutateInput(t *testing.T) {
	m := map[string]any{
		target.Name: "[] v1",
		"keep":      "me",
	}
	Parse(m)
	if _, ok := m[target.Name]; !ok {
		t.Fatal("Parse should not mutate the input map")
	}
	if m["keep"] != "me" {
		t.Fatal("Parse should not mutate input values")
	}
}

func TestParse_userSigilWithOptionalKeysIsAccepted(t *testing.T) {
	m := map[string]any{
		target.Name: "[custom] " + target.Version,
		"custom":    "value",
	}
	// A matched user sigil is accepted even when ParseParams stores nothing:
	// optional keys may be absent. Its declared keys are claimed from Extra.
	p := Parse(m, &emptyParseSigilObj{name: "custom", keys: []string{"custom"}})
	if p.Sigils == nil || p.Sigils["custom"] == nil {
		t.Fatal("matched user sigil should be accepted")
	}
	if _, ok := p.Extra["custom"]; ok {
		t.Fatal("declared sigil key should be claimed from Extra")
	}
}

func TestParse_absentUserSigilIsSkipped(t *testing.T) {
	m := map[string]any{
		target.Name: "[custom] " + target.Version,
		"custom":    "value",
	}
	// A user sigil whose Match reports absence must be skipped and must leave its
	// data untouched in Extra. newMockSigil only matches when its keys are present,
	// and "absent" declares a key the map does not carry.
	p := Parse(m, newMockSigil("custom", "absent"))
	if p.Sigils != nil {
		t.Fatal("absent sigil should not be accepted")
	}
	if p.Extra["custom"] != "value" {
		t.Fatal("absent sigil must leave custom data in Extra")
	}
}

func TestParse_nilUserSigilIsSkipped(t *testing.T) {
	m := map[string]any{
		target.Name: "[custom] " + target.Version,
		"custom":    "value",
	}
	// A nil element in the variadic must be skipped, not dereferenced, and must
	// not disrupt parsing of the valid sigil that follows it.
	p := Parse(m, nil, &emptyParseSigilObj{name: "custom", keys: []string{"custom"}})
	if p.Sigils == nil || p.Sigils["custom"] == nil {
		t.Fatal("nil sigil must not disrupt parsing of valid sigils")
	}
}

func TestParse_nilCloneUserSigilIsSkipped(t *testing.T) {
	m := map[string]any{
		target.Name: "[custom] " + target.Version,
		"custom":    "value",
	}
	p := Parse(m, &nilCloneParseSigilObj{
		emptyParseSigilObj: emptyParseSigilObj{name: "custom", keys: []string{"custom"}},
	})
	if p.Sigils != nil {
		t.Fatal("nil-clone sigil should not be accepted")
	}
	if p.Extra["custom"] != "value" {
		t.Fatal("nil-clone sigil must leave custom data in Extra")
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
	obj.AddSigil(slices.Values(seqOf(newMockSigil("aaa", "key1"))))
	m := map[string]any{
		target.Name: "[aaa] " + target.Version,
		"key1":      "test",
		"extra":     "val",
	}
	p := Parse(m, obj.sigilSlice()...)
	ni := p.NodeInfo()
	if ni["extra"] != "val" {
		t.Fatal("extra key should be in NodeInfo")
	}
	if ni["key1"] != "test" {
		t.Fatal("sigil key should be reassembled")
	}
	if _, ok := ni[target.Name]; !ok {
		t.Fatal("ratatoskr metadata key should be present")
	}
}

func TestParsedObj_NodeInfo_preservesRemoteVersion(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(slices.Values(seqOf(newMockSigil("aaa", "key1"))))
	m := map[string]any{
		target.Name: "[aaa] v9.9.9",
		"key1":      "test",
	}
	p := Parse(m, obj.sigilSlice()...)
	ni := p.NodeInfo()
	if ni[target.Name] != "[aaa] v9.9.9" {
		t.Fatalf("unexpected metadata: %v", ni[target.Name])
	}
}

func TestParsedObj_NodeInfo_preservesUnknownSigilNames(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(slices.Values(seqOf(newMockSigil("aaa", "key1"))))
	m := map[string]any{
		target.Name: "[aaa,zzz] v9.9.9",
		"key1":      "test",
		"zzz":       "opaque",
	}
	p := Parse(m, obj.sigilSlice()...)
	ni := p.NodeInfo()
	if ni[target.Name] != "[aaa,zzz] v9.9.9" {
		t.Fatalf("unexpected metadata: %v", ni[target.Name])
	}
	if ni["zzz"] != "opaque" {
		t.Fatal("unknown sigil params should stay in NodeInfo")
	}
}

func TestParsedObj_NodeInfo_noVersion(t *testing.T) {
	p := &ParsedObj{Extra: map[string]any{"foo": "bar"}}
	ni := p.NodeInfo()
	if _, ok := ni[target.Name]; ok {
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
		target.Name:    "[inet,info] v0.1.3",
		"buildname":    "yggdrasil",
		"buildversion": "0.5.13",
		"extra":        "data",
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
