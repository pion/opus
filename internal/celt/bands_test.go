// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuantBandSingleBin(t *testing.T) {
	decoder := rangeDecoderWithRawBits(0b00000001)
	state := bandDecodeState{rangeDecoder: &decoder}
	x := []float32{0}
	y := []float32{0}
	remainingBits := 2 << bitResolution

	mask := quantBand(
		0, x, y, 1, 2<<bitResolution, spreadNormal, 1, maxBands, 0, nil,
		&remainingBits, 0, nil, 0, 1, nil, 1, &state,
	)

	assert.Equal(t, uint(1), mask)
	assert.Equal(t, float32(-1), x[0])
	assert.Equal(t, float32(1), y[0])
}

func TestQuantBandFoldedAndPulsePaths(t *testing.T) {
	t.Run("zeros when no fold source is available", func(t *testing.T) {
		state := bandDecodeState{rangeDecoder: rangeDecoderForBandTests(), seed: 1}
		x := []float32{7, 7, 7, 7}
		remainingBits := 0

		mask := quantBand(
			0, x, nil, len(x), 0, spreadNormal, 1, maxBands, 0, nil,
			&remainingBits, 0, nil, 1, 1, nil, 0, &state,
		)

		assert.Zero(t, mask)
		assert.Equal(t, []float32{0, 0, 0, 0}, x)
	})

	t.Run("fills from deterministic noise", func(t *testing.T) {
		state := bandDecodeState{rangeDecoder: rangeDecoderForBandTests(), seed: 1}
		x := make([]float32, 4)
		remainingBits := 0

		mask := quantBand(
			0, x, nil, len(x), 0, spreadNormal, 1, maxBands, 0, nil,
			&remainingBits, 0, nil, 1, 1, nil, 1, &state,
		)

		assert.Equal(t, uint(1), mask)
		assert.InDelta(t, 1, vectorEnergy(x), 0.001)
	})

	t.Run("folds from a previous lowband", func(t *testing.T) {
		state := bandDecodeState{rangeDecoder: rangeDecoderForBandTests(), seed: 1}
		x := make([]float32, 4)
		lowband := []float32{0.25, -0.25, 0.5, -0.5}
		remainingBits := 0

		mask := quantBand(
			0, x, nil, len(x), 0, spreadNormal, 1, maxBands, 0, lowband,
			&remainingBits, 0, nil, 1, 1, nil, 1, &state,
		)

		assert.Equal(t, uint(1), mask)
		assert.InDelta(t, 1, vectorEnergy(x), 0.001)
	})

	t.Run("decodes algebraic pulses", func(t *testing.T) {
		decoder := rangeDecoderWithCDFSymbol(0, cwrsUrow(4, 1)[1]+cwrsUrow(4, 1)[2])
		state := bandDecodeState{rangeDecoder: &decoder}
		x := make([]float32, 4)
		remainingBits := 16

		mask := quantBand(
			0, x, nil, len(x), 8, spreadNormal, 1, maxBands, 0, nil,
			&remainingBits, 0, nil, 1, 1, nil, 1, &state,
		)

		assert.Equal(t, uint(1), mask)
		assert.InDelta(t, 1, vectorEnergy(x), 0.001)
	})
}

func TestQuantBandSplits(t *testing.T) {
	t.Run("mono split", func(t *testing.T) {
		state := bandDecodeState{rangeDecoder: rangeDecoderForBandTests(), seed: 1}
		x := make([]float32, 8)
		lowbandOut := make([]float32, 8)
		scratch := make([]float32, 8)
		remainingBits := 512

		mask := quantBand(
			4, x, nil, len(x), 320, spreadNormal, 1, maxBands, 0, nil,
			&remainingBits, 2, lowbandOut, 0, 1, scratch, 1, &state,
		)

		assert.NotZero(t, mask)
		assert.InDelta(t, 1, vectorEnergy(x), 0.001)
		assert.NotZero(t, vectorEnergy(lowbandOut))
	})

	t.Run("stereo split", func(t *testing.T) {
		state := bandDecodeState{rangeDecoder: rangeDecoderForBandTests(), seed: 1}
		x := make([]float32, 4)
		y := make([]float32, 4)
		lowbandOut := make([]float32, 4)
		scratch := make([]float32, 4)
		remainingBits := 512

		mask := quantBand(
			4, x, y, len(x), 320, spreadNormal, 1, maxBands, 0, nil,
			&remainingBits, 2, lowbandOut, 0, 1, scratch, 1, &state,
		)

		assert.NotZero(t, mask)
		assert.InDelta(t, 1, vectorEnergy(x), 0.001)
		assert.InDelta(t, 1, vectorEnergy(y), 0.001)
	})
}

func TestQuantAllBands(t *testing.T) {
	decoder := rangeDecoderWithCDFSymbol(0, 64)
	state := bandDecodeState{rangeDecoder: &decoder, seed: 1}
	info := frameSideInfo{
		lm:           0,
		totalBits:    128,
		startBand:    0,
		endBand:      4,
		channelCount: 2,
		spread:       spreadNormal,
		allocation: allocationState{
			codedBands: 4,
			intensity:  3,
			dualStereo: 1,
		},
	}
	for band := info.startBand; band < info.endBand; band++ {
		info.allocation.pulses[band] = 8
	}
	x := make([]float32, int(bandEdges[maxBands]))
	y := make([]float32, int(bandEdges[maxBands]))

	masks := quantAllBands(&info, x, y, 128<<bitResolution, &state)

	require.Len(t, masks, 2*maxBands)
	assert.NotZero(t, masks[0])
	assert.NotZero(t, masks[1])
	assert.NotZero(t, vectorEnergy(x[:int(bandEdges[info.endBand])]))
	assert.NotZero(t, vectorEnergy(y[:int(bandEdges[info.endBand])]))
}

func TestBandMathHelpers(t *testing.T) {
	assert.False(t, shouldSplitBand(0, 0, 0))
	assert.True(t, shouldSplitBand(4, 2, 320))

	assert.Equal(t, 1, computeQN(4, 0, 0, 0, false))
	assert.Greater(t, computeQN(4, 320, 0, 0, false), 1)
	assert.Greater(t, computeQN(2, 320, 0, 0, true), 1)

	assert.Equal(t, 32768, bitexactCos(0))
	assert.InDelta(t, 23171, bitexactCos(8192), 2)
	assert.Equal(t, 0, bitexactLog2Tan(16384, 16384))
	assert.Equal(t, -2, fracMul16(2<<14, 2))
	assert.Equal(t, uint32(0), isqrt32(0))
	assert.Equal(t, uint32(12), isqrt32(144))
	assert.Equal(t, uint32(1015568748), lcgRand(1))
	assert.Equal(t, uint(0b0001), bitInterleave(0b0011))
	assert.Equal(t, uint(0x0C), bitDeinterleave(0b0010))
}

func TestHadamardHelpers(t *testing.T) {
	vector := []float32{1, 2, 3, 4}
	haar1(vector, len(vector), 1)
	assert.InDelta(t, 2.12132, vector[0], 0.0001)
	assert.InDelta(t, -0.7, vector[1], 0.1)

	vector = []float32{1, 2, 3, 4}
	state := bandDecodeState{}
	deinterleaveHadamard(vector, 2, 2, false, &state)
	assert.Equal(t, []float32{1, 3, 2, 4}, vector)
	interleaveHadamard(vector, 2, 2, false, &state)
	assert.Equal(t, []float32{1, 2, 3, 4}, vector)
	assert.Len(t, state.tmpScratch, len(vector))

	vector = []float32{1, 2, 3, 4}
	deinterleaveHadamard(vector, 2, 2, true, &state)
	interleaveHadamard(vector, 2, 2, true, &state)
	assert.Equal(t, []float32{1, 2, 3, 4}, vector)
}

func TestQuantAllBandsIgnoresDualStereoWithoutSecondChannel(t *testing.T) {
	decoder := rangeDecoderWithCDFSymbol(0, 64)
	state := bandDecodeState{rangeDecoder: &decoder, seed: 1}
	info := frameSideInfo{
		lm:           0,
		totalBits:    128,
		startBand:    0,
		endBand:      2,
		channelCount: 1,
		spread:       spreadNormal,
		allocation: allocationState{
			codedBands: 2,
			intensity:  1,
			dualStereo: 1,
		},
	}
	for band := info.startBand; band < info.endBand; band++ {
		info.allocation.pulses[band] = 8
	}
	x := make([]float32, int(bandEdges[maxBands]))

	masks := quantAllBands(&info, x, nil, 128<<bitResolution, &state)

	require.Len(t, masks, maxBands)
	assert.NotZero(t, masks[0])
}

func TestDecodeBandTheta(t *testing.T) {
	decoder := rangeDecoderWithCDFSymbol(0, 7)
	assert.Equal(t, 0, decodeBandTheta(4, 4, true, 1, &decoder))

	decoder = rangeDecoderWithCDFSymbol(2, 5)
	assert.Equal(t, 2, decodeBandTheta(4, 2, false, 2, &decoder))

	decoder = rangeDecoderWithCDFSymbol(0, 9)
	assert.Equal(t, 0, decodeBandTheta(4, 4, false, 1, &decoder))
}

func TestYBandSlice(t *testing.T) {
	assert.Nil(t, yBandSlice(nil, 0, 1))
	assert.Equal(t, []float32{2, 3}, yBandSlice([]float32{1, 2, 3, 4}, 1, 3))
}

func rangeDecoderForBandTests() *rangecoding.Decoder {
	decoder := rangecoding.Decoder{}
	decoder.SetInternalValues(make([]byte, 16), 0, 1<<31, 0)

	return &decoder
}
