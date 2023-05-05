// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package rangecoding

import (
	"testing"
)

// nolint: gochecknoglobals
var (
	silkModelFrameTypeInactive = []uint{256, 26, 256}

	silkModelGainHighbits = [][]uint{
		{256, 32, 144, 212, 241, 253, 254, 255, 256},
		{256, 2, 19, 64, 124, 186, 233, 252, 256},
		{256, 1, 4, 30, 101, 195, 245, 254, 256},
	}

	silkModelGainLowbits = []uint{256, 32, 64, 96, 128, 160, 192, 224, 256}

	silkModelGainDelta = []uint{
		256, 6, 11, 22, 53, 185, 206, 214, 218, 221, 223, 225, 227, 228,
		229, 230, 231, 232, 233, 234, 235, 236, 237, 238, 239, 240, 241, 242,
		243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 254, 255, 256,
	}

	silkModelLsfS1 = [][][]uint{
		{
			{
				256, 44, 78, 108, 127, 148, 160, 171, 174, 177, 179,
				195, 197, 199, 200, 205, 207, 208, 211, 214, 215, 216,
				218, 220, 222, 225, 226, 235, 244, 246, 253, 255, 256,
			},
			{
				256, 1, 11, 12, 20, 23, 31, 39, 53, 66, 80,
				81, 95, 107, 120, 131, 142, 154, 165, 175, 185, 196,
				204, 213, 221, 228, 236, 237, 238, 244, 245, 251, 256,
			},
		},
		{
			{
				256, 31, 52, 55, 72, 73, 81, 98, 102, 103, 121,
				137, 141, 143, 146, 147, 157, 158, 161, 177, 188, 204,
				206, 208, 211, 213, 224, 225, 229, 238, 246, 253, 256,
			},
			{
				256, 1, 5, 21, 26, 44, 55, 60, 74, 89, 90,
				93, 105, 118, 132, 146, 152, 166, 178, 180, 186, 187,
				199, 211, 222, 232, 235, 245, 250, 251, 252, 253, 256,
			},
		},
	}

	silkModelLsfS2 = [][]uint{
		{256, 1, 2, 3, 18, 242, 253, 254, 255, 256},
		{256, 1, 2, 4, 38, 221, 253, 254, 255, 256},
		{256, 1, 2, 6, 48, 197, 252, 254, 255, 256},
		{256, 1, 2, 10, 62, 185, 246, 254, 255, 256},
		{256, 1, 4, 20, 73, 174, 248, 254, 255, 256},
		{256, 1, 4, 21, 76, 166, 239, 254, 255, 256},
		{256, 1, 8, 32, 85, 159, 226, 252, 255, 256},
		{256, 1, 2, 20, 83, 161, 219, 249, 255, 256},
		{256, 1, 2, 3, 12, 244, 253, 254, 255, 256},
		{256, 1, 2, 4, 32, 218, 253, 254, 255, 256},
		{256, 1, 2, 5, 47, 199, 252, 254, 255, 256},
		{256, 1, 2, 12, 61, 187, 252, 254, 255, 256},
		{256, 1, 5, 24, 72, 172, 249, 254, 255, 256},
		{256, 1, 2, 16, 70, 170, 242, 254, 255, 256},
		{256, 1, 2, 17, 78, 165, 226, 251, 255, 256},
		{256, 1, 8, 29, 79, 156, 237, 254, 255, 256},
	}

	silkModelLsfInterpolationOffset = []uint{256, 13, 35, 64, 75, 256}

	silkModelLcgSeed = []uint{256, 64, 128, 192, 256}

	silkModelExcRate = [][]uint{
		{256, 15, 66, 78, 124, 169, 182, 215, 242, 256},
		{256, 33, 63, 99, 116, 150, 199, 217, 238, 256},
	}

	silkModelPulseCount = [][]uint{
		{
			256, 131, 205, 230, 238, 241, 244, 245, 246, 247, 248, 249, 250, 251, 252,
			253, 254, 255, 256,
		},
		{
			256, 58, 151, 211, 234, 241, 244, 245, 246, 247, 248, 249, 250, 251, 252,
			253, 254, 255, 256,
		},
		{
			256, 43, 94, 140, 173, 197, 213, 224, 232, 238, 241, 244, 247, 249, 250,
			251, 253, 254, 256,
		},
		{
			256, 17, 69, 140, 197, 228, 240, 245, 246, 247, 248, 249, 250, 251, 252,
			253, 254, 255, 256,
		},
		{
			256, 6, 27, 68, 121, 170, 205, 226, 237, 243, 246, 248, 250, 251, 252, 253,
			254, 255, 256,
		},
		{
			256, 7, 21, 43, 71, 100, 128, 153, 173, 190, 203, 214, 223, 230, 235, 239,
			243, 246, 256,
		},
		{
			256, 2, 7, 21, 50, 92, 138, 179, 210, 229, 240, 246, 249, 251, 252, 253,
			254, 255, 256,
		},
		{
			256, 1, 3, 7, 17, 36, 65, 100, 137, 171, 199, 219, 233, 241, 246, 250, 252,
			254, 256,
		},
		{
			256, 1, 3, 5, 10, 19, 33, 53, 77, 104, 132, 158, 181, 201, 216, 227, 235,
			241, 256,
		},
		{
			256, 1, 2, 3, 9, 36, 94, 150, 189, 214, 228, 238, 244, 247, 250, 252, 253,
			254, 256,
		},
		{
			256, 2, 3, 9, 36, 94, 150, 189, 214, 228, 238, 244, 247, 250, 252, 253,
			254, 256, 256,
		},
	}
)

func TestDecoder(t *testing.T) {
	d := &Decoder{}
	d.Init([]byte{0x0b, 0xe4, 0xc1, 0x36, 0xec, 0xc5, 0x80})

	if result := d.DecodeSymbolLogP(0x1); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolLogP(0x1); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelFrameTypeInactive); result != 1 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelGainHighbits[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelGainLowbits); result != 6 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelGainDelta); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelGainDelta); result != 3 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelGainDelta); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS1[1][0]); result != 9 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[10]); result != 5 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[9]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfS2[8]); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLsfInterpolationOffset); result != 4 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelLcgSeed); result != 2 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelExcRate[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
	if result := d.DecodeSymbolWithICDF(silkModelPulseCount[0]); result != 0 {
		t.Fatal("")
	}
}
