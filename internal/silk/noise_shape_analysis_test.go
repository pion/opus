// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoiseShapeAnalysisAndProcessGainsVoiced runs the full analysis chain
// (findPitchLags -> noiseShapeAnalysis -> processGains) for a periodic
// (voiced) signal, using findPitchLags's own residual as noiseShapeAnalysis's
// pitchRes — the same wiring encode_frame_FLP.c uses, and a regression check
// for the pitchRes/shapeBuf mixup this test also exists to catch.
func TestNoiseShapeAnalysisAndProcessGainsVoiced(t *testing.T) {
	const (
		fsKHz        = 16
		nbSubfr      = 4
		subfrLength  = 5 * fsKHz
		laShape      = 3 * fsKHz
		frameLength  = nbSubfr * subfrLength
		ltpMemLength = 20 * fsKHz
		period       = 100
	)

	analysisBuf := make([]float32, ltpMemLength+frameLength)
	for i := range analysisBuf {
		analysisBuf[i] = float32(3000 * math.Sin(2*math.Pi*float64(i)/period))
	}

	enc := NewEncoder()
	enc.ltpCorr = 0.6
	voiced, pitchL, _, _, res, predGain := enc.findPitchLags(analysisBuf, fsKHz, nbSubfr, 200, 0)
	require.True(t, voiced, "periodic signal should be voiced")

	shapeBuf := make([]float32, frameLength+2*laShape)
	for i := range shapeBuf {
		shapeBuf[i] = float32(3000 * math.Sin(2*math.Pi*float64(i)/period))
	}
	pitchRes := res[ltpMemLength:]
	qualityBands := [vadNBands]int32{20000, 20000, 15000, 15000}

	sr := enc.noiseShapeAnalysis(
		shapeBuf, pitchRes, frameSignalTypeVoiced, pitchL, predGain, 20*128, 200, qualityBands, fsKHz, nbSubfr, subfrLength)

	require.NotNil(t, sr)
	require.Len(t, sr.gains, nbSubfr)
	for k, g := range sr.gains {
		assert.Greaterf(t, g, float32(0), "gain %d", k)
		assert.Falsef(t, math.IsNaN(float64(g)), "gain %d", k)
	}
	require.Len(t, sr.arQ13, nbSubfr*maxShapeLPCOrder)
	require.Len(t, sr.tiltQ14, nbSubfr)
	require.Len(t, sr.lfShpQ14, nbSubfr)
	require.Len(t, sr.harmShapeQ14, nbSubfr)
	// Voiced frames leave the sparseness measure untouched — quantOffset
	// starts Low and processGains decides the real value below.
	assert.Equal(t, frameQuantizationOffsetTypeLow, sr.quantOffset)

	resNrg := make([]float32, nbSubfr)
	for k := range resNrg {
		resNrg[k] = 500000
	}

	gainsQ16Int, gainIndices, lambdaQ10, quantOffset := enc.processGains(
		sr, resNrg, frameSignalTypeVoiced, 0.6, 20*128, 200, 0, subfrLength, nbSubfr, false)

	require.Len(t, gainsQ16Int, nbSubfr)
	require.Len(t, gainIndices, nbSubfr)
	for k, g := range gainsQ16Int {
		assert.Positivef(t, g, "gainsQ16Int %d", k)
	}
	assert.Positive(t, lambdaQ10)
	assert.Contains(t,
		[]frameQuantizationOffsetType{frameQuantizationOffsetTypeLow, frameQuantizationOffsetTypeHigh}, quantOffset)
}

// TestNoiseShapeAnalysisUnvoicedSparseness exercises the unvoiced sparseness
// measure directly: a bursty residual (high energy variation between 2ms
// segments) must pick the Low offset, a smooth one the High offset — this is
// exactly the branch that used to read the wrong buffer (shapeBuf instead of
// pitchRes).
func TestNoiseShapeAnalysisUnvoicedSparseness(t *testing.T) {
	const (
		fsKHz       = 16
		nbSubfr     = 4
		subfrLength = 5 * fsKHz
		laShape     = 3 * fsKHz
		frameLength = nbSubfr * subfrLength
	)
	qualityBands := [vadNBands]int32{20000, 20000, 15000, 15000}
	shapeBuf := make([]float32, frameLength+2*laShape)
	pitchL := make([]int, nbSubfr)

	bursty := make([]float32, frameLength)
	for i := range bursty {
		if (i/(2*fsKHz))%2 == 0 {
			bursty[i] = 5000
		}
	}
	enc := NewEncoder()
	srBursty := enc.noiseShapeAnalysis(
		shapeBuf, bursty, frameSignalTypeUnvoiced, pitchL, 1.0, 20*128, 200, qualityBands, fsKHz, nbSubfr, subfrLength)
	assert.Equal(t, frameQuantizationOffsetTypeLow, srBursty.quantOffset, "bursty residual: expected Low")

	flat := make([]float32, frameLength)
	for i := range flat {
		flat[i] = float32(500 * math.Sin(2*math.Pi*float64(i)/23))
	}
	enc2 := NewEncoder()
	srFlat := enc2.noiseShapeAnalysis(
		shapeBuf, flat, frameSignalTypeUnvoiced, pitchL, 1.0, 20*128, 200, qualityBands, fsKHz, nbSubfr, subfrLength)
	assert.Equal(t, frameQuantizationOffsetTypeHigh, srFlat.quantOffset, "flat residual: expected High")
}
