package qr

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestCompareWithQrencode generates a matrix for a given input and compares it
// module-by-module against the output of qrencode (reference implementation).
func TestCompareWithQrencode(t *testing.T) {
	if _, err := exec.LookPath("qrencode"); err != nil {
		t.Skip("qrencode not found")
	}

	// Only byte-mode inputs where our mask selection matches qrencode.
	// Differences in mode (numeric/alphanumeric) or mask choice produce
	// completely different but equally valid matrices.
	inputs := []string{
		"https://example.com",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			// Generate reference matrix from qrencode
			refMatrix := qrencodeMatrix(t, input)
			if refMatrix == nil {
				return
			}

			// Generate our matrix
			data := []byte(input)
			version, err := selectVersion(data)
			if err != nil {
				t.Fatal(err)
			}
			ourMatrix := buildMatrix(data, version)

			refSize := len(refMatrix)
			ourSize := len(ourMatrix)
			if refSize != ourSize {
				t.Fatalf("size mismatch: ref=%d, ours=%d (version ref=? ours=%d)",
					refSize, ourSize, version)
				return
			}

			diffs := 0
			for r := 0; r < ourSize; r++ {
				for c := 0; c < ourSize; c++ {
					if ourMatrix[r][c] != refMatrix[r][c] {
						diffs++
						if diffs <= 20 {
							t.Errorf("diff at (%d,%d): ours=%v ref=%v", r, c, ourMatrix[r][c], refMatrix[r][c])
						}
					}
				}
			}
			if diffs > 20 {
				t.Errorf("... and %d more diffs (total %d)", diffs-20, diffs)
			}
			if diffs > 0 {
				t.Log("OUR matrix:")
				t.Log(matrixASCII(ourMatrix))
				t.Log("REF matrix:")
				t.Log(matrixASCII(refMatrix))
			}
		})
	}
}

// qrencodeMatrix calls qrencode and parses the ASCII output into a bool matrix.
func qrencodeMatrix(t *testing.T, input string) [][]bool {
	t.Helper()
	// qrencode -t ASCII outputs '#' for dark and ' ' for light, one row per line
	cmd := exec.Command("qrencode", "-t", "ASCII", "-l", "Q", "-m", "0", input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("qrencode failed: %v", err)
		return nil
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	// ASCII mode uses 2 chars per module (## for dark, "  " for light)
	size := len(lines)
	if size == 0 {
		t.Fatal("qrencode output empty")
		return nil
	}
	// Detect char width per module
	lineLen := len(lines[0])
	charsPerModule := lineLen / size
	if charsPerModule < 1 {
		charsPerModule = 1
	}

	m := make([][]bool, size)
	for r, line := range lines {
		m[r] = make([]bool, size)
		for c := 0; c < size; c++ {
			idx := c * charsPerModule
			if idx < len(line) {
				m[r][c] = line[idx] == '#'
			}
		}
	}
	return m
}

func matrixASCII(m [][]bool) string {
	var sb strings.Builder
	for _, row := range m {
		for _, cell := range row {
			if cell {
				fmt.Fprint(&sb, "██")
			} else {
				fmt.Fprint(&sb, "  ")
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
