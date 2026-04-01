package qr

// ecBlocksObj describes the RS block structure for one QR version at EC level Q.
type ecBlocksObj struct {
	ecPerBlock int // EC codewords per block
	g1Count    int // number of blocks in group 1
	g1Data     int // data codewords per block in group 1
	g2Count    int // number of blocks in group 2 (0 = no group 2)
	g2Data     int // data codewords per block in group 2
}

// ecParamsQ holds EC block parameters for versions 1–10 at level Q.
// Index 0 = version 1.
var ecParamsQ = [10]ecBlocksObj{
	{13, 1, 13, 0, 0},  // v1:  13 data
	{22, 1, 22, 0, 0},  // v2:  22 data
	{18, 2, 17, 0, 0},  // v3:  34 data
	{26, 2, 24, 0, 0},  // v4:  48 data
	{18, 2, 15, 2, 16}, // v5:  62 data
	{24, 4, 19, 0, 0},  // v6:  76 data
	{18, 2, 14, 4, 15}, // v7:  88 data
	{22, 4, 18, 2, 19}, // v8:  110 data
	{20, 4, 16, 4, 17}, // v9:  132 data
	{24, 6, 19, 2, 20}, // v10: 154 data
}

// dataCapacityQ is the maximum input bytes for version i+1 at EC level Q.
var dataCapacityQ = [10]int{13, 22, 34, 48, 62, 76, 88, 110, 132, 154}

// alignmentPos lists alignment pattern center coordinates for version i+1.
// Version 1 has no alignment patterns.
var alignmentPos = [10][]int{
	{},
	{6, 18},
	{6, 22},
	{6, 26},
	{6, 30},
	{6, 34},
	{6, 22, 38},
	{6, 24, 42},
	{6, 26, 46},
	{6, 28, 50},
}
