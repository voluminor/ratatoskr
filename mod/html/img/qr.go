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

// //

// renderOverlay renders the QR matrix as SVG with a centered yggdrasil-leaf on top.
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

	// Leaf viewBox "30 0 200 260", aspect 200:260
	center := float64(total) / 2
	leafW := 0.312 * float64(size)
	leafH := leafW * 1.3
	leafX := center - leafW/2
	leafY := center - leafH/2

	fmt.Fprintf(&sb,
		`<svg x="%.1f" y="%.1f" width="%.1f" height="%.1f" viewBox="30 0 200 260" shape-rendering="geometricPrecision">`,
		leafX, leafY, leafW, leafH,
	)

	// Stroke is centered on the path: half outward (visible), half inward (under green fill).
	// stroke-width = 1.0 module → 0.5 module visible border.
	strokeW := fmt.Sprintf("%.1f", 200/leafW)
	inner := extractSVGInner(overlay)
	inner = strings.ReplaceAll(inner, `stroke-width:8`, `stroke-width:`+strokeW)
	inner = strings.ReplaceAll(inner, `stroke-width="8"`, `stroke-width="`+strokeW+`"`)
	sb.WriteString(inner)
	sb.WriteString(`</svg>`)

	sb.WriteString(`</svg>`)
	return []byte(sb.String())
}

// // // // // // // // // //

// QRCode returns an SVG QR code for the Yggdrasil address derived from key,
// with a yggdrasil-leaf overlay in the center.
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
