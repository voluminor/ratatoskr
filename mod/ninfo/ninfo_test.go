package ninfo

import (
	"testing"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/sigil_core"
)

// // // // // // // // // //

func newTestObj() *Obj {
	return &Obj{
		sigils: make(map[string]sigils.Interface),
	}
}

// // // // // // // // // //
// AddSigil / GetSigil / DelSigil

func TestAddSigil_valid(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(newMockSigil("test-sigil", "key1"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.GetSigil("test-sigil") == nil {
		t.Fatal("sigil not found after add")
	}
}

func TestAddSigil_duplicate(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("test-sigil"))
	errs := obj.AddSigil(newMockSigil("test-sigil"))
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestAddSigil_invalidName(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(newMockSigil("AB"))
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if obj.GetSigil("AB") != nil {
		t.Fatal("invalid sigil should not be stored")
	}
}

func TestAddSigil_multiple(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(
		newMockSigil("aaa"),
		newMockSigil("bbb"),
		newMockSigil("ccc"),
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(obj.sigils) != 3 {
		t.Fatalf("expected 3 sigils, got %d", len(obj.sigils))
	}
}

// //

func TestGetSigil_notFound(t *testing.T) {
	obj := newTestObj()
	if obj.GetSigil("nonexistent") != nil {
		t.Fatal("expected nil for missing sigil")
	}
}

// //

func TestDelSigil_valid(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("test-sigil"))
	if err := obj.DelSigil("test-sigil"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetSigil("test-sigil") != nil {
		t.Fatal("sigil should be removed")
	}
}

func TestDelSigil_notFound(t *testing.T) {
	obj := newTestObj()
	if err := obj.DelSigil("missing"); err == nil {
		t.Fatal("expected error for missing sigil")
	}
}

// // // // // // // // // //
// ImportSigils

func TestImportSigils_append(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("existing"))

	src, _ := sigil_core.New(nil, newMockSigil("new-one"))
	errs := obj.ImportSigils(src, ImportAppend)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.GetSigil("new-one") == nil {
		t.Fatal("imported sigil not found")
	}
	if obj.GetSigil("existing") == nil {
		t.Fatal("existing sigil should be preserved")
	}
}

func TestImportSigils_append_conflict(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("shared"))

	src, _ := sigil_core.New(nil, newMockSigil("shared"))
	errs := obj.ImportSigils(src, ImportAppend)
	if len(errs) != 1 {
		t.Fatalf("expected 1 conflict error, got %d", len(errs))
	}
}

func TestImportSigils_replace(t *testing.T) {
	obj := newTestObj()
	old := newMockSigil("shared", "old-key")
	obj.AddSigil(old)

	replacement := newMockSigil("shared", "new-key")
	src, _ := sigil_core.New(nil, replacement)
	errs := obj.ImportSigils(src, ImportReplace)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	got := obj.GetSigil("shared")
	if got != replacement {
		t.Fatal("sigil should be replaced")
	}
}

func TestImportSigils_reset(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("old-one"))

	src, _ := sigil_core.New(nil, newMockSigil("new-one"))
	errs := obj.ImportSigils(src, ImportReset)
	if errs != nil {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.GetSigil("old-one") != nil {
		t.Fatal("old sigil should be cleared")
	}
	if obj.GetSigil("new-one") == nil {
		t.Fatal("new sigil should be present")
	}
}

// // // // // // // // // //
// sigilSlice

func TestSigilSlice_empty(t *testing.T) {
	obj := newTestObj()
	if obj.sigilSlice() != nil {
		t.Fatal("expected nil for empty sigils")
	}
}

func TestSigilSlice_populated(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("aaa"), newMockSigil("bbb"))
	sl := obj.sigilSlice()
	if len(sl) != 2 {
		t.Fatalf("expected 2, got %d", len(sl))
	}
}
