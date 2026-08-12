// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/stretchr/testify/assert"
)

func TestPulseCacheHelpers(t *testing.T) {
	assert.Equal(t, 0, getPulses(0))
	assert.Equal(t, 7, getPulses(7))
	assert.Equal(t, 8, getPulses(8))
	assert.Equal(t, 15, getPulses(15))
	assert.Equal(t, 16, getPulses(16))
	assert.Equal(t, 60, getPulses(31))

	assert.Zero(t, bitsToPulses(0, 0, 0))
	assert.Equal(t, 1, bitsToPulses(0, 0, 8))
	assert.Equal(t, 1, bitsToPulses(0, 1, 16))
	assert.Equal(t, 2, bitsToPulses(0, 1, 24))
	assert.Equal(t, 1, bitsToPulses(1, 1, 16))
	assert.Equal(t, 16, pulsesToBits(0, 1, 1))
	assert.Equal(t, 24, pulsesToBits(0, 1, 2))
	assert.Equal(t, 16, pulsesToBits(1, 1, 1))
}

func TestDecodeFineEnergy(t *testing.T) {
	decoder := NewDecoder()
	decoder.rangeDecoder = rangeDecoderWithRawBits(0b00001010)
	decoder.previousLogE[0][0] = 1
	decoder.previousLogE[1][0] = 2
	decoder.previousLogE[0][1] = 3
	decoder.previousLogE[1][1] = 4
	info := frameSideInfo{
		startBand:    0,
		endBand:      2,
		channelCount: 2,
	}
	var fineQuant [maxBands]int
	fineQuant[0] = 2

	decoder.decodeFineEnergy(&info, fineQuant)

	assert.InDelta(t, 1.125, decoder.previousLogE[0][0], 0.000001)
	assert.InDelta(t, 2.125, decoder.previousLogE[1][0], 0.000001)
	assert.Equal(t, float32(3), decoder.previousLogE[0][1])
	assert.Equal(t, float32(4), decoder.previousLogE[1][1])
}

func TestFinalizeFineEnergy(t *testing.T) {
	decoder := NewDecoder()
	decoder.rangeDecoder = rangeDecoderWithRawBits(0b00000101)
	info := frameSideInfo{
		startBand:    0,
		endBand:      3,
		channelCount: 1,
	}
	var fineQuant [maxBands]int
	var finePriority [maxBands]int
	fineQuant[0] = 2
	fineQuant[1] = 2
	fineQuant[2] = maxFineBits
	finePriority[0] = 0
	finePriority[1] = 1

	decoder.finalizeFineEnergy(&info, fineQuant, finePriority, 2)

	assert.InDelta(t, 0.0625, decoder.previousLogE[0][0], 0.000001)
	assert.InDelta(t, -0.0625, decoder.previousLogE[0][1], 0.000001)
	assert.Zero(t, decoder.previousLogE[0][2])
}

func rangeDecoderWithRawBits(bits byte) rangecoding.Decoder {
	decoder := rangecoding.Decoder{}
	decoder.SetInternalValues([]byte{bits}, 0, 1<<31, 0)

	return decoder
}

func TestIntensityBandForRate(t *testing.T) {
	// libopus intensity_thresholds, read against equiv_rate in kb/s. The values
	// are the bands opus_demo picks at each of these rates for 20 ms stereo.
	cases := []struct {
		kbps int
		band int
	}{
		{23, 9},
		{31, 10},
		{47, 12},
		{63, 15},
		{95, 19},
		{127, 20},
	}

	for _, tc := range cases {
		got := intensityBandForRate(tc.kbps, 0)
		assert.Equal(t, tc.band, got, "kbps=%d", tc.kbps)
	}
}

func TestIntensityBandForRateHysteresis(t *testing.T) {
	// 24 kb/s alone lands on band 10, but coming from 9 the entry's margin
	// (thresholds[9]+hysteresis[9] = 26) holds it there for another frame.
	assert.Equal(t, 10, intensityBandForRate(24, 0))
	assert.Equal(t, 9, intensityBandForRate(24, 9))
	assert.Equal(t, 10, intensityBandForRate(26, 9))
}

func TestIntensityBandForRateMonotonic(t *testing.T) {
	prev := 0
	for kbps := range 200 {
		got := intensityBandForRate(kbps, 0)
		assert.GreaterOrEqual(t, got, prev,
			"intensity must be monotonically non-decreasing: kbps=%d got=%d prev=%d", kbps, got, prev)
		prev = got
	}
}

func TestChooseDualStereo(t *testing.T) {
	n := int(bandEdges[13]) * 4 // lm=2 → scale=4, enough for 13 bands
	t.Run("MonoSignal", func(t *testing.T) {
		mdctL := make([]float32, n)
		mdctR := make([]float32, n)
		for i := range n {
			v := float32(i) * 0.01
			mdctL[i] = v
			mdctR[i] = v
		}
		assert.False(t, chooseDualStereo([2][]float32{mdctL, mdctR}, 2),
			"identical L/R (mono) should always prefer mid/side")
	})

	t.Run("UncorrelatedSignal", func(t *testing.T) {
		mdctL := make([]float32, n)
		mdctR := make([]float32, n)
		for i := range n {
			mdctL[i] = float32(i) * 0.1
			mdctR[i] = float32(n-i) * 0.1
		}
		assert.True(t, chooseDualStereo([2][]float32{mdctL, mdctR}, 2),
			"uncorrelated L/R should prefer dual stereo")
	})

	t.Run("AntiCorrelatedSignal", func(t *testing.T) {
		mdctL := make([]float32, n)
		mdctR := make([]float32, n)
		for i := range n {
			mdctL[i] = float32(i) * 0.1
			mdctR[i] = -float32(i) * 0.1
		}
		assert.False(t, chooseDualStereo([2][]float32{mdctL, mdctR}, 2),
			"fully anti-correlated stereo should prefer mid/side")
	})

	t.Run("Lm0ReturnsFalse", func(t *testing.T) {
		mdctL := make([]float32, n)
		mdctR := make([]float32, n)
		for i := range n {
			mdctL[i] = float32(i) * 0.1
			mdctR[i] = -float32(i) * 0.1
		}
		assert.False(t, chooseDualStereo([2][]float32{mdctL, mdctR}, 0),
			"LM=0 must always return false per RFC §5.3.5")
	})
}
