package qr

// // // // // // // // // //

func make2D(size int) [][]bool {
	m := make([][]bool, size)
	for i := range m {
		m[i] = make([]bool, size)
	}
	return m
}

func copy2D(src [][]bool) [][]bool {
	dst := make([][]bool, len(src))
	for i := range src {
		dst[i] = make([]bool, len(src[i]))
		copy(dst[i], src[i])
	}
	return dst
}

// //

// setFinderPattern places a 7×7 finder pattern with top-left corner at (row, col).
func setFinderPattern(m, reserved [][]bool, row, col int) {
	for r := 0; r < 7; r++ {
		for c := 0; c < 7; c++ {
			on := r == 0 || r == 6 || c == 0 || c == 6 ||
				(r >= 2 && r <= 4 && c >= 2 && c <= 4)
			m[row+r][col+c] = on
			reserved[row+r][col+c] = true
		}
	}
}

func placeFinderPatterns(m, reserved [][]bool, size int) {
	setFinderPattern(m, reserved, 0, 0)
	setFinderPattern(m, reserved, 0, size-7)
	setFinderPattern(m, reserved, size-7, 0)
	// Separators: 1-module white border around each finder pattern
	for i := 0; i < 8; i++ {
		reserved[7][i] = true
		reserved[i][7] = true
		reserved[7][size-1-i] = true
		reserved[i][size-8] = true
		reserved[size-8][i] = true
		reserved[size-1-i][7] = true
	}
}

func placeTimingPatterns(m, reserved [][]bool, size int) {
	for i := 8; i < size-8; i++ {
		on := i%2 == 0
		m[6][i] = on
		m[i][6] = on
		reserved[6][i] = true
		reserved[i][6] = true
	}
	// Dark module: always on, position depends on version
	m[size-8][8] = true
	reserved[size-8][8] = true
}

func placeAlignmentPatterns(m, reserved [][]bool, version, size int) {
	pos := alignmentPos[version-1]
	for _, r := range pos {
		for _, c := range pos {
			if reserved[r][c] {
				continue
			}
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					on := dr == -2 || dr == 2 || dc == -2 || dc == 2 ||
						(dr == 0 && dc == 0)
					m[r+dr][c+dc] = on
					reserved[r+dr][c+dc] = true
				}
			}
		}
	}
}

// //

func reserveFormatInfo(reserved [][]bool, size int) {
	for i := 0; i <= 8; i++ {
		reserved[8][i] = true
		reserved[i][8] = true
	}
	for i := 0; i < 8; i++ {
		reserved[8][size-1-i] = true
	}
	for i := 0; i < 7; i++ {
		reserved[size-1-i][8] = true
	}
}

func reserveVersionInfo(reserved [][]bool, size int) {
	for i := 0; i < 18; i++ {
		reserved[5-i%6][size-9-i/6] = true
		reserved[size-9-i/6][5-i%6] = true
	}
}

// // // // // // // // // //

// placeData fills data modules in the QR zigzag pattern (right-to-left column pairs,
// alternating up/down). Reserved cells are skipped; remainder bits are left false.
func placeData(m, reserved [][]bool, codewords []byte, size int) {
	bits := make([]bool, len(codewords)*8)
	for i, cw := range codewords {
		for b := 7; b >= 0; b-- {
			bits[i*8+(7-b)] = (cw>>b)&1 == 1
		}
	}

	bitIdx := 0
	goingUp := true
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 {
			col-- // skip timing column as right side of a pair
		}
		for rowOff := 0; rowOff < size; rowOff++ {
			row := rowOff
			if goingUp {
				row = size - 1 - rowOff
			}
			for dc := 0; dc < 2; dc++ {
				c := col - dc
				if reserved[row][c] {
					continue
				}
				if bitIdx < len(bits) {
					m[row][c] = bits[bitIdx]
					bitIdx++
				}
			}
		}
		goingUp = !goingUp
	}
}

// // // // // // // // // //

// bchFormat computes the 15-bit format information word for fmtData (5-bit value)
// using BCH(15,5) with generator 0x537, then XOR with the QR mask 0x5412.
func bchFormat(fmtData int) int {
	d := fmtData << 10
	for i := 14; i >= 10; i-- {
		if d&(1<<i) != 0 {
			d ^= 0x537 << (i - 10)
		}
	}
	return ((fmtData << 10) | d) ^ 0x5412
}

// placeFormatInfo writes the format information for EC level Q and the given mask.
// First copy: around top-left finder. Second copy: top-right + bottom-left.
func placeFormatInfo(m [][]bool, maskID, size int) {
	// EC level Q = 11 binary = 3; format data = EC(2 bits) | mask(3 bits)
	bits := bchFormat((3 << 3) | maskID)

	// First copy: bit i at positions defined by QR standard
	firstCopy := [15][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8},
		{7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
	}
	for i, pos := range firstCopy {
		m[pos[0]][pos[1]] = (bits>>(14-i))&1 == 1
	}

	// Second copy: bottom-left (bits 0–6) and top-right (bits 7–14)
	for i := 0; i < 7; i++ {
		m[size-1-i][8] = (bits>>(14-i))&1 == 1
	}
	for i := 7; i < 15; i++ {
		m[8][size-15+i] = (bits>>(14-i))&1 == 1
	}
}

// bchVersion computes the 18-bit version information word using BCH(18,6)
// with generator 0x1F25.
func bchVersion(version int) int {
	d := version << 12
	for i := 17; i >= 12; i-- {
		if d&(1<<i) != 0 {
			d ^= 0x1F25 << (i - 12)
		}
	}
	return (version << 12) | d
}

// placeVersionInfo writes the version information blocks (QR v7+).
func placeVersionInfo(m [][]bool, version, size int) {
	bits := bchVersion(version)
	for i := 0; i < 18; i++ {
		v := (bits>>i)&1 == 1
		m[5-i%6][size-9-i/6] = v // top-right block
		m[size-9-i/6][5-i%6] = v // bottom-left block
	}
}

// // // // // // // // // //

func maskCondition(maskID, r, c int) bool {
	switch maskID {
	case 0:
		return (r+c)%2 == 0
	case 1:
		return r%2 == 0
	case 2:
		return c%3 == 0
	case 3:
		return (r+c)%3 == 0
	case 4:
		return (r/2+c/3)%2 == 0
	case 5:
		return (r*c)%2+(r*c)%3 == 0
	case 6:
		return ((r*c)%2+(r*c)%3)%2 == 0
	case 7:
		return ((r+c)%2+(r*c)%3)%2 == 0
	}
	return false
}

func applyMask(m, reserved [][]bool, maskID, size int) [][]bool {
	result := copy2D(m)
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if !reserved[r][c] && maskCondition(maskID, r, c) {
				result[r][c] = !result[r][c]
			}
		}
	}
	return result
}

// //

func penaltyRunScore(row []bool) int {
	score := 0
	run := 1
	for i := 1; i < len(row); i++ {
		if row[i] == row[i-1] {
			run++
		} else {
			if run >= 5 {
				score += 3 + (run - 5)
			}
			run = 1
		}
	}
	if run >= 5 {
		score += 3 + (run - 5)
	}
	return score
}

var (
	penaltyPat1 = []bool{true, false, true, true, true, false, true, false, false, false, false}
	penaltyPat2 = []bool{false, false, false, false, true, false, true, true, true, false, true}
)

func matchPat(row []bool, pat []bool) bool {
	for i := range pat {
		if row[i] != pat[i] {
			return false
		}
	}
	return true
}

func calcPenalty(m [][]bool, size int) int {
	score := 0

	// Rule 1: runs of 5+ in rows and columns
	col := make([]bool, size)
	for r := 0; r < size; r++ {
		score += penaltyRunScore(m[r])
		for c := 0; c < size; c++ {
			col[c] = m[c][r]
		}
		score += penaltyRunScore(col)
	}

	// Rule 2: 2×2 blocks of same colour
	for r := 0; r < size-1; r++ {
		for c := 0; c < size-1; c++ {
			v := m[r][c]
			if m[r][c+1] == v && m[r+1][c] == v && m[r+1][c+1] == v {
				score += 3
			}
		}
	}

	// Rule 3: finder-like patterns in rows and columns
	for r := 0; r < size; r++ {
		for c := 0; c <= size-11; c++ {
			if matchPat(m[r][c:], penaltyPat1) || matchPat(m[r][c:], penaltyPat2) {
				score += 40
			}
		}
	}
	for c := 0; c < size; c++ {
		for r := 0; r <= size-11; r++ {
			match1, match2 := true, true
			for k := 0; k < 11; k++ {
				if m[r+k][c] != penaltyPat1[k] {
					match1 = false
				}
				if m[r+k][c] != penaltyPat2[k] {
					match2 = false
				}
			}
			if match1 || match2 {
				score += 40
			}
		}
	}

	// Rule 4: dark module proportion deviation from 50%
	dark := 0
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if m[r][c] {
				dark++
			}
		}
	}
	pct := dark * 100 / (size * size)
	diff := pct - 50
	if diff < 0 {
		diff = -diff
	}
	score += (diff / 5) * 10

	return score
}

// // // // // // // // // //

// buildMatrix constructs the complete QR code matrix for the given data and version.
func buildMatrix(data []byte, version int) [][]bool {
	size := 21 + (version-1)*4
	codewords := encodePayload(data, version)

	m := make2D(size)
	reserved := make2D(size)

	placeFinderPatterns(m, reserved, size)
	placeTimingPatterns(m, reserved, size)
	placeAlignmentPatterns(m, reserved, version, size)
	reserveFormatInfo(reserved, size)
	if version >= 7 {
		reserveVersionInfo(reserved, size)
	}

	placeData(m, reserved, codewords, size)

	// Evaluate all 8 masks and pick the one with lowest penalty score
	bestMask := 0
	bestScore := -1
	var bestMatrix [][]bool
	for maskID := 0; maskID < 8; maskID++ {
		masked := applyMask(m, reserved, maskID, size)
		placeFormatInfo(masked, maskID, size)
		if version >= 7 {
			placeVersionInfo(masked, version, size)
		}
		s := calcPenalty(masked, size)
		if bestScore < 0 || s < bestScore {
			bestScore = s
			bestMask = maskID
			bestMatrix = masked
		}
	}
	_ = bestMask
	return bestMatrix
}
