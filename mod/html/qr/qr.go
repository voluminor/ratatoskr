// Package qr generates QR codes as inline SVG strings without external dependencies.
//
// Encoding: byte mode, EC level Q (25% recovery), versions 1–10 (up to 154 bytes).
package qr

// // // // // // // // // //

// QR returns an SVG string containing a QR code for url.
// Returns an error if url exceeds 154 bytes (version 10, EC level Q limit).
func QR(url string) (string, error) {
	data := []byte(url)
	version, err := selectVersion(data)
	if err != nil {
		return "", err
	}
	return renderSVG(buildMatrix(data, version)), nil
}
