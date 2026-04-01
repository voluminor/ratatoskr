package qr

import "sync"

// // // // // // // // // //

var (
	gfOnce sync.Once
	gfExp  [512]byte // antilog table, doubled to avoid modulo in multiply
	gfLog  [256]int  // log table; gfLog[0] is unused
)

// gfInit initialises GF(256) tables using the QR primitive polynomial
// x^8 + x^4 + x^3 + x^2 + 1 = 0x11D.
func gfInit() {
	gfOnce.Do(func() {
		x := 1
		for i := 0; i < 255; i++ {
			gfExp[i] = byte(x)
			gfLog[x] = i
			x <<= 1
			if x&0x100 != 0 {
				x ^= 0x11D
			}
		}
		for i := 255; i < 512; i++ {
			gfExp[i] = gfExp[i-255]
		}
	})
}

// //

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

// //

// polyMul multiplies two polynomials over GF(256).
// Coefficients are ordered from highest to lowest degree.
func polyMul(p, q []byte) []byte {
	res := make([]byte, len(p)+len(q)-1)
	for i, a := range p {
		for j, b := range q {
			res[i+j] ^= gfMul(a, b)
		}
	}
	return res
}

// rsGeneratorPoly returns the RS generator polynomial for ecN EC codewords:
// g(x) = ∏(x + α^i) for i = 0..ecN-1, coefficients high-to-low.
func rsGeneratorPoly(ecN int) []byte {
	gfInit()
	g := []byte{1}
	for i := 0; i < ecN; i++ {
		g = polyMul(g, []byte{1, gfExp[i]})
	}
	return g
}

// // // // // // // // // //

// rsEncode computes ecN Reed-Solomon error correction codewords for data.
// Uses polynomial long division over GF(256).
func rsEncode(data []byte, ecN int) []byte {
	gfInit()
	gen := rsGeneratorPoly(ecN)
	msg := make([]byte, len(data)+ecN)
	copy(msg, data)
	for i := range data {
		coef := msg[i]
		if coef == 0 {
			continue
		}
		for j, g := range gen {
			msg[i+j] ^= gfMul(coef, g)
		}
	}
	return msg[len(data):]
}
