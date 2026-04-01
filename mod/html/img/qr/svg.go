package qr

import (
	"fmt"
	"strings"
)

// // // // // // // // // //

const (
	QuietZone    = 4 // mandatory 4-module quiet zone (ISO 18004)
	ModulePixels = 8 // rendered pixel size per module; 8px gives ~200–500px total for v1–10
)

// RenderSVG converts a QR matrix to SVG bytes.
//
// Each dark module is a separate <rect> for maximum compatibility
// across mobile browsers and WebView engines.
// shape-rendering="crispEdges" disables anti-aliasing on module boundaries.
// Color protection prevents dark-mode inversion.
func RenderSVG(m [][]bool) []byte {
	size := len(m)
	total := size + 2*QuietZone
	px := total * ModulePixels

	var sb strings.Builder
	sb.Grow(size * size * 70)

	fmt.Fprintf(&sb,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" `+
			`width="%d" height="%d" `+
			`shape-rendering="crispEdges" `+
			`style="color-scheme:light;forced-color-adjust:none">`,
		total, total, px, px,
	)
	fmt.Fprintf(&sb,
		`<rect width="%d" height="%d" fill="#fff" style="fill:#fff!important"/>`,
		total, total,
	)
	for r, row := range m {
		for c, dark := range row {
			if dark {
				fmt.Fprintf(&sb,
					`<rect x="%d" y="%d" width="1" height="1" fill="#000" style="fill:#000!important"/>`,
					c+QuietZone, r+QuietZone,
				)
			}
		}
	}
	sb.WriteString(`</svg>`)

	return []byte(sb.String())
}
