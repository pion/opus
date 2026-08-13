// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThetaRDORoundTrip drives the rate-distortion search, which encodes every
// stereo band twice and rewinds the range coder, the band and the noise-fill
// seed in between. An incomplete rewind desynchronises the range coder, so the
// final-range check is what actually guards the search.
func TestThetaRDORoundTrip(t *testing.T) {
	encoder := NewEncoder()
	encoder.SetComplexity(thetaRDOComplexity)
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	left := make([]float32, frameSampleCount)
	right := make([]float32, frameSampleCount)
	phase := 0.0
	out := make([]float32, frameSampleCount*2)
	for frame := range 5 {
		for i := range frameSampleCount {
			// Both channels move so the angle lands somewhere different every
			// frame; a fixed pair would only ever exercise one rounding.
			left[i] = float32(math.Sin(phase) + 0.5*math.Sin(phase*3.7))
			right[i] = float32(math.Sin(phase*1.03+0.4) - 0.3*math.Sin(phase*7.1))
			phase += 2 * math.Pi * 500 / sampleRate
		}
		data := encodeFrame(t, &encoder, [][]float32{left, right}, 240)
		require.NotEmptyf(t, data, "frame %d", frame)
		require.NoErrorf(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands),
			"frame %d", frame)
		assert.Equalf(t, encoder.FinalRange(), decoder.FinalRange(),
			"range coder out of sync at frame %d", frame)
	}
}

// TestThetaRDOChangesTheBitstream pins the gate from the other side: below the
// threshold the encoder must take the plain path, so the two bitstreams have to
// differ. Without this the search could silently no-op and every other test
// here would still pass.
func TestThetaRDOChangesTheBitstream(t *testing.T) {
	frameSampleCount := shortBlockSampleCount << maxLM
	left := make([]float32, frameSampleCount)
	right := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		left[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		right[i] = float32(math.Sin(2 * math.Pi * 660 * float64(i) / sampleRate))
	}

	plain := NewEncoder()
	plain.SetComplexity(thetaRDOComplexity - 1)
	searched := NewEncoder()
	searched.SetComplexity(thetaRDOComplexity)

	plainData := encodeFrame(t, &plain, [][]float32{left, right}, 240)
	searchedData := encodeFrame(t, &searched, [][]float32{left, right}, 240)

	assert.NotEqual(t, plainData, searchedData, "the search should change the chosen angles")
	assert.NotEqual(t, plain.FinalRange(), searched.FinalRange(),
		"a different set of angles must land on a different range-coder state")
}

func TestQuantizeStereoBandThetaRoundingBrackets(t *testing.T) {
	// The two candidates have to bracket the nearest-value choice: if they
	// collapsed onto the same symbol the search would compare an encode with
	// itself and never pick anything.
	x := []float32{0.9, 0.4, -0.2, 0.7, 0.1, -0.5, 0.3, 0.8}
	y := []float32{0.2, -0.6, 0.5, 0.1, -0.9, 0.4, -0.3, 0.2}

	for _, qn := range []int{2, 4, 8, 16, 32} {
		down := quantizeStereoBandTheta(x, y, qn, -1)
		up := quantizeStereoBandTheta(x, y, qn, 1)
		nearest := quantizeStereoBandTheta(x, y, qn, 0)

		assert.Equalf(t, down+1, up, "qn=%d: candidates must be adjacent", qn)
		assert.GreaterOrEqualf(t, nearest, down, "qn=%d", qn)
		assert.LessOrEqualf(t, nearest, up, "qn=%d", qn)
		assert.GreaterOrEqualf(t, down, 0, "qn=%d", qn)
		assert.LessOrEqualf(t, up, qn, "qn=%d", qn)
	}
}

func TestChannelWeightsLiftTheQuieterChannel(t *testing.T) {
	// A near-silent side must not make its own distortion irrelevant, so the
	// reference nudges both weights up by a third of the smaller energy.
	wx, wy := channelWeights(9, 3)
	assert.InDelta(t, 10.0, wx, 1e-6)
	assert.InDelta(t, 4.0, wy, 1e-6)

	// Equal energies stay equal.
	wx, wy = channelWeights(6, 6)
	assert.InDelta(t, wx, wy, 1e-6)
}

func TestBandDistortionRewardsTheCloserReconstruction(t *testing.T) {
	orig := []float32{1, 0, -1, 0}
	exact := []float32{1, 0, -1, 0}
	poor := []float32{0, 1, 0, -1}

	good := bandDistortion(orig, orig, exact, exact, 1, 1)
	bad := bandDistortion(orig, orig, poor, poor, 1, 1)
	assert.Greater(t, good, bad, "a matching reconstruction must score higher")
}
