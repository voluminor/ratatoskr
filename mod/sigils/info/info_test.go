package info

import (
	"reflect"
	"strings"
	"testing"
)

// // // // // // // // // //

func validConfig() ConfigObj {
	return ConfigObj{Name: "node.example", Type: "server"}
}

func TestNewValidation(t *testing.T) {
	maxGroups := make(map[string][]string, maxContactGroups)
	for i := range maxContactGroups {
		maxGroups["group"+string(rune('a'+i))] = []string{"contact-value"}
	}
	tooManyGroups := cloneContacts(maxGroups)
	tooManyGroups["extra"] = []string{"contact-value"}
	maxContacts := make([]string, maxContactsPerGroup)
	for i := range maxContacts {
		maxContacts[i] = "contact-" + strings.Repeat("a", i+1)
	}
	tests := []struct {
		name   string
		mutate func(*ConfigObj)
		valid  bool
	}{
		{name: "minimal", valid: true},
		{name: "full", mutate: func(c *ConfigObj) {
			c.Location = "Warsaw, Poland"
			c.Description = "Public relay node"
			c.Contacts = map[string][]string{"email": {"admin@example.com"}}
		}, valid: true},
		{name: "name boundaries", mutate: func(c *ConfigObj) { c.Name = strings.Repeat("a", 4) }, valid: true},
		{name: "name maximum", mutate: func(c *ConfigObj) { c.Name = strings.Repeat("a", 64) }, valid: true},
		{name: "type boundaries", mutate: func(c *ConfigObj) { c.Type = "ab" }, valid: true},
		{name: "type maximum", mutate: func(c *ConfigObj) { c.Type = strings.Repeat("a", 32) }, valid: true},
		{name: "maximum contact groups", mutate: func(c *ConfigObj) { c.Contacts = maxGroups }, valid: true},
		{name: "maximum contacts", mutate: func(c *ConfigObj) { c.Contacts = map[string][]string{"email": maxContacts} }, valid: true},
		{name: "missing name", mutate: func(c *ConfigObj) { c.Name = "" }},
		{name: "short name", mutate: func(c *ConfigObj) { c.Name = "abc" }},
		{name: "long name", mutate: func(c *ConfigObj) { c.Name = strings.Repeat("a", 65) }},
		{name: "uppercase name", mutate: func(c *ConfigObj) { c.Name = "Node.example" }},
		{name: "space in name", mutate: func(c *ConfigObj) { c.Name = "node example" }},
		{name: "missing type", mutate: func(c *ConfigObj) { c.Type = "" }},
		{name: "short type", mutate: func(c *ConfigObj) { c.Type = "a" }},
		{name: "long type", mutate: func(c *ConfigObj) { c.Type = strings.Repeat("a", 33) }},
		{name: "uppercase type", mutate: func(c *ConfigObj) { c.Type = "Server" }},
		{name: "short location", mutate: func(c *ConfigObj) { c.Location = "x" }},
		{name: "long location", mutate: func(c *ConfigObj) { c.Location = strings.Repeat("x", 515) }},
		{name: "leading location space", mutate: func(c *ConfigObj) { c.Location = " Warsaw" }},
		{name: "trailing description space", mutate: func(c *ConfigObj) { c.Description = "public " }},
		{name: "control description", mutate: func(c *ConfigObj) { c.Description = "bad\nvalue" }},
		{name: "format location", mutate: func(c *ConfigObj) { c.Location = "bad\u200bvalue" }},
		{name: "too many contact groups", mutate: func(c *ConfigObj) { c.Contacts = tooManyGroups }},
		{name: "empty contact group", mutate: func(c *ConfigObj) { c.Contacts = map[string][]string{"email": {}} }},
		{name: "too many contacts", mutate: func(c *ConfigObj) {
			c.Contacts = map[string][]string{"email": append(maxContacts, "extra-contact")}
		}},
		{name: "invalid contact group", mutate: func(c *ConfigObj) { c.Contacts = map[string][]string{"E": {"contact-value"}} }},
		{name: "short contact", mutate: func(c *ConfigObj) { c.Contacts = map[string][]string{"email": {"ab"}} }},
		{name: "control contact", mutate: func(c *ConfigObj) { c.Contacts = map[string][]string{"email": {"bad\nvalue"}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			if test.mutate != nil {
				test.mutate(&config)
			}
			obj, err := New(config)
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
	validFull := map[string]any{
		"name":        "node.example",
		"type":        "server",
		"location":    "Warsaw, Poland",
		"description": "Public relay node",
		"contact":     map[string]any{"email": []any{"admin@example.com"}},
	}
	tests := []struct {
		name     string
		nodeInfo map[string]any
		match    bool
	}{
		{name: "minimal", nodeInfo: map[string]any{"name": "node.example", "type": "server"}, match: true},
		{name: "full JSON shape", nodeInfo: validFull, match: true},
		{name: "typed contacts", nodeInfo: map[string]any{"name": "node.example", "type": "server", "contact": map[string][]string{"email": {"admin@example.com"}}}, match: true},
		{name: "missing name", nodeInfo: map[string]any{"type": "server"}},
		{name: "missing type", nodeInfo: map[string]any{"name": "node.example"}},
		{name: "name not string", nodeInfo: map[string]any{"name": 1.0, "type": "server"}},
		{name: "type not string", nodeInfo: map[string]any{"name": "node.example", "type": nil}},
		{name: "invalid name", nodeInfo: map[string]any{"name": "Node.example", "type": "server"}},
		{name: "location not string", nodeInfo: map[string]any{"name": "node.example", "type": "server", "location": 1.0}},
		{name: "invalid location", nodeInfo: map[string]any{"name": "node.example", "type": "server", "location": "x"}},
		{name: "description not string", nodeInfo: map[string]any{"name": "node.example", "type": "server", "description": []any{}}},
		{name: "contact wrong type", nodeInfo: map[string]any{"name": "node.example", "type": "server", "contact": []any{}}},
		{name: "contact value not array", nodeInfo: map[string]any{"name": "node.example", "type": "server", "contact": map[string]any{"email": "admin@example.com"}}},
		{name: "contact element not string", nodeInfo: map[string]any{"name": "node.example", "type": "server", "contact": map[string]any{"email": []any{1.0}}}},
		{name: "empty contact group", nodeInfo: map[string]any{"name": "node.example", "type": "server", "contact": map[string]any{"email": []any{}}}},
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
	nodeInfo := map[string]any{
		"name":    "node.example",
		"type":    "server",
		"contact": map[string]any{"email": []any{"admin@example.com"}},
		"other":   true,
	}
	obj, err := Parse(nodeInfo)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := &ConfigObj{Name: "node.example", Type: "server", Contacts: map[string][]string{"email": {"admin@example.com"}}}
	if got := obj.Info(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Info() = %#v, want %#v", got, want)
	}
	if _, err := Parse(map[string]any{}); err == nil {
		t.Fatal("Parse accepted missing info data")
	}
	if _, err := Parse(map[string]any{"name": "Node.example", "type": "server"}); err == nil {
		t.Fatal("Parse accepted invalid name")
	}

	current, err := New(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	parsed := current.ParseParams(nodeInfo)
	if _, exists := parsed["other"]; exists {
		t.Fatalf("ParseParams retained unrelated key: %#v", parsed)
	}
	if got := current.Info(); !reflect.DeepEqual(got, want) {
		t.Fatalf("object was not updated: %#v", got)
	}
	current.ParseParams(map[string]any{"name": "Node.example", "type": "server"})
	if got := current.Info(); !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid update changed object: %#v", got)
	}
}

func TestOwnershipAndMerge(t *testing.T) {
	input := validConfig()
	input.Contacts = map[string][]string{"email": {"admin@example.com"}}
	obj, err := New(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Contacts["email"][0] = "changed@example.com"
	info := obj.Info()
	info.Name = "changed.example"
	info.Contacts["email"][0] = "changed@example.com"
	params := obj.Params()
	params["contact"].(map[string][]string)["email"][0] = "changed@example.com"
	clone := obj.Clone().(*Obj)
	clone.conf.Contacts["email"][0] = "changed@example.com"
	if got := obj.Info().Contacts["email"][0]; got != "admin@example.com" {
		t.Fatalf("mutable alias changed object: %q", got)
	}

	base := map[string]any{"other": "value"}
	merged, err := obj.SetParams(base)
	if err != nil {
		t.Fatalf("SetParams: %v", err)
	}
	if _, exists := base["name"]; exists {
		t.Fatal("SetParams mutated input")
	}
	if got := merged["name"]; got != "node.example" {
		t.Fatalf("merged name = %#v", got)
	}
	if _, err := obj.SetParams(map[string]any{"name": "occupied"}); err == nil {
		t.Fatal("SetParams accepted a key conflict")
	}
	if obj.GetName() != Name() || !reflect.DeepEqual(obj.GetParams(), Keys()) {
		t.Fatal("interface identity does not match package identity")
	}

	var zero Obj
	if len(zero.Params()) != 0 || zero.Info() != nil {
		t.Fatal("zero-value object exposed data")
	}
}
