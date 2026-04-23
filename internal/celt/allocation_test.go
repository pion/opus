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
