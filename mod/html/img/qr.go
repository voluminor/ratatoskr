// Package img provides embedded images and QR code generation for Yggdrasil HTML pages.
package img

import (
	"crypto/ed25519"
	"fmt"
	"net"
	"strings"

	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"

	"github.com/voluminor/ratatoskr/mod/html/img/qr"
)

// // // // // // // // // //

// QRCode derives the Yggdrasil IPv6 address from key and returns an SVG QR code
// encoding http://[address]/ with a yggdrasil-leaf overlay in the center.
func QRCode(key ed25519.PublicKey) ([]byte, error) {
	addr := yggaddr.AddrForKey(key)
	ip := net.IP(addr[:])
	url := fmt.Sprintf("http://[%s]:80/", ip.String())

	matrix, err := qr.Matrix(url)
	if err != nil {
		return nil, err
	}
	return renderOverlay(matrix, leafSVG), nil
}

// // // // // // // // // //

// renderOverlay renders the QR matrix as SVG with a centered leaf icon on top.
// The leaf is drawn over the QR modules (not cut out); its white stroke
// provides enough contrast. EC level Q (25% recovery) handles the occlusion.
func renderOverlay(m [][]bool, overlay string) []byte {
	size := len(m)
	total := size + 2*qr.QuietZone
	px := total * qr.ModulePixels

	var sb strings.Builder
	sb.Grow(size*size*70 + len(overlay) + 512)

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
					c+qr.QuietZone, r+qr.QuietZone,
				)
			}
		}
	}

	// yggdrasil-leaf.svg viewBox is "30 0 200 260"; aspect ~0.77:1 (width:height).
	// The leaf SVG has a white stroke outline that clears underlying modules.
	center := float64(total) / 2
	leafR := 0.24 * float64(size)
	leafW := leafR * 1.3
	leafH := leafW * 1.3 // preserve viewBox aspect ratio 200:260
	leafX := center - leafW/2
	leafY := center - leafH/2
	fmt.Fprintf(&sb,
		`<svg x="%.1f" y="%.1f" width="%.1f" height="%.1f" viewBox="30 0 200 260" shape-rendering="geometricPrecision">`,
		leafX, leafY, leafW, leafH,
	)
	// Visible white border = 0.5 QR module.
	// SVG stroke is centered on the path: half goes outward (visible), half inward
	// (covered by the green fill layer). So stroke-width = 1.0 module for 0.5 visible.
	strokeW := fmt.Sprintf("%.1f", 1.0*200/leafW)
	inner := extractSVGInner(overlay)
	inner = strings.ReplaceAll(inner, `stroke-width:8`, `stroke-width:`+strokeW)
	inner = strings.ReplaceAll(inner, `stroke-width="8"`, `stroke-width="`+strokeW+`"`)
	sb.WriteString(inner)
	sb.WriteString(`</svg>`)

	sb.WriteString(`</svg>`)
	return []byte(sb.String())
}

// //

// extractSVGInner strips the outer <svg ...> and </svg> wrapper, returning inner content.
func extractSVGInner(svg string) string {
	start := strings.Index(svg, ">")
	if start < 0 {
		return svg
	}
	end := strings.LastIndex(svg, "</svg>")
	if end < 0 {
		return svg[start+1:]
	}
	return svg[start+1 : end]
}
