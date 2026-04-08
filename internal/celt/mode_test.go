// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMode(t *testing.T) {
	mode := DefaultMode()

	assert.Equal(t, sampleRate, mode.SampleRate())
	assert.Equal(t, shortBlockSampleCount, mode.ShortBlockSampleCount())
	assert.Equal(t, maxLM, mode.MaxLM())
	assert.Equal(t, maxBands, mode.BandCount())
}

func TestFrameSampleCount(t *testing.T) {
	mode := DefaultMode()

	for _, test := range []struct {
		name       string
		lm         int
		sampleSize int
	}{
		{name: "2.5ms", lm: 0, sampleSize: 120},
		{name: "5ms", lm: 1, sampleSize: 240},
		{name: "10ms", lm: 2, sampleSize: 480},
		{name: "20ms", lm: 3, sampleSize: 960},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := mode.FrameSampleCount(test.lm)
			require.NoError(t, err)
			assert.Equal(t, test.sampleSize, got)

			lm, err := mode.LMForFrameSampleCount(test.sampleSize)
			require.NoError(t, err)
			assert.Equal(t, test.lm, lm)
		})
	}

	_, err := mode.FrameSampleCount(4)
	assert.ErrorIs(t, err, errInvalidLM)
	_, err = mode.LMForFrameSampleCount(720)
	assert.ErrorIs(t, err, errInvalidFrameSize)
}

func TestBandEdges(t *testing.T) {
	mode := DefaultMode()

	edges, err := mode.BandEdges(0)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 16, 20, 24, 28, 32, 40, 48, 60, 78, 100}, edges)

	edges, err = mode.BandEdges(3)
	require.NoError(t, err)
	assert.Equal(t, 0, edges[0])
	assert.Equal(t, 800, edges[len(edges)-1])

	width, err := mode.BandWidth(20, 3)
	require.NoError(t, err)
	assert.Equal(t, 176, width)
}

func TestBandRangeForSampleRate(t *testing.T) {
	mode := DefaultMode()

	for _, test := range []struct {
		name       string
		sampleRate int
		endBand    int
	}{
		{name: "narrowband", sampleRate: 8000, endBand: 13},
		{name: "wideband", sampleRate: 16000, endBand: 17},
		{name: "superwideband", sampleRate: 24000, endBand: 19},
		{name: "fullband", sampleRate: 48000, endBand: 21},
	} {
		t.Run(test.name, func(t *testing.T) {
			startBand, endBand, err := mode.BandRangeForSampleRate(test.sampleRate)
			require.NoError(t, err)
			assert.Equal(t, 0, startBand)
			assert.Equal(t, test.endBand, endBand)
		})
	}

	_, _, err := mode.BandRangeForSampleRate(12000)
	assert.ErrorIs(t, err, errInvalidSampleRate)
}

func TestHybridBandRange(t *testing.T) {
	startBand, endBand, err := DefaultMode().HybridBandRange(48000)
	require.NoError(t, err)

	assert.Equal(t, hybridStartBand, startBand)
	assert.Equal(t, maxBands, endBand)

	_, _, err = DefaultMode().HybridBandRange(16000)
	assert.ErrorIs(t, err, errInvalidSampleRate)
}

func TestStaticTables(t *testing.T) {
	assert.Len(t, bandEdges, maxBands+1)
	assert.Equal(t, int16(0), bandEdges[0])
	assert.Equal(t, int16(100), bandEdges[len(bandEdges)-1])

	assert.Len(t, bandAllocation, maxBands)
	assert.Equal(t, uint8(90), bandAllocation[1][0])
	assert.Equal(t, uint8(188), bandAllocation[20][10])

	assert.Equal(t, []uint{32768, 32767, 32768}, icdfSilence)
	assert.Equal(t, []uint{32, 7, 9, 30, 32}, icdfSpread)
}

func TestDecoderReset(t *testing.T) {
	decoder := NewDecoder()
	decoder.previousLogE[0][0] = 1
	decoder.overlap[0][0] = 1

	decoder.Reset()

	assert.Zero(t, decoder.previousLogE[0][0])
	assert.Zero(t, decoder.overlap[0][0])
	assert.Equal(t, sampleRate, decoder.Mode().SampleRate())
}
