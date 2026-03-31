package sigil_core

import (
	"testing"

	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //
// New

func TestNew_nil_nodeInfo(t *testing.T) {
	obj, errs := New(nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.NodeInfo() == nil {
		t.Fatal("expected non-nil NodeInfo")
	}
}

func TestNew_with_nodeInfo(t *testing.T) {
	base := map[string]any{"custom": "value"}
	obj, errs := New(base)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.NodeInfo()["custom"] != "value" {
		t.Fatal("expected custom key in NodeInfo")
	}
}

func TestNew_with_sigils(t *testing.T) {
	obj, errs := New(nil, newMockSigil("aaa", "key1"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.Get("aaa") == nil {
		t.Fatal("sigil not found after New")
	}
	if obj.NodeInfo()["key1"] != "test" {
		t.Fatal("sigil data not in NodeInfo")
	}
}

func TestNew_invalid_sigil_skipped(t *testing.T) {
	obj, errs := New(nil, newMockSigil("AB")) // too short + uppercase
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if obj.Get("AB") != nil {
		t.Fatal("invalid sigil should not be stored")
	}
}

// // // // // // // // // //
// Add

func TestAdd_valid(t *testing.T) {
	obj, _ := New(nil)
	errs := obj.Add(newMockSigil("test-sigil", "key1"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.Get("test-sigil") == nil {
		t.Fatal("sigil not found after Add")
	}
}

func TestAdd_duplicate(t *testing.T) {
	obj, _ := New(nil)
	obj.Add(newMockSigil("test-sigil"))
	errs := obj.Add(newMockSigil("test-sigil"))
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestAdd_updates_metadata(t *testing.T) {
	obj, _ := New(nil)
	obj.Add(newMockSigil("aaa"), newMockSigil("bbb"))
	expected := "[aaa,bbb] " + target.GlobalVersion
	got := obj.NodeInfo()[target.GlobalName]
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

// // // // // // // // // //
// Get

func TestGet_notFound(t *testing.T) {
	obj, _ := New(nil)
	if obj.Get("nonexistent") != nil {
		t.Fatal("expected nil for missing sigil")
	}
}

// // // // // // // // // //
// Del

func TestDel_valid(t *testing.T) {
	obj, _ := New(nil)
	obj.Add(newMockSigil("test-sigil", "key1"))
	if err := obj.Del("test-sigil"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.Get("test-sigil") != nil {
		t.Fatal("sigil should be removed")
	}
	if _, ok := obj.NodeInfo()["key1"]; ok {
		t.Fatal("sigil keys should be removed from NodeInfo")
	}
}

func TestDel_notFound(t *testing.T) {
	obj, _ := New(nil)
	if err := obj.Del("missing"); err == nil {
		t.Fatal("expected error for missing sigil")
	}
}

// // // // // // // // // //
// Accessors

func TestSigils_map(t *testing.T) {
	obj, _ := New(nil)
	obj.Add(newMockSigil("aaa"), newMockSigil("bbb"))
	if len(obj.Sigils()) != 2 {
		t.Fatalf("expected 2 sigils, got %d", len(obj.Sigils()))
	}
}

func TestLenSigils(t *testing.T) {
	obj, _ := New(nil)
	obj.Add(newMockSigil("aaa"))
	if obj.LenSigils() != 1 {
		t.Fatalf("expected 1, got %d", obj.LenSigils())
	}
}

func TestLenLocal(t *testing.T) {
	obj, _ := New(map[string]any{"x": 1})
	obj.Add(newMockSigil("aaa", "key1"))
	// x + key1 + ratatoskr metadata = 3
	if obj.LenLocal() != 3 {
		t.Fatalf("expected 3, got %d", obj.LenLocal())
	}
}

func TestLen(t *testing.T) {
	obj, _ := New(nil)
	obj.Add(newMockSigil("aaa", "key1"))
	// sigils: 1, localNodeInfo: key1 + ratatoskr = 2; total = 3
	if obj.Len() != 3 {
		t.Fatalf("expected 3, got %d", obj.Len())
	}
}

func TestString(t *testing.T) {
	obj, _ := New(nil, newMockSigil("aaa"))
	s := obj.String()
	if s == "" {
		t.Fatal("expected non-empty string")
	}
}
