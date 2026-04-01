package img

import _ "embed"

// // // // // // // // // //

//go:embed logo_simplified.svg
var logoSVG []byte

//go:embed yggdrasil-leaf.svg
var leafSVG string

// // // // // // // // // //

// Logo returns the ratatoskr logo as SVG bytes.
func Logo() []byte { return logoSVG }
