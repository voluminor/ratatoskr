package settings_test

import (
	"os"
	"path/filepath"
	"testing"

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

func TestSaveFile_JSON(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	path := filepath.Join(dir, "out.json")

	if err := msettings.SaveFile(src, path); err != nil {
		t.Fatal(err)
	}

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetOutput() != src.GetLog().GetOutput() {
		t.Fatalf("output mismatch: got %v, want %v", dst.GetLog().GetOutput(), src.GetLog().GetOutput())
	}
}

func TestSaveFile_YAML(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	path := filepath.Join(dir, "out.yml")

	if err := msettings.SaveFile(src, path); err != nil {
		t.Fatal(err)
	}

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetMaxAge() != src.GetLog().GetMaxAge() {
		t.Fatalf("max_age mismatch: got %v, want %v", dst.GetLog().GetMaxAge(), src.GetLog().GetMaxAge())
	}
}

func TestSaveFile_HJSON(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	path := filepath.Join(dir, "out.hjson")

	if err := msettings.SaveFile(src, path); err != nil {
		t.Fatal(err)
	}

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetLevel().GetConsole() != src.GetLog().GetLevel().GetConsole() {
		t.Fatalf("level.console mismatch: got %v, want %v", dst.GetLog().GetLevel().GetConsole(), src.GetLog().GetLevel().GetConsole())
	}
}

// //

func TestParseFile_UnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	os.WriteFile(path, []byte("x = 1"), 0644)

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestSaveFile_UnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.toml")

	if err := msettings.SaveFile(gsettings.NewDefault(), path); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestParseFile_NotFound(t *testing.T) {
	dst := gsettings.NewDefault()
	if err := msettings.ParseFile("/nonexistent/path.json", dst); err == nil {
		t.Fatal("expected error for missing file")
	}
}
