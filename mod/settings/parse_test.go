package settings_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	msettings "github.com/voluminor/ratatoskr/mod/settings"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

func TestParseFile_JSON(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	srcPath := filepath.Join(dir, "test.json")
	gsettings.SaveJSON(src, srcPath)

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(srcPath, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetFormat() != src.GetLog().GetFormat() {
		t.Fatalf("format mismatch: got %v, want %v", dst.GetLog().GetFormat(), src.GetLog().GetFormat())
	}
}

func TestParseFile_YAML(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	srcPath := filepath.Join(dir, "test.yaml")
	gsettings.SaveYAML(src, srcPath)

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(srcPath, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetMaxSize() != src.GetLog().GetMaxSize() {
		t.Fatalf("max_size mismatch: got %v, want %v", dst.GetLog().GetMaxSize(), src.GetLog().GetMaxSize())
	}
}

func TestParseFile_HJSON(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	srcPath := filepath.Join(dir, "test.conf")
	gsettings.SaveHJSON(src, srcPath)

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(srcPath, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetCompress() != src.GetLog().GetCompress() {
		t.Fatalf("compress mismatch: got %v, want %v", dst.GetLog().GetCompress(), src.GetLog().GetCompress())
	}
}

// //

func TestParseFile_UnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	os.WriteFile(path, []byte("x = 1"), 0644)

	if err := msettings.ParseFile(path, gsettings.NewDefault()); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestParseFile_NotFound(t *testing.T) {
	if err := msettings.ParseFile("/nonexistent/path.json", gsettings.NewDefault()); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// //

func TestParseFile_Chain_Simple(t *testing.T) {
	dir := t.TempDir()

	writeYAML(t, filepath.Join(dir, "c.yml"), map[string]any{
		"yggdrasil": map[string]any{
			"key":          map[string]any{"text": "cccc"},
			"admin_listen": "unix:///c.sock",
		},
	})
	writeYAML(t, filepath.Join(dir, "b.yml"), map[string]any{
		"config": "c.yml",
		"yggdrasil": map[string]any{
			"admin_listen": "unix:///b.sock",
		},
	})
	writeYAML(t, filepath.Join(dir, "a.yml"), map[string]any{
		"config": "b.yml",
		"yggdrasil": map[string]any{
			"admin_listen": "unix:///a.sock",
		},
	})

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(filepath.Join(dir, "a.yml"), dst); err != nil {
		t.Fatal(err)
	}
	if got := dst.GetYggdrasil().GetKey().GetText(); got != "cccc" {
		t.Fatalf("key.text: got %q, want %q", got, "cccc")
	}
	if got := dst.GetYggdrasil().GetAdminListen(); got != "unix:///c.sock" {
		t.Fatalf("admin_listen: got %q, want %q", got, "unix:///c.sock")
	}
}

func TestParseFile_Chain_IntermediateIgnored(t *testing.T) {
	dir := t.TempDir()

	writeYAML(t, filepath.Join(dir, "base.yml"), map[string]any{
		"yggdrasil": map[string]any{"admin_listen": "none"},
	})
	writeYAML(t, filepath.Join(dir, "redirect.yml"), map[string]any{
		"config": "base.yml",
		"yggdrasil": map[string]any{
			"admin_listen": "unix:///should-be-ignored.sock",
			"key":          map[string]any{"text": "deadbeef"},
		},
	})

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(filepath.Join(dir, "redirect.yml"), dst); err != nil {
		t.Fatal(err)
	}
	if got := dst.GetYggdrasil().GetAdminListen(); got != "none" {
		t.Fatalf("admin_listen: got %q, want %q", got, "none")
	}
	if got := dst.GetYggdrasil().GetKey().GetText(); got != "" {
		t.Fatalf("key.text: got %q, want empty", got)
	}
}

func TestParseFile_Chain_NoRedirect(t *testing.T) {
	dir := t.TempDir()

	writeYAML(t, filepath.Join(dir, "standalone.yml"), map[string]any{
		"yggdrasil": map[string]any{"admin_listen": "unix:///standalone.sock"},
	})

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(filepath.Join(dir, "standalone.yml"), dst); err != nil {
		t.Fatal(err)
	}
	if got := dst.GetYggdrasil().GetAdminListen(); got != "unix:///standalone.sock" {
		t.Fatalf("admin_listen: got %q, want %q", got, "unix:///standalone.sock")
	}
}

func TestParseFile_Chain_Circular(t *testing.T) {
	dir := t.TempDir()

	writeYAML(t, filepath.Join(dir, "x.yml"), map[string]any{"config": "y.yml"})
	writeYAML(t, filepath.Join(dir, "y.yml"), map[string]any{"config": "x.yml"})

	dst := gsettings.NewDefault()
	err := msettings.ParseFile(filepath.Join(dir, "x.yml"), dst)
	if err == nil {
		t.Fatal("expected circular reference error")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected 'circular' in error, got: %s", err)
	}
}

func TestParseFile_Chain_SelfRef(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "self.yml"), map[string]any{"config": "self.yml"})

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(filepath.Join(dir, "self.yml"), dst); err == nil {
		t.Fatal("expected circular reference error")
	}
}

func TestParseFile_Chain_ExceedsMax(t *testing.T) {
	dir := t.TempDir()

	for i := range 33 {
		var content map[string]any
		if i < 32 {
			content = map[string]any{"config": fmt.Sprintf("f%d.yml", i+1)}
		} else {
			content = map[string]any{"yggdrasil": map[string]any{"admin_listen": "none"}}
		}
		writeYAML(t, filepath.Join(dir, fmt.Sprintf("f%d.yml", i)), content)
	}

	dst := gsettings.NewDefault()
	err := msettings.ParseFile(filepath.Join(dir, "f0.yml"), dst)
	if err == nil {
		t.Fatal("expected chain limit error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected 'exceeds' in error, got: %s", err)
	}
}

func TestParseFile_Chain_BrokenLink(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "broken.yml"), map[string]any{"config": "nonexistent.yml"})

	if err := msettings.ParseFile(filepath.Join(dir, "broken.yml"), gsettings.NewDefault()); err == nil {
		t.Fatal("expected file not found error")
	}
}

func TestParseFile_Chain_ConfigCleared(t *testing.T) {
	dir := t.TempDir()

	writeYAML(t, filepath.Join(dir, "a.yml"), map[string]any{"config": "b.yml"})
	writeYAML(t, filepath.Join(dir, "b.yml"), map[string]any{
		"yggdrasil": map[string]any{"admin_listen": "none"},
	})

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(filepath.Join(dir, "a.yml"), dst); err != nil {
		t.Fatal(err)
	}
	if got := dst.GetConfig(); got != "" {
		t.Fatalf("config must be cleared after parsing, got %q", got)
	}
}

func TestParseFile_Chain_RelativePath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0755)

	writeYAML(t, filepath.Join(sub, "target.yml"), map[string]any{
		"yggdrasil": map[string]any{"admin_listen": "unix:///target.sock"},
	})
	writeYAML(t, filepath.Join(dir, "entry.yml"), map[string]any{
		"config": "sub/target.yml",
	})

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(filepath.Join(dir, "entry.yml"), dst); err != nil {
		t.Fatal(err)
	}
	if got := dst.GetYggdrasil().GetAdminListen(); got != "unix:///target.sock" {
		t.Fatalf("admin_listen: got %q, want %q", got, "unix:///target.sock")
	}
}

// //

func writeYAML(t *testing.T, path string, data map[string]any) {
	t.Helper()
	raw, err := yaml.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
}
