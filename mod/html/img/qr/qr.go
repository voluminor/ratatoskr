// Package qr generates QR codes as SVG without external dependencies.
//
// Encoding: byte mode, EC level Q (25% recovery), versions 1–10 (up to 154 bytes).
package qr

// // // // // // // // // //

// Generate returns a QR code SVG for url.
func Generate(url string) ([]byte, error) {
	data := []byte(url)
	version, err := selectVersion(data)
	if err != nil {
		return nil, err
	}
	return RenderSVG(buildMatrix(data, version)), nil
}

// Matrix returns the raw QR boolean matrix for url (true = dark module).
func Matrix(url string) ([][]bool, error) {
	data := []byte(url)
	version, err := selectVersion(data)
	if err != nil {
		return nil, err
	}
	return buildMatrix(data, version), nil
}
