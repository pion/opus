// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeFrameSideInfoValidatesConfig(t *testing.T) {
	decoder := NewDecoder()
	validConfig := frameConfig{
		frameSampleCount: shortBlockSampleCount,
		startBand:        0,
		endBand:          maxBands,
		channelCount:     1,
	}

	cfg := validConfig
	cfg.frameSampleCount = 720
	_, err := decoder.decodeFrameSideInfo(nil, cfg)
	assert.ErrorIs(t, err, errInvalidFrameSize)

	cfg = validConfig
	cfg.startBand = -1
	_, err = decoder.decodeFrameSideInfo(nil, cfg)
	assert.ErrorIs(t, err, errInvalidBand)

	cfg = validConfig
	cfg.endBand = maxBands + 1
	_, err = decoder.decodeFrameSideInfo(nil, cfg)
	assert.ErrorIs(t, err, errInvalidBand)

	cfg = validConfig
	cfg.channelCount = 3
	_, err = decoder.decodeFrameSideInfo(nil, cfg)
	assert.ErrorIs(t, err, errInvalidChannelCount)
}

func TestDecodeFrameSideInfoSilence(t *testing.T) {
	decoder := NewDecoder()

	info, err := decoder.decodeFrameSideInfo(nil, frameConfig{
		frameSampleCount: shortBlockSampleCount,
		startBand:        0,
		endBand:          maxBands,
		channelCount:     1,
	})

	require.NoError(t, err)
	assert.True(t, info.silence)
	assert.Equal(t, 0, info.lm)
	assert.False(t, info.postFilter.enabled)
	assert.False(t, info.transient)
	assert.False(t, info.intraEnergy)
}

func TestDecodeFrameSideInfoAllDefaultFlags(t *testing.T) {
	decoder := NewDecoder()

	info, err := decoder.decodeFrameSideInfo(make([]byte, 8), frameConfig{
		frameSampleCount: shortBlockSampleCount << 1,
		startBand:        0,
		endBand:          maxBands,
		channelCount:     2,
	})

	require.NoError(t, err)
	assert.False(t, info.silence)
	assert.Equal(t, 1, info.lm)
	assert.Equal(t, 2, info.channelCount)
	assert.False(t, info.postFilter.enabled)
	assert.False(t, info.transient)
	assert.Zero(t, info.shortBlockCount)
	assert.False(t, info.intraEnergy)
}

func TestDecodeFrameSideInfoRangeTrace(t *testing.T) {
	decoder := NewDecoder()
	info, err := decoder.validateFrameConfig(frameConfig{
		frameSampleCount: shortBlockSampleCount << 1,
		startBand:        0,
		endBand:          maxBands,
		channelCount:     2,
	})
	require.NoError(t, err)
	info.totalBits = 64

	trace := newRangeTrace(t, &decoder)
	decoder.rangeDecoder.Init(make([]byte, 8))
	trace.require(rangeCheckpoint{
		name:          "range init",
		tell:          1,
		tellFrac:      8,
		remainingBits: 33,
		finalRange:    2147483648,
	})

	decoder.decodeSilenceFlag(&info)
	trace.require(rangeCheckpoint{
		name:          "silence flag",
		tell:          2,
		tellFrac:      9,
		remainingBits: 33,
		finalRange:    2147418112,
	})

	err = decoder.decodePostFilter(&info)
	require.NoError(t, err)
	trace.require(rangeCheckpoint{
		name:          "post-filter disabled",
		tell:          3,
		tellFrac:      17,
		remainingBits: 33,
		finalRange:    1073709056,
	})

	decoder.decodeTransientFlag(&info)
	trace.require(rangeCheckpoint{
		name:          "transient flag",
		tell:          3,
		tellFrac:      18,
		remainingBits: 33,
		finalRange:    939495424,
	})

	decoder.decodeIntraEnergyFlag(&info)
	trace.require(rangeCheckpoint{
		name:          "intra energy flag",
		tell:          3,
		tellFrac:      20,
		remainingBits: 33,
		finalRange:    822058496,
	})
}

func TestDecodePostFilter(t *testing.T) {
	decoder := NewDecoder()
	decoder.rangeDecoder = rangeDecoderWithBinaryOne()
	decoder.rangeDecoder.SetInternalValues(
		[]byte{0x5A, 0xA5},
		40,
		1<<31,
		0,
	)
	info := frameSideInfo{
		startBand: 0,
		totalBits: 256,
	}
	trace := newRangeTrace(t, &decoder)
	trace.require(rangeCheckpoint{
		name:          "before post-filter",
		tell:          8,
		tellFrac:      64,
		remainingBits: -24,
		finalRange:    2147483648,
	})

	err := decoder.decodePostFilter(&info)

	require.NoError(t, err)
	trace.require(rangeCheckpoint{
		name:          "after post-filter",
		tell:          26,
		tellFrac:      205,
		remainingBits: -36,
		finalRange:    44739242,
	})
	assert.True(t, info.postFilter.enabled)
	assert.Equal(t, 5, info.postFilter.octave)
	assert.Equal(t, 676, info.postFilter.period)
	assert.Equal(t, float32(0.5625), info.postFilter.gain)
	assert.Equal(t, 2, info.postFilter.tapset)
}

func TestDecodePostFilterSkipsWhenBandZeroIsAbsent(t *testing.T) {
	decoder := NewDecoder()
	decoder.rangeDecoder = rangeDecoderWithBinaryOne()
	info := frameSideInfo{
		startBand: 17,
		totalBits: 256,
	}

	err := decoder.decodePostFilter(&info)

	require.NoError(t, err)
	assert.False(t, info.postFilter.enabled)
}

func TestDecodeTransientAndIntraFlags(t *testing.T) {
	t.Run("skip 2.5ms transient flag", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{lm: 0, totalBits: 256}

		decoder.decodeTransientFlag(&info)

		assert.False(t, info.transient)
		assert.Zero(t, info.shortBlockCount)
	})

	t.Run("decode transient flag when LM > 0", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{lm: 2, totalBits: 256}

		decoder.decodeTransientFlag(&info)

		assert.True(t, info.transient)
		assert.Equal(t, 4, info.shortBlockCount)
	})

	t.Run("decode intra energy flag", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{totalBits: 256}

		decoder.decodeIntraEnergyFlag(&info)

		assert.True(t, info.intraEnergy)
	})
}

func rangeDecoderWithBinaryOne() rangecoding.Decoder {
	decoder := rangecoding.Decoder{}
	decoder.SetInternalValues(nil, 40, 1<<31, 0)

	return decoder
}
