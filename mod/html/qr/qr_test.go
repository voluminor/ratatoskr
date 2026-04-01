package qr

import (
	"strings"
	"testing"
)

// // // // // // // // // //
// GF256

func TestGF256Multiply(t *testing.T) {
	gfInit()
	// 2 * 0x80 = x * x^7 = x^8, which reduces via 0x11D: x^8 = x^4+x^3+x^2+1 = 0x1D
	if got := gfMul(2, 0x80); got != 0x1D {
		t.Errorf("gfMul(2, 0x80) = %#x, want 0x1D", got)
	}
	// identity
	for x := byte(1); x != 0; x++ {
		if got := gfMul(1, x); got != x {
			t.Errorf("gfMul(1, %d) = %d, want %d", x, got, x)
		}
	}
	// zero
	for x := byte(0); x != 255; x++ {
		if got := gfMul(0, x); got != 0 {
			t.Errorf("gfMul(0, %d) = %d, want 0", x, got)
		}
	}
	// commutative
	if gfMul(3, 7) != gfMul(7, 3) {
		t.Error("gfMul not commutative")
	}
}

// // // // // // // // // //
// Reed-Solomon self-check: data || rsEncode(data) must be divisible by the generator poly.

func TestRSEncodeSelfConsistency(t *testing.T) {
	gfInit()
	// Version 1 Q: 13 data codewords, 13 EC codewords
	// Encoding "1" (one byte, version 1 Q)
	data := []byte{0x40, 0x13, 0x10, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11}
	ec := rsEncode(data, 13)
	if len(ec) != 13 {
		t.Fatalf("rsEncode returned %d bytes, want 13", len(ec))
	}

	// Verify: the full codeword (data||ec) must be a multiple of the generator polynomial.
	msg := append(append([]byte{}, data...), ec...)
	gen := rsGeneratorPoly(13)
	rem := make([]byte, len(msg))
	copy(rem, msg)
	for i := 0; i < len(data); i++ {
		coef := rem[i]
		if coef == 0 {
			continue
		}
		for j, g := range gen {
			rem[i+j] ^= gfMul(coef, g)
		}
	}
	for i, b := range rem[len(data):] {
		if b != 0 {
			t.Errorf("RS check failed at EC position %d: got %#x, want 0x00", i, b)
		}
	}
}

// // // // // // // // // //
// Format information

// readFormatInfo reads the 15 format info bits from the first copy in the matrix.
func readFormatInfo(m [][]bool) int {
	firstCopy := [15][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8},
		{7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
	}
	bits := 0
	for i, pos := range firstCopy {
		if m[pos[0]][pos[1]] {
			bits |= 1 << i
		}
	}
	return bits
}

// decodeFormatInfo returns (ecIndicator, maskID, valid).
// Strips the XOR mask, verifies BCH, and extracts the 5-bit data.
func decodeFormatInfo(rawBits int) (ecIndicator, maskID int, valid bool) {
	unmasked := rawBits ^ 0x5412
	// BCH check: remainder after dividing unmasked by generator should be 0
	d := unmasked
	for i := 14; i >= 10; i-- {
		if d&(1<<i) != 0 {
			d ^= 0x537 << (i - 10)
		}
	}
	if d != 0 {
		return 0, 0, false
	}
	// Extract 5-bit data: upper 5 bits of unmasked (bits 14-10)
	fmtData := (unmasked >> 10) & 0x1F
	return (fmtData >> 3) & 0x3, fmtData & 0x7, true
}

func TestBchFormat(t *testing.T) {
	// For EC level Q the bchFormat output must decode back to EC=3 (Q) and the given mask.
	for maskID := 0; maskID < 8; maskID++ {
		fmtData := (3 << 3) | maskID // Q=11binary=3
		bits := bchFormat(fmtData)
		ec, mask, ok := decodeFormatInfo(bits)
		if !ok {
			t.Errorf("mask %d: BCH check failed (bits=%#x)", maskID, bits)
			continue
		}
		if ec != 3 {
			t.Errorf("mask %d: decoded EC=%d, want 3 (Q)", maskID, ec)
		}
		if mask != maskID {
			t.Errorf("mask %d: decoded mask=%d", maskID, mask)
		}
	}
}

// // // // // // // // // //
// Matrix structure

func TestFinderPatterns(t *testing.T) {
	m := make2D(21)
	reserved := make2D(21)
	placeFinderPatterns(m, reserved, 21)

	// Border of top-left finder: rows 0 and 6, cols 0-6; cols 0 and 6, rows 0-6
	for i := 0; i < 7; i++ {
		if !m[0][i] {
			t.Errorf("top-left finder border missing at (0,%d)", i)
		}
		if !m[6][i] {
			t.Errorf("top-left finder border missing at (6,%d)", i)
		}
		if !m[i][0] {
			t.Errorf("top-left finder border missing at (%d,0)", i)
		}
		if !m[i][6] {
			t.Errorf("top-left finder border missing at (%d,6)", i)
		}
	}
	// Interior of top-left finder: separator ring must be white
	for r := 1; r <= 5; r++ {
		for c := 1; c <= 5; c++ {
			inCenter := r >= 2 && r <= 4 && c >= 2 && c <= 4
			inBorder := r == 1 || r == 5 || c == 1 || c == 5
			if inBorder && !inCenter {
				if m[r][c] {
					t.Errorf("top-left finder separator not white at (%d,%d)", r, c)
				}
			}
		}
	}
	// Center 3×3 must be dark
	for r := 2; r <= 4; r++ {
		for c := 2; c <= 4; c++ {
			if !m[r][c] {
				t.Errorf("top-left finder center not dark at (%d,%d)", r, c)
			}
		}
	}
}

func TestTimingPatterns(t *testing.T) {
	m := make2D(21)
	reserved := make2D(21)
	placeTimingPatterns(m, reserved, 21)

	// Row 6, cols 8-12: alternating dark/light starting with dark
	for i := 8; i <= 12; i++ {
		want := i%2 == 0
		if m[6][i] != want {
			t.Errorf("timing row 6 col %d: got %v, want %v", i, m[6][i], want)
		}
	}
	// Col 6, rows 8-12: same
	for i := 8; i <= 12; i++ {
		want := i%2 == 0
		if m[i][6] != want {
			t.Errorf("timing col 6 row %d: got %v, want %v", i, m[i][6], want)
		}
	}
	// Dark module
	if !m[13][8] {
		t.Error("dark module (13,8) not set")
	}
}

// // // // // // // // // //
// Integration

func TestFormatInfoRoundTrip(t *testing.T) {
	// Generate a QR for a short URL and verify format info decodes to EC=Q.
	matrix := buildMatrix([]byte("https://ygg.example.com/"), 2)
	rawBits := readFormatInfo(matrix)
	ec, _, ok := decodeFormatInfo(rawBits)
	if !ok {
		t.Fatalf("format info BCH check failed (bits=%#x)", rawBits)
	}
	if ec != 3 {
		t.Errorf("format info EC indicator = %d (binary %02b), want 3 (Q=11)", ec, ec)
	}
}

func TestQROutputIsSVG(t *testing.T) {
	svg, err := QR("https://ygg.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg, "<svg ") {
		t.Errorf("output doesn't start with <svg>: %q", svg[:min(len(svg), 20)])
	}
	if !strings.Contains(svg, `viewBox=`) {
		t.Error("SVG missing viewBox attribute")
	}
	if !strings.Contains(svg, `color-scheme:light`) {
		t.Error("SVG missing color-scheme protection")
	}
	if !strings.Contains(svg, `forced-color-adjust:none`) {
		t.Error("SVG missing forced-color-adjust protection")
	}
	if !strings.Contains(svg, `!important`) {
		t.Error("SVG missing !important color protection")
	}
}

func TestQRTooLong(t *testing.T) {
	// 155 bytes should fail (version 10 Q max = 154)
	_, err := QR(strings.Repeat("x", 155))
	if err == nil {
		t.Error("expected error for input > 154 bytes, got nil")
	}
}

func TestQRVersionSelection(t *testing.T) {
	cases := []struct {
		length  int
		version int
	}{
		{13, 1},
		{14, 2},
		{22, 2},
		{23, 3},
		{34, 3},
		{35, 4},
		{154, 10},
	}
	for _, tc := range cases {
		data := []byte(strings.Repeat("a", tc.length))
		got, err := selectVersion(data)
		if err != nil {
			t.Errorf("len=%d: unexpected error: %v", tc.length, err)
			continue
		}
		if got != tc.version {
			t.Errorf("len=%d: version=%d, want %d", tc.length, got, tc.version)
		}
	}
}

// // // // // // // // // //

// // // // // // // // // //
// Decode round-trip: generate QR → read back bits → RS decode → verify original payload.

// buildReserved reconstructs the reserved-cell map for a given version.
func buildReserved(version, size int) [][]bool {
	m := make2D(size)
	reserved := make2D(size)
	placeFinderPatterns(m, reserved, size)
	placeTimingPatterns(m, reserved, size)
	placeAlignmentPatterns(m, reserved, version, size)
	reserveFormatInfo(reserved, size)
	if version >= 7 {
		reserveVersionInfo(reserved, size)
	}
	return reserved
}

// readZigzagBits mirrors placeData: reads data bits from the matrix in zigzag order.
func readZigzagBits(matrix [][]bool, reserved [][]bool, size int) []bool {
	var bits []bool
	goingUp := true
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 {
			col--
		}
		for rowOff := 0; rowOff < size; rowOff++ {
			row := rowOff
			if goingUp {
				row = size - 1 - rowOff
			}
			for dc := 0; dc < 2; dc++ {
				c := col - dc
				if !reserved[row][c] {
					bits = append(bits, matrix[row][c])
				}
			}
		}
		goingUp = !goingUp
	}
	return bits
}

func TestDecodeRoundTrip(t *testing.T) {
	inputs := []string{
		"A",
		"https://ygg.example.com/",
		"http://[200:abcd::1]:8080/",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			data := []byte(input)
			version, err := selectVersion(data)
			if err != nil {
				t.Fatal(err)
			}
			size := 21 + (version-1)*4
			matrix := buildMatrix(data, version)

			// Read format info to get the chosen mask
			rawBits := readFormatInfo(matrix)
			_, maskID, ok := decodeFormatInfo(rawBits)
			if !ok {
				t.Fatalf("format info BCH invalid (bits=%#x)", rawBits)
			}

			// Apply inverse mask to the matrix
			reserved := buildReserved(version, size)
			unmasked := copy2D(matrix)
			for r := 0; r < size; r++ {
				for c := 0; c < size; c++ {
					if !reserved[r][c] && maskCondition(maskID, r, c) {
						unmasked[r][c] = !unmasked[r][c]
					}
				}
			}

			// Read raw data bits in zigzag order
			rawBits2 := readZigzagBits(unmasked, reserved, size)

			// Convert bits to codewords
			p := ecParamsQ[version-1]
			totalBlocks := p.g1Count + p.g2Count
			totalDataCW := p.g1Count*p.g1Data + p.g2Count*p.g2Data
			totalCW := totalDataCW + totalBlocks*p.ecPerBlock

			if len(rawBits2) < totalCW*8 {
				t.Fatalf("not enough bits: got %d, need %d", len(rawBits2), totalCW*8)
			}

			codewords := make([]byte, totalCW)
			for i := range codewords {
				for b := 0; b < 8; b++ {
					if rawBits2[i*8+b] {
						codewords[i] |= 1 << (7 - b)
					}
				}
			}

			// De-interleave: extract per-block data codewords
			// Build block sizes (mirrors encodePayload interleaving)
			type blockSizeObj struct{ dataLen int }
			blockSizes := make([]blockSizeObj, 0, totalBlocks)
			for i := 0; i < p.g1Count; i++ {
				blockSizes = append(blockSizes, blockSizeObj{p.g1Data})
			}
			for i := 0; i < p.g2Count; i++ {
				blockSizes = append(blockSizes, blockSizeObj{p.g2Data})
			}
			maxData := p.g1Data
			if p.g2Count > 0 && p.g2Data > maxData {
				maxData = p.g2Data
			}

			// De-interleave data codewords
			blockData := make([][]byte, totalBlocks)
			for i := range blockData {
				blockData[i] = make([]byte, blockSizes[i].dataLen)
			}
			pos := 0
			for i := 0; i < maxData; i++ {
				for b, bs := range blockSizes {
					if i < bs.dataLen {
						blockData[b][i] = codewords[pos]
						pos++
					}
				}
			}

			// De-interleave EC codewords and RS-verify each block
			blockEC := make([][]byte, totalBlocks)
			for i := range blockEC {
				blockEC[i] = make([]byte, p.ecPerBlock)
			}
			for i := 0; i < p.ecPerBlock; i++ {
				for b := range blockSizes {
					blockEC[b][i] = codewords[pos]
					pos++
				}
			}

			for b := range blockSizes {
				full := append(append([]byte{}, blockData[b]...), blockEC[b]...)
				gen := rsGeneratorPoly(p.ecPerBlock)
				rem := make([]byte, len(full))
				copy(rem, full)
				for i := 0; i < len(blockData[b]); i++ {
					coef := rem[i]
					if coef == 0 {
						continue
					}
					for j, g := range gen {
						rem[i+j] ^= gfMul(coef, g)
					}
				}
				for i, bt := range rem[len(blockData[b]):] {
					if bt != 0 {
						t.Errorf("block %d RS check failed at EC pos %d: got %#x", b, i, bt)
					}
				}
			}

			// Reconstruct full data sequence from de-interleaved blocks
			var allData []byte
			for _, bd := range blockData {
				allData = append(allData, bd...)
			}

			// Decode byte-mode payload: 4-bit mode + 8-bit count + data
			// Build bitstream from allData
			var msgBits []bool
			for _, byt := range allData {
				for b := 7; b >= 0; b-- {
					msgBits = append(msgBits, (byt>>b)&1 == 1)
				}
			}

			// Read mode indicator (4 bits)
			mode := 0
			for i := 0; i < 4; i++ {
				if msgBits[i] {
					mode |= 1 << (3 - i)
				}
			}
			if mode != 0b0100 {
				t.Errorf("mode indicator = %04b, want 0100 (byte mode)", mode)
			}

			// Read character count (8 bits for v1-9, 16 for v10)
			countBits := 8
			if version == 10 {
				countBits = 16
			}
			charCount := 0
			for i := 0; i < countBits; i++ {
				if msgBits[4+i] {
					charCount |= 1 << (countBits - 1 - i)
				}
			}
			if charCount != len(data) {
				t.Errorf("decoded char count = %d, want %d", charCount, len(data))
			}

			// Read data bytes
			decoded := make([]byte, charCount)
			base := 4 + countBits
			for i := 0; i < charCount; i++ {
				for b := 0; b < 8; b++ {
					if msgBits[base+i*8+b] {
						decoded[i] |= 1 << (7 - b)
					}
				}
			}
			if string(decoded) != input {
				t.Errorf("decoded = %q, want %q", decoded, input)
			}
		})
	}
}

// // // // // // // // // //

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
