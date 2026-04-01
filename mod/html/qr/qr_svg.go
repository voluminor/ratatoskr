package qr

import (
	"fmt"
	"strings"
)

// // // // // // // // // //

const (
	quietZone    = 4 // mandatory 4-module quiet zone (ISO 18004)
	modulePixels = 8 // rendered pixel size per module; 8px gives ~200–500px total for v1–10
)

// renderSVG converts a QR matrix to an SVG string.
//
// Dimensions: explicit width/height ensure browsers render at a scannable size
// rather than collapsing to the raw viewBox pixel count (~29–65px).
//
// Color protection:
//   - color-scheme:light prevents dark-mode inversion in browsers
//   - forced-color-adjust:none blocks Windows High Contrast overrides
//   - inline !important beats any external stylesheet
//
// All dark modules are encoded as a single <path> for compact output.
func renderSVG(m [][]bool) string {
	size := len(m)
	total := size + 2*quietZone
	px := total * modulePixels

	var path strings.Builder
	for r, row := range m {
		for c, dark := range row {
			if dark {
				fmt.Fprintf(&path, "M%d,%dh1v1h-1z", c+quietZone, r+quietZone)
			}
		}
	}

	var sb strings.Builder
	// shape-rendering="crispEdges" disables anti-aliasing on module boundaries,
	// which is critical for QR scanner readability.
	fmt.Fprintf(&sb,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" `+
			`width="%d" height="%d" `+
			`shape-rendering="crispEdges" `+
			`style="color-scheme:light;forced-color-adjust:none">`,
		total, total, px, px,
	)
	fmt.Fprintf(&sb,
		`<rect width="%d" height="%d" fill="#ffffff" style="fill:#ffffff!important"/>`,
		total, total,
	)
	fmt.Fprintf(&sb,
		`<path fill="#000000" style="fill:#000000!important" d="%s"/>`,
		path.String(),
	)
	sb.WriteString(`</svg>`)

	return sb.String()
}
