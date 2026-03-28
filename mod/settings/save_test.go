package settings_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	msettings "github.com/voluminor/ratatoskr/mod/settings"
	"github.com/voluminor/ratatoskr/target"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

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

func TestStripRootKey_PreservesNested(t *testing.T) {
	input := "{\n  \"config\": \"root\",\n  \"nested\": {\n    \"config\": \"keep-me\"\n  }\n}"
	got := string(msettings.StripRootKey([]byte(input), "config"))
	if !strings.Contains(got, "keep-me") {
		t.Fatal("StripRootKey must preserve nested 'config' keys")
	}
	if strings.Contains(got, "root") {
		t.Fatal("StripRootKey must strip root-level 'config'")
	}
}

func TestStripRootKey_YAML_PreservesNested(t *testing.T) {
	input := "config: root-value\nnested:\n  config: keep-me\n"
	got := string(msettings.StripRootKey([]byte(input), "config"))
	if !strings.Contains(got, "keep-me") {
		t.Fatal("StripRootKey must preserve nested YAML 'config' keys")
	}
	if strings.Contains(got, "root-value") {
		t.Fatal("StripRootKey must strip root-level YAML 'config'")
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
		{gsettings.GoConfExportFormatJson, target.GlobalName + ".json"},
		{gsettings.GoConfExportFormatYml, target.GlobalName + ".yml"},
		{gsettings.GoConfExportFormatConf, target.GlobalName + ".conf"},
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
	want := "/tmp/out/" + target.GlobalName + ".json"
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
