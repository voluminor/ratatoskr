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

func TestSaveFile_JSON(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFile(src, dir, gsettings.GoConfExportFormatJson)
	if err != nil {
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

	path, err := msettings.SaveFile(src, dir, gsettings.GoConfExportFormatYml)
	if err != nil {
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

	path, err := msettings.SaveFile(src, dir, gsettings.GoConfExportFormatConf)
	if err != nil {
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

func TestSaveFile_ExcludesConfig(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	src.Config = "should-be-stripped"

	path, err := msettings.SaveFile(src, dir, gsettings.GoConfExportFormatJson)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "should-be-stripped") {
		t.Fatal("config field must be stripped from output")
	}
}

func TestSaveFile_UnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := msettings.SaveFile(gsettings.NewDefault(), dir, 99)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestSaveFile_GeneratesCorrectFilename(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		format gsettings.GoConfExportFormatEnum
		want   string
	}{
		{gsettings.GoConfExportFormatJson, msettings.ConfigBaseName + ".json"},
		{gsettings.GoConfExportFormatYml, msettings.ConfigBaseName + ".yml"},
		{gsettings.GoConfExportFormatConf, msettings.ConfigBaseName + ".conf"},
	}
	for _, c := range cases {
		sub := filepath.Join(dir, c.want)
		os.MkdirAll(filepath.Dir(sub), 0755)

		path, err := msettings.SaveFile(gsettings.NewDefault(), dir, c.format)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != c.want {
			t.Errorf("format %v: got filename %q, want %q", c.format, filepath.Base(path), c.want)
		}
	}
}

func TestSaveFile_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	_, err := msettings.SaveFile(gsettings.NewDefault(), dir, gsettings.GoConfExportFormatYml)
	if err != nil {
		t.Fatal(err)
	}
}

// //

func TestSaveFilePretty_YAML_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatYml)
	if err != nil {
		t.Fatal(err)
	}

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetOutput() != src.GetLog().GetOutput() {
		t.Fatalf("output mismatch: got %v, want %v", dst.GetLog().GetOutput(), src.GetLog().GetOutput())
	}
	if dst.GetYggdrasil().GetIf().GetMtu() != src.GetYggdrasil().GetIf().GetMtu() {
		t.Fatalf("mtu mismatch: got %v, want %v", dst.GetYggdrasil().GetIf().GetMtu(), src.GetYggdrasil().GetIf().GetMtu())
	}
}

func TestSaveFilePretty_JSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatJson)
	if err != nil {
		t.Fatal(err)
	}

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetLog().GetLevel().GetFile() != src.GetLog().GetLevel().GetFile() {
		t.Fatalf("level.file mismatch: got %v, want %v", dst.GetLog().GetLevel().GetFile(), src.GetLog().GetLevel().GetFile())
	}
}

func TestSaveFilePretty_HJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatConf)
	if err != nil {
		t.Fatal(err)
	}

	dst := gsettings.NewDefault()
	if err := msettings.ParseFile(path, dst); err != nil {
		t.Fatal(err)
	}
	if dst.GetYggdrasil().GetAdminListen() != src.GetYggdrasil().GetAdminListen() {
		t.Fatalf("admin_listen mismatch: got %v, want %v", dst.GetYggdrasil().GetAdminListen(), src.GetYggdrasil().GetAdminListen())
	}
}

// //

func TestSaveFilePretty_YAML_HasComments(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatYml)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	for _, expected := range []string{
		"Yggdrasil network node configuration",
		"logging configuration",
		"TUN adapter",
		"console log level",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("YAML output missing comment: %q", expected)
		}
	}
}

func TestSaveFilePretty_HJSON_HasComments(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatConf)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	for _, expected := range []string{
		"# Yggdrasil network node configuration",
		"# logging configuration",
		"# TUN adapter",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("HJSON output missing comment: %q", expected)
		}
	}
}

// //

func TestSaveFilePretty_YAML_FieldOrder(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatYml)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	yggIdx := strings.Index(content, "yggdrasil:")
	logIdx := strings.Index(content, "log:")
	if yggIdx < 0 || logIdx < 0 {
		t.Fatal("missing yggdrasil or log section")
	}
	if yggIdx >= logIdx {
		t.Fatal("yggdrasil must appear before log in YAML output")
	}

	keyIdx := strings.Index(content, "  key:")
	listenIdx := strings.Index(content, "  listen:")
	peersIdx := strings.Index(content, "  peers:")
	if keyIdx < 0 || listenIdx < 0 || peersIdx < 0 {
		t.Fatal("missing key, listen, or peers in yggdrasil section")
	}
	if !(keyIdx < listenIdx && listenIdx < peersIdx) {
		t.Fatal("yggdrasil children must follow settings.yml order: key < listen < peers")
	}
}

func TestSaveFilePretty_HJSON_FieldOrder(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatConf)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	yggIdx := strings.Index(content, "yggdrasil:")
	logIdx := strings.Index(content, "log:")
	if yggIdx < 0 || logIdx < 0 {
		t.Fatal("missing yggdrasil or log section")
	}
	if yggIdx >= logIdx {
		t.Fatal("yggdrasil must appear before log in HJSON output")
	}
}

func TestSaveFilePretty_JSON_FieldOrder(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatJson)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	yggIdx := strings.Index(content, `"yggdrasil"`)
	logIdx := strings.Index(content, `"log"`)
	if yggIdx < 0 || logIdx < 0 {
		t.Fatal("missing yggdrasil or log key in JSON")
	}
	if yggIdx >= logIdx {
		t.Fatal("yggdrasil must appear before log in JSON output")
	}
}

// //

func TestSaveFilePretty_ExcludesConfig(t *testing.T) {
	dir := t.TempDir()
	src := gsettings.NewDefault()
	src.Config = "should-be-stripped"

	path, err := msettings.SaveFilePretty(src, dir, gsettings.GoConfExportFormatYml)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "should-be-stripped") {
		t.Fatal("config field must be stripped from pretty output")
	}
}

// //

func TestSaveUnsafePretty_MapData_YAML(t *testing.T) {
	dir := t.TempDir()
	data := map[string]any{
		"yggdrasil": map[string]any{
			"key": map[string]any{"text": "deadbeef"},
		},
	}

	path, err := msettings.SaveUnsafePretty(data, dir, gsettings.GoConfExportFormatYml)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	if !strings.Contains(content, "deadbeef") {
		t.Fatal("YAML output missing key text value")
	}
	if !strings.Contains(content, "Yggdrasil network node configuration") {
		t.Fatal("YAML output missing yggdrasil comment")
	}
}

func TestSaveUnsafePretty_MapData_JSON(t *testing.T) {
	dir := t.TempDir()
	data := map[string]any{
		"yggdrasil": map[string]any{
			"key": map[string]any{"text": "deadbeef"},
		},
	}

	path, err := msettings.SaveUnsafePretty(data, dir, gsettings.GoConfExportFormatJson)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "deadbeef") {
		t.Fatal("JSON output missing key text value")
	}
}

func TestSaveUnsafePretty_MapData_HJSON(t *testing.T) {
	dir := t.TempDir()
	data := map[string]any{
		"yggdrasil": map[string]any{
			"key": map[string]any{"text": "deadbeef"},
		},
	}

	path, err := msettings.SaveUnsafePretty(data, dir, gsettings.GoConfExportFormatConf)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	content := string(raw)

	if !strings.Contains(content, "deadbeef") {
		t.Fatal("HJSON output missing key text value")
	}
	if !strings.Contains(content, "# Yggdrasil network node configuration") {
		t.Fatal("HJSON output missing yggdrasil comment")
	}
}

// //

func TestSaveUnsafePretty_UnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := msettings.SaveUnsafePretty("data", dir, 99)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// //

func TestFormatExt(t *testing.T) {
	cases := []struct {
		format gsettings.GoConfExportFormatEnum
		want   string
	}{
		{gsettings.GoConfExportFormatJson, ".json"},
		{gsettings.GoConfExportFormatYml, ".yml"},
		{gsettings.GoConfExportFormatConf, ".conf"},
	}
	for _, c := range cases {
		if got := msettings.FormatExt(c.format); got != c.want {
			t.Errorf("FormatExt(%v) = %q, want %q", c.format, got, c.want)
		}
	}
}

func TestConfigPath(t *testing.T) {
	got := msettings.ConfigPath("/tmp/out", gsettings.GoConfExportFormatJson)
	want := "/tmp/out/" + msettings.ConfigBaseName + ".json"
	if got != want {
		t.Fatalf("ConfigPath: got %q, want %q", got, want)
	}
}

func TestValidateDir_CreatesNested(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	abs, err := msettings.ValidateDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		t.Fatal("directory not created")
	}
}

func TestValidateDir_Empty(t *testing.T) {
	_, err := msettings.ValidateDir("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestValidateDir_FileNotDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(f, []byte("x"), 0644)
	_, err := msettings.ValidateDir(f)
	if err == nil {
		t.Fatal("expected error for non-directory path")
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
