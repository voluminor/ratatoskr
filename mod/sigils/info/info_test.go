package info

import (
	"strings"
	"testing"
)

// // // // // // // // // //
// Name / Keys

func TestName(t *testing.T) {
	if Name() != "info" {
		t.Fatalf("expected info, got %s", Name())
	}
}

func TestKeys(t *testing.T) {
	k := Keys()
	if len(k) != 5 {
		t.Fatalf("expected 5 keys, got %d", len(k))
	}
}

// // // // // // // // // //
// New — validation

func TestNew_minimal(t *testing.T) {
	obj, err := New(ConfigObj{
		Name: "test.node",
		Type: "server",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetName() != "info" {
		t.Fatal("wrong name")
	}
}

func TestNew_full(t *testing.T) {
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Location: "Moscow datacenter",
		Contacts: map[string][]string{
			"email": {"admin@example.com"},
			"xmpp":  {"admin@jabber.example.com"},
		},
		Description: "open peering policy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_missingName(t *testing.T) {
	_, err := New(ConfigObj{Type: "server"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestNew_missingType(t *testing.T) {
	_, err := New(ConfigObj{Name: "test.node"})
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestNew_emptyNameAndType(t *testing.T) {
	_, err := New(ConfigObj{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

// //

func TestNew_nameTooShort(t *testing.T) {
	_, err := New(ConfigObj{Name: "abc", Type: "server"}) // 3 chars, min 4
	if err == nil {
		t.Fatal("expected error for short name")
	}
}

func TestNew_nameExact4(t *testing.T) {
	_, err := New(ConfigObj{Name: "abcd", Type: "sv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_nameExact64(t *testing.T) {
	_, err := New(ConfigObj{Name: strings.Repeat("a", 64), Type: "sv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_nameTooLong(t *testing.T) {
	_, err := New(ConfigObj{Name: strings.Repeat("a", 65), Type: "sv"})
	if err == nil {
		t.Fatal("expected error for 65-char name")
	}
}

func TestNew_nameUppercase(t *testing.T) {
	_, err := New(ConfigObj{Name: "Test.Node", Type: "sv"})
	if err == nil {
		t.Fatal("expected error for uppercase name")
	}
}

func TestNew_nameWithSpace(t *testing.T) {
	_, err := New(ConfigObj{Name: "test node", Type: "sv"})
	if err == nil {
		t.Fatal("expected error for name with space")
	}
}

// //

func TestNew_typeTooShort(t *testing.T) {
	_, err := New(ConfigObj{Name: "test.node", Type: "a"}) // 1 char, min 2
	if err == nil {
		t.Fatal("expected error for short type")
	}
}

func TestNew_typeExact2(t *testing.T) {
	_, err := New(ConfigObj{Name: "test.node", Type: "sv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_typeExact32(t *testing.T) {
	_, err := New(ConfigObj{Name: "test.node", Type: strings.Repeat("a", 32)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_typeTooLong(t *testing.T) {
	_, err := New(ConfigObj{Name: "test.node", Type: strings.Repeat("a", 33)})
	if err == nil {
		t.Fatal("expected error for 33-char type")
	}
}

func TestNew_typeUppercase(t *testing.T) {
	_, err := New(ConfigObj{Name: "test.node", Type: "Server"})
	if err == nil {
		t.Fatal("expected error for uppercase type")
	}
}

// //

func TestNew_invalidPeering_singleChar(t *testing.T) {
	_, err := New(ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Description: "a", // reText requires at least 2 non-space chars
	})
	if err == nil {
		t.Fatal("expected error for single-char peering")
	}
}

func TestNew_invalidPeering_leadingSpace(t *testing.T) {
	_, err := New(ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Description: " leading space",
	})
	if err == nil {
		t.Fatal("expected error for leading space in peering")
	}
}

func TestNew_invalidPeering_trailingSpace(t *testing.T) {
	_, err := New(ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Description: "trailing space ",
	})
	if err == nil {
		t.Fatal("expected error for trailing space in peering")
	}
}

func TestNew_validPeering_minimal(t *testing.T) {
	_, err := New(ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Description: "ok",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_peeringTooLong(t *testing.T) {
	// reText: ^\S[\S ]{0,512}\S$ — so total max is 514 chars
	_, err := New(ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Description: "x" + strings.Repeat(" ", 513) + "x", // 515 chars
	})
	if err == nil {
		t.Fatal("expected error for too long peering")
	}
}

func TestNew_emptyPeering_ok(t *testing.T) {
	_, err := New(ConfigObj{
		Name: "test.node",
		Type: "server",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// //

func TestNew_tooManyContactGroups(t *testing.T) {
	contacts := make(map[string][]string)
	for i := range 9 {
		name := strings.Repeat("a", 2) + strings.Repeat("b", i)
		contacts[name] = []string{"contact@example.com"}
	}
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: contacts,
	})
	if err == nil {
		t.Fatal("expected error for >8 contact groups")
	}
}

func TestNew_exactMaxContactGroups(t *testing.T) {
	contacts := make(map[string][]string)
	for i := range 8 {
		name := strings.Repeat("a", 2) + strings.Repeat("b", i)
		contacts[name] = []string{"contact@example.com"}
	}
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: contacts,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_emptyContactGroup(t *testing.T) {
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: map[string][]string{"email": {}},
	})
	if err == nil {
		t.Fatal("expected error for empty contact group")
	}
}

func TestNew_tooManyContactsPerGroup(t *testing.T) {
	contacts := make([]string, 9)
	for i := range contacts {
		contacts[i] = "contact" + strings.Repeat("x", i) + "@example.com"
	}
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: map[string][]string{"email": contacts},
	})
	if err == nil {
		t.Fatal("expected error for >8 contacts per group")
	}
}

func TestNew_invalidContactGroupName(t *testing.T) {
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: map[string][]string{"A": {"foo@bar.com"}}, // uppercase, too short
	})
	if err == nil {
		t.Fatal("expected error for invalid group name")
	}
}

func TestNew_invalidContactValue(t *testing.T) {
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: map[string][]string{"email": {"a"}}, // too short for reContacts
	})
	if err == nil {
		t.Fatal("expected error for invalid contact value")
	}
}

func TestNew_validContact(t *testing.T) {
	_, err := New(ConfigObj{
		Name:     "test.node",
		Type:     "server",
		Contacts: map[string][]string{"email": {"admin@example.com"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// // // // // // // // // //
// Match

func TestMatch_valid_nameAndType(t *testing.T) {
	ni := map[string]any{"name": "test", "type": "server"}
	if !Match(ni) {
		t.Fatal("expected match with name+type")
	}
}

func TestMatch_valid_full(t *testing.T) {
	ni := map[string]any{
		"name":        "test",
		"type":        "server",
		"location":    "Moscow",
		"contact":     map[string]any{"email": []any{"foo@bar.com"}},
		"description": "open",
	}
	if !Match(ni) {
		t.Fatal("expected match with full data")
	}
}

func TestMatch_missingName(t *testing.T) {
	ni := map[string]any{"type": "server"}
	if Match(ni) {
		t.Fatal("expected no match without name")
	}
}

func TestMatch_missingType(t *testing.T) {
	ni := map[string]any{"name": "test"}
	if Match(ni) {
		t.Fatal("expected no match without type")
	}
}

func TestMatch_nameNotString(t *testing.T) {
	ni := map[string]any{"name": 123, "type": "server"}
	if Match(ni) {
		t.Fatal("expected no match for non-string name")
	}
}

func TestMatch_typeNotString(t *testing.T) {
	ni := map[string]any{"name": "test", "type": 123}
	if Match(ni) {
		t.Fatal("expected no match for non-string type")
	}
}

func TestMatch_locationNotString(t *testing.T) {
	ni := map[string]any{"name": "test", "type": "server", "location": 123}
	if Match(ni) {
		t.Fatal("expected no match for non-string location")
	}
}

func TestMatch_contactWrongType(t *testing.T) {
	ni := map[string]any{"name": "test", "type": "server", "contact": "string"}
	if Match(ni) {
		t.Fatal("expected no match for string contact")
	}
}

func TestMatch_contactValueNotArray(t *testing.T) {
	ni := map[string]any{
		"name": "test", "type": "server",
		"contact": map[string]any{"email": "not-array"},
	}
	if Match(ni) {
		t.Fatal("expected no match for non-array contact value")
	}
}

func TestMatch_contactElementNotString(t *testing.T) {
	ni := map[string]any{
		"name": "test", "type": "server",
		"contact": map[string]any{"email": []any{123}},
	}
	if Match(ni) {
		t.Fatal("expected no match for non-string contact element")
	}
}

func TestMatch_empty(t *testing.T) {
	ni := map[string]any{}
	if Match(ni) {
		t.Fatal("expected no match for empty map")
	}
}

func TestMatch_peeringNotString(t *testing.T) {
	ni := map[string]any{"name": "test", "type": "server", "description": 42}
	if Match(ni) {
		t.Fatal("expected no match for non-string peering")
	}
}

// // // // // // // // // //
// Parse

func TestParse_valid(t *testing.T) {
	ni := map[string]any{
		"name":        "test.node",
		"type":        "server",
		"location":    "EU",
		"description": "open",
		"contact": map[string]any{
			"email": []any{"a@b.com"},
		},
	}
	obj, err := Parse(ni)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := obj.Params()
	if p["name"] != "test.node" {
		t.Fatalf("unexpected name: %v", p["name"])
	}
	if p["location"] != "EU" {
		t.Fatalf("unexpected location: %v", p["location"])
	}
}

func TestParse_noMatch(t *testing.T) {
	_, err := Parse(map[string]any{"foo": "bar"})
	if err == nil {
		t.Fatal("expected error for non-matching NodeInfo")
	}
}

func TestParse_nameAndTypeOnly(t *testing.T) {
	obj, err := Parse(map[string]any{"name": "test", "type": "sv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := obj.Params()
	if p["name"] != "test" || p["type"] != "sv" {
		t.Fatalf("unexpected params: %v", p)
	}
	if _, ok := p["location"]; ok {
		t.Fatal("location should be absent")
	}
}

// // // // // // // // // //
// ParseParams

func TestParseParams_selectsOnlyKeys(t *testing.T) {
	ni := map[string]any{
		"name":    "test",
		"type":    "server",
		"unknown": "data",
	}
	pp := ParseParams(ni)
	if _, ok := pp["name"]; !ok {
		t.Fatal("expected name")
	}
	if _, ok := pp["unknown"]; ok {
		t.Fatal("unexpected unknown key")
	}
}

func TestParseParams_empty(t *testing.T) {
	pp := ParseParams(map[string]any{})
	if len(pp) != 0 {
		t.Fatalf("expected empty, got %v", pp)
	}
}

// // // // // // // // // //
// SetParams

func TestSetParams_noConflict(t *testing.T) {
	obj, _ := New(ConfigObj{Name: "test.node", Type: "sv"})
	result, err := obj.SetParams(map[string]any{"other": "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "test.node" {
		t.Fatal("name not set")
	}
	if result["type"] != "sv" {
		t.Fatal("type not set")
	}
	if result["other"] != "data" {
		t.Fatal("other lost")
	}
}

func TestSetParams_conflict(t *testing.T) {
	obj, _ := New(ConfigObj{Name: "test.node", Type: "sv"})
	_, err := obj.SetParams(map[string]any{"name": "existing"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestSetParams_skipsEmpty(t *testing.T) {
	obj, _ := New(ConfigObj{Name: "test.node", Type: "sv"})
	result, _ := obj.SetParams(map[string]any{})
	if _, ok := result["location"]; ok {
		t.Fatal("empty location should not be set")
	}
	if _, ok := result["description"]; ok {
		t.Fatal("empty peering should not be set")
	}
	if _, ok := result["contact"]; ok {
		t.Fatal("empty contacts should not be set")
	}
}

func TestSetParams_doesNotMutateInput(t *testing.T) {
	obj, _ := New(ConfigObj{Name: "test.node", Type: "sv"})
	ni := map[string]any{"other": "data"}
	obj.SetParams(ni)
	if _, ok := ni["name"]; ok {
		t.Fatal("SetParams should not mutate input")
	}
}

// // // // // // // // // //
// Obj.ParseParams

func TestObjParseParams(t *testing.T) {
	obj, _ := New(ConfigObj{Name: "original", Type: "sv"})
	ni := map[string]any{
		"name":     "foreign",
		"type":     "router",
		"location": "US",
	}
	obj.ParseParams(ni)
	p := obj.Params()
	if p["name"] != "foreign" {
		t.Fatalf("expected foreign, got %v", p["name"])
	}
	if p["type"] != "router" {
		t.Fatalf("expected router, got %v", p["type"])
	}
	if p["location"] != "US" {
		t.Fatalf("expected US, got %v", p["location"])
	}
}

// // // // // // // // // //
// Params

func TestParams_emptyFields(t *testing.T) {
	obj := &Obj{conf: &ConfigObj{}}
	p := obj.Params()
	if len(p) != 0 {
		t.Fatalf("expected empty params, got %v", p)
	}
}

func TestParams_allFields(t *testing.T) {
	obj, _ := New(ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Location:    "EU",
		Description: "open policy",
		Contacts:    map[string][]string{"email": {"a@b.com"}},
	})
	p := obj.Params()
	if p["name"] != "test.node" {
		t.Fatal("missing name")
	}
	if p["type"] != "server" {
		t.Fatal("missing type")
	}
	if p["location"] != "EU" {
		t.Fatal("missing location")
	}
	if p["description"] != "open policy" {
		t.Fatal("missing peering")
	}
	if p["contact"] == nil {
		t.Fatal("missing contact")
	}
}

// // // // // // // // // //

func BenchmarkNew(b *testing.B) {
	conf := ConfigObj{
		Name:        "test.node",
		Type:        "server",
		Location:    "Moscow",
		Contacts:    map[string][]string{"email": {"admin@example.com"}},
		Description: "open peering",
	}
	for b.Loop() {
		New(conf)
	}
}

func BenchmarkMatch(b *testing.B) {
	ni := map[string]any{
		"name":        "test",
		"type":        "server",
		"location":    "EU",
		"contact":     map[string]any{"email": []any{"a@b.com"}},
		"description": "open",
	}
	for b.Loop() {
		Match(ni)
	}
}

func BenchmarkParse(b *testing.B) {
	ni := map[string]any{
		"name":     "test",
		"type":     "server",
		"location": "EU",
		"contact":  map[string]any{"email": []any{"a@b.com"}},
	}
	for b.Loop() {
		Parse(ni)
	}
}
