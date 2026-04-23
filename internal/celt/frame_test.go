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
		frameSampleCount:   shortBlockSampleCount,
		startBand:          0,
		endBand:            maxBands,
		channelCount:       1,
		outputChannelCount: 1,
	}

	cfg := validConfig
	cfg.frameSampleCount = 720
	_, err := decoder.decodeFrameSideInfo(nil, cfg, nil)
	assert.ErrorIs(t, err, errInvalidFrameSize)

	cfg = validConfig
	cfg.startBand = -1
	_, err = decoder.decodeFrameSideInfo(nil, cfg, nil)
	assert.ErrorIs(t, err, errInvalidBand)

	cfg = validConfig
	cfg.endBand = maxBands + 1
	_, err = decoder.decodeFrameSideInfo(nil, cfg, nil)
	assert.ErrorIs(t, err, errInvalidBand)

	cfg = validConfig
	cfg.channelCount = 3
	_, err = decoder.decodeFrameSideInfo(nil, cfg, nil)
	assert.ErrorIs(t, err, errInvalidChannelCount)
}

func TestDecodeFrameSideInfoSilence(t *testing.T) {
	decoder := NewDecoder()

	info, err := decoder.decodeFrameSideInfo(nil, frameConfig{
		frameSampleCount:   shortBlockSampleCount,
		startBand:          0,
		endBand:            maxBands,
		channelCount:       1,
		outputChannelCount: 1,
	}, nil)

	require.NoError(t, err)
	assert.True(t, info.silence)
	assert.Equal(t, 0, info.lm)
	assert.False(t, info.postFilter.enabled)
	assert.False(t, info.transient)
	assert.False(t, info.intraEnergy)
}

func TestDecodeLostFrameBypassesSilenceSideInfo(t *testing.T) {
	decoder := NewDecoder()
	decoder.previousLogE[0][0] = 4
	out := make([]float32, shortBlockSampleCount)

	err := decoder.Decode(nil, out, false, 1, shortBlockSampleCount, 0, maxBands)

	require.NoError(t, err)
	assert.Equal(t, float32(2.5), decoder.previousLogE[0][0])
	assert.Equal(t, float32(2.5), decoder.previousLogE[1][0])
	assert.Zero(t, decoder.FinalRange())
	assert.Equal(t, 1, decoder.lossCount)
}

func TestDecodeSynthesizesNonSilenceFrame(t *testing.T) {
	decoder := NewDecoder()
	out := make([]float32, shortBlockSampleCount)

	err := decoder.Decode(make([]byte, 8), out, false, 1, shortBlockSampleCount, 0, maxBands)

	require.NoError(t, err)
	assert.NotZero(t, vectorEnergy(out))
	assert.NotZero(t, decoder.FinalRange())
	assert.Zero(t, decoder.lossCount)
}

func TestDecodeWithRangeUsesSharedDecoder(t *testing.T) {
	decoder := NewDecoder()
	out := make([]float32, shortBlockSampleCount)
	shared := rangecoding.Decoder{}
	shared.Init(make([]byte, 8))

	err := decoder.DecodeWithRange(
		make([]byte, 8),
		out,
		false,
		1,
		shortBlockSampleCount,
		0,
		maxBands,
		&shared,
	)

	require.NoError(t, err)
	assert.NotZero(t, vectorEnergy(out))
	assert.Equal(t, decoder.FinalRange(), shared.FinalRange())
}

func TestDecodeFrameSideInfoAllDefaultFlags(t *testing.T) {
	decoder := NewDecoder()

	info, err := decoder.decodeFrameSideInfo(make([]byte, 8), frameConfig{
		frameSampleCount:   shortBlockSampleCount << 1,
		startBand:          0,
		endBand:            maxBands,
		channelCount:       2,
		outputChannelCount: 2,
	}, nil)

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
		frameSampleCount:   shortBlockSampleCount << 1,
		startBand:          0,
		endBand:            maxBands,
		channelCount:       2,
		outputChannelCount: 2,
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

func TestDecodeCoarseEnergy(t *testing.T) {
	t.Run("decodes a Laplace-coded inter energy delta", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder.SetInternalValues(nil, 0, 1<<31, 32767<<16)
		decoder.previousLogE[0][0] = 4
		info := frameSideInfo{
			lm:           1,
			totalBits:    256,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
		}

		decoder.decodeCoarseEnergy(&info)

		assert.InDelta(t, 3.1875, decoder.previousLogE[0][0], 0.000001)
		assert.Equal(t, decoder.previousLogE, info.coarseEnergy)
	})

	t.Run("mono history uses and preserves the louder previous channel", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.previousLogE[0][0] = 1
		decoder.previousLogE[1][0] = 4
		info := frameSideInfo{
			lm:           0,
			totalBits:    0,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
		}

		decoder.prepareCoarseEnergyHistory(&info)
		decoder.decodeCoarseEnergy(&info)

		assert.Equal(t, float32(2.59375), decoder.previousLogE[0][0])
		assert.Equal(t, float32(2.59375), decoder.previousLogE[1][0])
		assert.Equal(t, decoder.previousLogE, info.coarseEnergy)
	})

	t.Run("stereo preserves untouched bands and independent channel prediction", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.previousLogE[0][0] = 5
		decoder.previousLogE[1][0] = 6
		decoder.previousLogE[0][1] = 4
		decoder.previousLogE[1][1] = 8
		decoder.previousLogE[0][2] = 7
		decoder.previousLogE[1][2] = 9
		info := frameSideInfo{
			lm:           0,
			totalBits:    0,
			startBand:    1,
			endBand:      2,
			channelCount: 2,
		}

		decoder.decodeCoarseEnergy(&info)

		assert.Equal(t, float32(5), decoder.previousLogE[0][0])
		assert.Equal(t, float32(6), decoder.previousLogE[1][0])
		assert.Equal(t, float32(2.59375), decoder.previousLogE[0][1])
		assert.Equal(t, float32(6.1875), decoder.previousLogE[1][1])
		assert.Equal(t, float32(7), decoder.previousLogE[0][2])
		assert.Equal(t, float32(9), decoder.previousLogE[1][2])
		assert.Equal(t, decoder.previousLogE, info.coarseEnergy)
	})

	t.Run("intra mode ignores previous frame energy", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.previousLogE[0][0] = 7
		info := frameSideInfo{
			lm:           2,
			totalBits:    0,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
			intraEnergy:  true,
		}

		decoder.decodeCoarseEnergy(&info)

		assert.Equal(t, float32(-1), decoder.previousLogE[0][0])
		assert.Equal(t, decoder.previousLogE, info.coarseEnergy)
	})

	t.Run("uses bounded one-bit fallback near the end of the frame", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{
			lm:           0,
			totalBits:    decoder.rangeDecoder.Tell() + 1,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
		}

		decoder.decodeCoarseEnergy(&info)

		assert.Equal(t, float32(-1), decoder.previousLogE[0][0])
		assert.Equal(t, decoder.previousLogE, info.coarseEnergy)
	})

	t.Run("uses small-energy icdf when two bits remain", func(t *testing.T) {
		for _, test := range []struct {
			name          string
			uniformSymbol uint32
			wantEnergy    float32
		}{
			{name: "zero delta", uniformSymbol: 0, wantEnergy: 0},
			{name: "negative delta", uniformSymbol: 2, wantEnergy: -1},
			{name: "positive delta", uniformSymbol: 3, wantEnergy: 1},
		} {
			t.Run(test.name, func(t *testing.T) {
				decoder := NewDecoder()
				decoder.rangeDecoder = rangeDecoderWithSmallEnergyCDFSymbol(test.uniformSymbol)
				info := frameSideInfo{
					lm:           0,
					totalBits:    decoder.rangeDecoder.Tell() + 2,
					startBand:    0,
					endBand:      1,
					channelCount: 1,
				}

				decoder.decodeCoarseEnergy(&info)

				assert.Equal(t, test.wantEnergy, decoder.previousLogE[0][0])
				assert.Equal(t, decoder.previousLogE, info.coarseEnergy)
			})
		}
	})
}

func TestDecodeTimeFrequencyChanges(t *testing.T) {
	t.Run("decodes non-transient tf_change and tf_select", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{
			lm:           1,
			totalBits:    256,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
		}

		decoder.decodeTimeFrequencyChanges(&info)

		assert.Equal(t, 1, info.tfSelect)
		assert.Equal(t, -2, info.tfChange[0])
	})

	t.Run("decodes transient tf_change and tf_select", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{
			lm:           2,
			totalBits:    256,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
			transient:    true,
		}

		decoder.decodeTimeFrequencyChanges(&info)

		assert.Equal(t, 1, info.tfSelect)
		assert.Equal(t, -1, info.tfChange[0])
	})

	t.Run("maps default transient changes when budget is exhausted", func(t *testing.T) {
		decoder := NewDecoder()
		info := frameSideInfo{
			lm:           3,
			totalBits:    0,
			startBand:    0,
			endBand:      2,
			channelCount: 1,
			transient:    true,
		}

		decoder.decodeTimeFrequencyChanges(&info)

		assert.Zero(t, info.tfSelect)
		assert.Equal(t, 3, info.tfChange[0])
		assert.Equal(t, 3, info.tfChange[1])
	})
}

func TestDecodeSpread(t *testing.T) {
	t.Run("decodes spread decision when enough bits remain", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithCDFSymbol(31, 32)
		info := frameSideInfo{totalBits: 256}

		decoder.decodeSpread(&info)

		assert.Equal(t, 3, info.spread)
	})

	t.Run("defaults to normal spread without enough bits", func(t *testing.T) {
		decoder := NewDecoder()
		info := frameSideInfo{totalBits: 0}

		decoder.decodeSpread(&info)

		assert.Equal(t, defaultSpreadDecision, info.spread)
	})
}

func TestDecodeDynamicAllocation(t *testing.T) {
	t.Run("decodes boosts until the band cap", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryOne()
		info := frameSideInfo{
			lm:           0,
			totalBits:    256,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
		}
		totalBitsEighth := info.totalBits << bitResolution

		remaining := decoder.decodeDynamicAllocation(&info, totalBitsEighth)

		assert.Equal(t, 72, info.bandBoost[0])
		assert.Less(t, remaining, totalBitsEighth)
	})

	t.Run("stops immediately on a zero boost flag", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithBinaryZero()
		info := frameSideInfo{
			lm:           0,
			totalBits:    256,
			startBand:    0,
			endBand:      1,
			channelCount: 1,
		}
		totalBitsEighth := info.totalBits << bitResolution

		remaining := decoder.decodeDynamicAllocation(&info, totalBitsEighth)

		assert.Zero(t, info.bandBoost[0])
		assert.Equal(t, totalBitsEighth, remaining)
	})
}

func TestDecodeAllocationTrim(t *testing.T) {
	t.Run("decodes trim when six bits remain", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithCDFSymbol(87, 128)
		info := frameSideInfo{}

		decoder.decodeAllocationTrim(&info, uint(256)<<bitResolution)

		assert.Equal(t, 6, info.allocationTrim)
	})

	t.Run("defaults when six bits are unavailable", func(t *testing.T) {
		decoder := NewDecoder()
		decoder.rangeDecoder = rangeDecoderWithCDFSymbol(87, 128)
		info := frameSideInfo{}

		decoder.decodeAllocationTrim(&info, decoder.rangeDecoder.TellFrac()+47)

		assert.Equal(t, defaultAllocationTrim, info.allocationTrim)
	})
}

func rangeDecoderWithBinaryOne() rangecoding.Decoder {
	decoder := rangecoding.Decoder{}
	decoder.SetInternalValues(nil, 40, 1<<31, 0)

	return decoder
}

func rangeDecoderWithBinaryZero() rangecoding.Decoder {
	decoder := rangecoding.Decoder{}
	decoder.SetInternalValues(nil, 40, 1<<31, (1<<31)-1)

	return decoder
}

func rangeDecoderWithSmallEnergyCDFSymbol(symbol uint32) rangecoding.Decoder {
	return rangeDecoderWithCDFSymbol(symbol, 4)
}

func rangeDecoderWithCDFSymbol(symbol, total uint32) rangecoding.Decoder {
	const scale = 1 << 24

	decoder := rangecoding.Decoder{}
	decoder.SetInternalValues(nil, 0, total*scale, (total-symbol-1)*scale)

	return decoder
}
