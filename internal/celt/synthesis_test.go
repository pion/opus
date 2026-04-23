// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmoothFade(t *testing.T) {
	in1 := []float32{1, 2, 3, 4}
	in2 := []float32{5, 6, 7, 8}
	out := make([]float32, len(in1))

	SmoothFade(in1, in2, out, 2, 2)

	w0 := celtWindow120[0] * celtWindow120[0]
	w1 := celtWindow120[1] * celtWindow120[1]
	assert.InDelta(t, w0*5+(1-w0)*1, out[0], 0.000001)
	assert.InDelta(t, w0*6+(1-w0)*2, out[1], 0.000001)
	assert.InDelta(t, w1*7+(1-w1)*3, out[2], 0.000001)
	assert.InDelta(t, w1*8+(1-w1)*4, out[3], 0.000001)
}

func TestLog2AmpAndDenormaliseBands(t *testing.T) {
	decoder := NewDecoder()
	info := frameSideInfo{
		startBand:    0,
		endBand:      2,
		channelCount: 1,
	}
	decoder.previousLogE[0][0] = 0
	decoder.previousLogE[0][1] = 40

	energy := decoder.log2Amp(&info)

	assert.InDelta(t, math.Pow(2, float64(energyMeans[0])), energy[0][0], 0.000001)
	assert.Equal(t, float32(math.Pow(2, 32)), energy[0][1])

	x := []float32{1, 2}
	freq := make([]float32, len(x))
	denormaliseBands(&frameSideInfo{lm: 0, startBand: 0, endBand: 2}, x, freq, [maxBands]float32{
		2,
		3,
	})

	assert.Equal(t, []float32{2, 6}, freq)
	assert.Equal(t, float32(-1), minFloat32(-1, 2))
	assert.Equal(t, float32(-1), minFloat32(2, -1))
}

func TestDenormaliseAndSynthesizeLayouts(t *testing.T) {
	tests := []struct {
		name               string
		channelCount       int
		outputChannelCount int
		lm                 int
		transient          bool
		postFilter         postFilter
	}{
		{
			name:               "mono to mono",
			channelCount:       1,
			outputChannelCount: 1,
		},
		{
			name:               "mono to stereo transient",
			channelCount:       1,
			outputChannelCount: 2,
			lm:                 1,
			transient:          true,
			postFilter: postFilter{
				enabled: true,
				period:  combFilterMinPeriod,
				gain:    postFilterGainStep,
				tapset:  1,
			},
		},
		{
			name:               "stereo to mono",
			channelCount:       2,
			outputChannelCount: 1,
		},
		{
			name:               "stereo to stereo transient",
			channelCount:       2,
			outputChannelCount: 2,
			lm:                 1,
			transient:          true,
			postFilter: postFilter{
				enabled: true,
				period:  combFilterMinPeriod + 1,
				gain:    2 * postFilterGainStep,
				tapset:  2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := NewDecoder()
			frameSampleCount := shortBlockSampleCount << tt.lm
			info := frameSideInfo{
				lm:                 tt.lm,
				startBand:          0,
				endBand:            2,
				channelCount:       tt.channelCount,
				outputChannelCount: tt.outputChannelCount,
				transient:          tt.transient,
				postFilter:         tt.postFilter,
			}
			leftSpectrum := make([]float32, frameSampleCount)
			leftSpectrum[0] = 1
			var rightSpectrum []float32
			if tt.channelCount == 2 {
				rightSpectrum = make([]float32, frameSampleCount)
				rightSpectrum[1] = -1
			}
			var energy [2][maxBands]float32
			energy[0][0] = 1
			energy[0][1] = 1
			energy[1][0] = 1
			energy[1][1] = 1
			out := make([]float32, frameSampleCount*tt.outputChannelCount)

			decoder.denormaliseAndSynthesize(&info, leftSpectrum, rightSpectrum, energy, out)

			require.Len(t, out, frameSampleCount*tt.outputChannelCount)
			assert.NotZero(t, vectorEnergy(out))
			if !tt.transient {
				assert.NotZero(t, vectorEnergy(decoder.overlap[0]))
				if tt.outputChannelCount == 2 {
					assert.NotZero(t, vectorEnergy(decoder.overlap[1]))
				}
			}
		})
	}
}

func TestAntiCollapseFillsEmptyTransientBlocks(t *testing.T) {
	decoder := NewDecoder()
	info := frameSideInfo{
		lm:           1,
		startBand:    0,
		endBand:      1,
		channelCount: 1,
	}
	x := make([]float32, shortBlockSampleCount<<info.lm)
	collapseMasks := make([]byte, maxBands)

	decoder.antiCollapse(&info, x, nil, collapseMasks, 1)

	assert.InDelta(t, 1, vectorEnergy(x[:2]), 0.000001)
	assert.Equal(t, x[0], -x[1])
}

func TestUpdateLogEHistoryAndInactiveBands(t *testing.T) {
	decoder := NewDecoder()
	info := frameSideInfo{
		startBand:    1,
		endBand:      3,
		channelCount: 1,
	}
	decoder.previousLogE[0][1] = 2
	decoder.previousLogE[0][2] = 3
	decoder.previousLogE1[0][1] = 1
	decoder.previousLogE2[0][1] = 0

	decoder.updateLogEHistory(&info)
	decoder.resetInactiveBandState(&info)

	assert.Equal(t, decoder.previousLogE[0], decoder.previousLogE[1])
	assert.Equal(t, float32(2), decoder.previousLogE1[0][1])
	assert.Equal(t, float32(1), decoder.previousLogE2[0][1])
	assert.Zero(t, decoder.previousLogE[0][0])
	assert.Equal(t, float32(-28), decoder.previousLogE1[0][0])
	assert.Equal(t, float32(-28), decoder.previousLogE2[0][maxBands-1])

	info.transient = true
	decoder.previousLogE[0][1] = -4
	decoder.previousLogE1[0][1] = -2
	decoder.updateLogEHistory(&info)
	assert.Equal(t, float32(-4), decoder.previousLogE1[0][1])
}

func TestPostfilterHelpers(t *testing.T) {
	decoder := NewDecoder()
	decoder.postfilter = postFilterState{
		period:    combFilterMinPeriod,
		oldPeriod: combFilterMinPeriod,
		gain:      postFilterGainStep,
		oldGain:   postFilterGainStep,
		tapset:    1,
		oldTapset: 0,
	}
	for i := range decoder.postfilterMem[0] {
		decoder.postfilterMem[0][i] = float32(i%7) / 7
	}
	time := make([]float32, shortBlockSampleCount<<1)
	for i := range time {
		time[i] = float32(i%5) / 5
	}
	info := frameSideInfo{
		lm: 1,
		postFilter: postFilter{
			enabled: true,
			period:  combFilterMinPeriod + 2,
			gain:    2 * postFilterGainStep,
			tapset:  2,
		},
	}

	before := append([]float32(nil), time...)
	decoder.applyPostfilter(&info, time, 0)
	decoder.updatePostfilterState(&info)

	assert.NotEqual(t, before, time)
	assert.Equal(t, combFilterMinPeriod+2, decoder.postfilter.period)
	assert.Equal(t, decoder.postfilter.period, decoder.postfilter.oldPeriod)
	assert.Equal(t, postFilterState{}, currentPostfilter(&frameSideInfo{}))
	assert.Equal(t, postFilterState{
		period: combFilterMinPeriod + 2,
		gain:   2 * postFilterGainStep,
		tapset: 2,
	}, currentPostfilter(&info))
}

func TestInverseMDCTAndDeemphasisHelpers(t *testing.T) {
	freq := make([]float32, shortBlockSampleCount)
	freq[0] = 1
	time := inverseMDCT(freq)
	assert.Len(t, time, shortBlockSampleCount*2)
	assert.NotZero(t, vectorEnergy(time))

	dft := inverseComplexDFT([]complex32{{r: 1}, {i: 1}})
	assert.Len(t, dft, 2)
	assert.Equal(t, celtWindow120[0], celtWindow(0))

	decoder := NewDecoder()
	out := make([]float32, 4)
	decoder.deemphasisAndInterleave([]float32{32768, 0}, []float32{16384, 0}, out, 2, 2)
	assert.Equal(t, float32(1), out[0])
	assert.Equal(t, float32(0.5), out[1])
	assert.NotZero(t, decoder.preemphasisMem[0])
	assert.NotZero(t, decoder.preemphasisMem[1])
}

func TestDecodeLostFrameClearsPostfilterHistory(t *testing.T) {
	decoder := NewDecoder()
	decoder.postfilterMem[0][0] = 1
	decoder.postfilterMem[1][0] = 2
	out := make([]float32, shortBlockSampleCount)

	err := decoder.Decode(nil, out, false, 1, shortBlockSampleCount, 0, maxBands)

	require.NoError(t, err)
	assert.Zero(t, vectorEnergy(decoder.postfilterMem[0]))
	assert.Zero(t, vectorEnergy(decoder.postfilterMem[1]))
}
