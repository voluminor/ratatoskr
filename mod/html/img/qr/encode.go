package qr

import "fmt"

// // // // // // // // // //

// bitWriterObj accumulates bits into a byte slice, MSB first.
type bitWriterObj struct {
	buf []byte
	cur byte // byte being assembled
	pos int  // next bit position within cur (0-7, MSB=0)
	n   int  // total bits written
}

func (w *bitWriterObj) write(val uint, bits int) {
	for i := bits - 1; i >= 0; i-- {
		bit := byte((val >> i) & 1)
		w.cur |= bit << (7 - w.pos)
		w.pos++
		w.n++
		if w.pos == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur = 0
			w.pos = 0
		}
	}
}

func (w *bitWriterObj) bitLen() int { return w.n }

func (w *bitWriterObj) bytes() []byte {
	if w.pos > 0 {
		return append(append([]byte{}, w.buf...), w.cur)
	}
	return append([]byte{}, w.buf...)
}

// // // // // // // // // //

// selectVersion returns the minimum QR version (1–10) that fits data at EC level Q.
func selectVersion(data []byte) (int, error) {
	n := len(data)
	for i, cap := range dataCapacityQ {
		if n <= cap {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("input too long: %d bytes exceeds version 10 Q capacity (154 bytes)", n)
}

// // // // // // // // // //

// encodePayload returns the final interleaved codeword sequence (data + EC).
func encodePayload(data []byte, version int) []byte {
	p := ecParamsQ[version-1]
	totalData := p.g1Count*p.g1Data + p.g2Count*p.g2Data

	// Build the data bit stream: mode | char count | bytes | terminator | padding
	w := &bitWriterObj{}
	w.write(0b0100, 4) // byte mode indicator
	// Character count: 8 bits for versions 1–9, 16 bits for version 10+ (byte mode).
	countBits := 8
	if version >= 10 {
		countBits = 16
	}
	w.write(uint(len(data)), countBits)
	for _, b := range data {
		w.write(uint(b), 8)
	}

	// Terminator: up to 4 zero bits
	rem := totalData*8 - w.bitLen()
	if rem > 4 {
		rem = 4
	}
	w.write(0, rem)

	// Pad to byte boundary
	for w.bitLen()%8 != 0 {
		w.write(0, 1)
	}

	// Pad bytes to fill capacity
	padSeq := [2]byte{0xEC, 0x11}
	for i := 0; w.bitLen()/8 < totalData; i++ {
		w.write(uint(padSeq[i%2]), 8)
	}

	raw := w.bytes()

	// Split raw data into blocks
	type blockObj struct{ data []byte }
	blocks := make([]blockObj, 0, p.g1Count+p.g2Count)
	pos := 0
	for i := 0; i < p.g1Count; i++ {
		blocks = append(blocks, blockObj{raw[pos : pos+p.g1Data]})
		pos += p.g1Data
	}
	for i := 0; i < p.g2Count; i++ {
		blocks = append(blocks, blockObj{raw[pos : pos+p.g2Data]})
		pos += p.g2Data
	}

	// Generate RS error correction for each block
	type blockECObj struct {
		data []byte
		ec   []byte
	}
	blocksEC := make([]blockECObj, len(blocks))
	for i, b := range blocks {
		blocksEC[i] = blockECObj{b.data, rsEncode(b.data, p.ecPerBlock)}
	}

	// Interleave data codewords
	maxData := p.g1Data
	if p.g2Count > 0 && p.g2Data > maxData {
		maxData = p.g2Data
	}
	result := make([]byte, 0, totalData+len(blocks)*p.ecPerBlock)
	for i := 0; i < maxData; i++ {
		for _, b := range blocksEC {
			if i < len(b.data) {
				result = append(result, b.data[i])
			}
		}
	}

	// Interleave EC codewords
	for i := 0; i < p.ecPerBlock; i++ {
		for _, b := range blocksEC {
			result = append(result, b.ec[i])
		}
	}

	return result
}
