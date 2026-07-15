package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// // // // // // // // // //

func TestExtractSigName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.go")
	if err := os.WriteFile(path, []byte("package example\n\nconst sigName = \"service\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := extractSigName(path)
	if err != nil {
		t.Fatal(err)
	}
	if name != "service" {
		t.Fatalf("name = %q, want service", name)
	}
}

func TestExtractSigNameRejectsNonConstant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.go")
	if err := os.WriteFile(path, []byte("package example\n\nvar sigName = \"service\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractSigName(path); err == nil {
		t.Fatal("variable sigName accepted")
	}
}

func TestCheckDuplicates(t *testing.T) {
	err := checkDuplicates([]sigilObj{
		{Alias: "first", Name: "service"},
		{Alias: "second", Name: "service"},
	})
	if err == nil {
		t.Fatal("duplicate sigName accepted")
	}
	if !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("duplicate error lacks both folders: %v", err)
	}
}
