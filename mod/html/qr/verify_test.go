package qr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExternalDecode generates QR SVGs, converts to PNG via cairosvg,
// and decodes via pyzbar. This catches any deviation from the QR standard
// that the internal round-trip test would miss.
func TestExternalDecode(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}

	cases := []string{
		"https://example.com",
		"http://[200:abcd::1]:8080/",
		"http://[200:dead:beef:1234:5678:9abc:def0:1234]:8443/",
		"HELLO WORLD",
		"1234567890",
	}

	dir := t.TempDir()

	for i, input := range cases {
		svg, err := QR(input)
		if err != nil {
			t.Fatalf("QR(%q): %v", input, err)
		}

		svgPath := filepath.Join(dir, "qr.svg")
		pngPath := filepath.Join(dir, "qr.png")
		if err := os.WriteFile(svgPath, []byte(svg), 0o644); err != nil {
			t.Fatal(err)
		}

		script := `
import sys, cairosvg
from PIL import Image
from pyzbar.pyzbar import decode

cairosvg.svg2png(url=sys.argv[1], write_to=sys.argv[2], output_width=600, output_height=600)
img = Image.open(sys.argv[2])
results = decode(img)
if not results:
    print("DECODE_FAIL", file=sys.stderr)
    sys.exit(1)
print(results[0].data.decode("utf-8"))
`
		cmd := exec.Command("python3", "-c", script, svgPath, pngPath)
		out, err := cmd.Output()
		decoded := strings.TrimSpace(string(out))

		if err != nil {
			stderr := ""
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = string(ee.Stderr)
			}
			t.Errorf("case %d %q: decode failed: %v\nstderr: %s", i, input, err, stderr)
			// Save failing SVG for inspection
			failPath := filepath.Join(dir, "fail.svg")
			_ = os.WriteFile(failPath, []byte(svg), 0o644)
			t.Logf("failing SVG saved to %s", failPath)
			continue
		}

		if decoded != input {
			t.Errorf("case %d: decoded=%q, want=%q", i, decoded, input)
		}
	}
}
