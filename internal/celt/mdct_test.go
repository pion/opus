// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT
package celt

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardComplexDFTRoundTrip(t *testing.T) {
	input := []complex32{
		{r: 1.0, i: 0.0},
		{r: -0.25, i: 0.75},
		{r: 0.5, i: -0.5},
		{r: 0.125, i: 0.25},
	}
	forward := forwardComplexDFT(input)
	recovered := inverseComplexDFT(forward)
	require.Len(t, recovered, len(input))
	assertComplexSliceClose(t, input, recovered, 1e-4)
}

// TestForwardMDCTPreservesPlainRegion verifies the analysis-side MDCT
// produces a frequency representation that, when inverted, recovers the
// unwindowed (middle) portion of the input time signal.
//
// This is the practical TDAC property: the plain region between the two
// windowed overlap halves must roundtrip exactly. The windowed edges are not
// reconstructed by a single frame — overlap-add with neighboring frames
// completes them in the full pipeline.
func TestForwardMDCTPreservesPlainRegion(t *testing.T) {
	testCases := []int{
		shortBlockSampleCount,
		shortBlockSampleCount << 1,
		shortBlockSampleCount << 2,
		shortBlockSampleCount << 3,
	}
	for _, frameSampleCount := range testCases {
		t.Run(frameSampleCountName(frameSampleCount), func(t *testing.T) {
			time := make([]float32, frameSampleCount+shortBlockSampleCount)
			for i := range time {
				time[i] = float32(math.Sin(2*math.Pi*7*float64(i)/float64(frameSampleCount)) +
					0.25*math.Cos(2*math.Pi*23*float64(i)/float64(frameSampleCount)))
			}
			freq := forwardMDCT(time)
			require.Len(t, freq, frameSampleCount)
			recovered := inverseMDCT(freq)
			require.Len(t, recovered, frameSampleCount+shortBlockSampleCount)
			// The plain region (where neither forward nor inverse apply the
			// window) must roundtrip exactly. The forward MDCT copies
			// time[overlap..n+overlap-overlap] verbatim; sample edges within
			// overlap/2 of either boundary go through windowing and are only
			// reconstructed via TDAC overlap-add with neighboring frames.
			plainStart := shortBlockSampleCount
			plainEnd := frameSampleCount
			assertFloat32SliceClose(t, time[plainStart:plainEnd],
				recovered[plainStart:plainEnd], 1e-3)
		})
	}
}

func TestForwardMDCTZero(t *testing.T) {
	freq := make([]float32, shortBlockSampleCount<<1)
	time := inverseMDCT(freq)
	recovered := forwardMDCT(time)
	require.Len(t, recovered, len(freq))
	assertFloat32SliceClose(t, freq, recovered, 1e-7)
}

func TestForwardMDCTInvalidInput(t *testing.T) {
	assert.Nil(t, forwardMDCT(nil))
	assert.Nil(t, forwardMDCT(make([]float32, shortBlockSampleCount)))
	assert.Nil(t, forwardMDCT(make([]float32, shortBlockSampleCount+1)))
}

func assertFloat32SliceClose(t *testing.T, expected, actual []float32, tolerance float64) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		assert.InDelta(t, expected[i], actual[i], tolerance, "index %d", i)
	}
}

func assertComplexSliceClose(t *testing.T, expected, actual []complex32, tolerance float64) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		assert.InDelta(t, expected[i].r, actual[i].r, tolerance, "real index %d", i)
		assert.InDelta(t, expected[i].i, actual[i].i, tolerance, "imag index %d", i)
	}
}

func frameSampleCountName(frameSampleCount int) string {
	return "frame_" + strconv.Itoa(frameSampleCount)
}
