// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestQuantBandMonoPreservesDirectionWithShortBlocks guards the Hadamard
// transform pair in quantBandMono. The encoder used to interleave x back at
// the end without ever deinterleaving it first (libopus does both under
// "if (encode)", celt/bands.c:1322), which left the band permuted: energy
// survived but the direction did not, so PVQ quantized a scrambled vector.
//
// FinalRange comparisons cannot catch this — encoder and decoder still agree
// on every bit, they just agree on the wrong content. Measuring the angle
// between the shape handed to quantBandMono and the shape it produces is
// what exposes it: short blocks used to collapse to ~0 correlation while
// long blocks stayed above 0.7.
func TestQuantBandMonoPreservesDirectionWithShortBlocks(t *testing.T) {
	for _, tc := range []struct{ band, bandBits int }{
		{8, 251}, {12, 408}, {18, 807}, {19, 884},
	} {
		n := (int(bandEdges[tc.band+1]) - int(bandEdges[tc.band])) << maxLM
		longCorr := quantBandMonoMeanCorrelation(t, tc.band, n, tc.bandBits, 1)
		shortCorr := quantBandMonoMeanCorrelation(t, tc.band, n, tc.bandBits, 1<<maxLM)

		assert.Greaterf(t, shortCorr, 0.6,
			"band %d: short blocks lost the band direction (corr=%.3f)", tc.band, shortCorr)
		assert.InDeltaf(t, longCorr, shortCorr, 0.1,
			"band %d: short blocks (corr=%.3f) must quantize as faithfully as long blocks (corr=%.3f)",
			tc.band, shortCorr, longCorr)

		// The recombine and time-divide loops and the folded deinterleave all
		// run haar1 on the band, so each needs the forward pass too.
		divided := quantBandMonoCorrelation(t, tc.band, n, tc.bandBits, 1, -1, false)
		assert.Greaterf(t, divided, 0.6,
			"band %d: time-divided blocks lost the band direction (corr=%.3f)", tc.band, divided)

		folded := quantBandMonoCorrelation(t, tc.band, n, tc.bandBits, 1<<maxLM, 0, true)
		assert.Greaterf(t, folded, 0.6,
			"band %d: folding lost the band direction (corr=%.3f)", tc.band, folded)
	}
}

// quantBandMonoMeanCorrelation returns the mean cosine similarity between a
// random unit-norm band and the shape quantBandMono leaves behind.
func quantBandMonoMeanCorrelation(t *testing.T, band, n, bandBits, blocks int) float64 {
	t.Helper()

	return quantBandMonoCorrelation(t, band, n, bandBits, blocks, 0, false)
}

// quantBandMonoCorrelation also drives the time-divide (tfChange < 0) and
// folding (lowband != nil) branches, which the encoder cannot reach on its
// own until tf_analysis lands.
func quantBandMonoCorrelation(t *testing.T, band, n, bandBits, blocks, tfChange int, withLowband bool) float64 {
	t.Helper()

	const trials = 20
	var total float64
	for trial := range trials {
		rng := rand.New(rand.NewSource(int64(band*1000 + blocks*97 + trial))) //nolint:gosec // deterministic input
		shape := make([]float32, n)
		for i := range shape {
			shape[i] = float32(rng.NormFloat64())
		}
		renormaliseVector(shape, n, normScaling)
		target := make([]float32, n)
		copy(target, shape)

		encoder := NewEncoder()
		encoder.rangeEncoder.Init()
		state := bandEncodeState{rangeEncoder: &encoder.rangeEncoder}
		remainingBits := 100000

		var lowband []float32
		if withLowband {
			lowband = make([]float32, n)
			for i := range lowband {
				lowband[i] = float32(rng.NormFloat64())
			}
			renormaliseVector(lowband, n, normScaling)
		}

		quantBandMono(
			band, shape, n, bandBits, spreadNormal, blocks, tfChange,
			lowband, &remainingBits, maxLM, nil, 0, 1, make([]float32, n), (1<<blocks)-1, &state,
			make([]int, n+3), make([]float32, n+3), make([]float32, n+3),
			make([]uint32, cwrsMaxPulseCount+2),
		)

		var dot, normTarget, normQuantized float64
		for i := range n {
			dot += float64(target[i]) * float64(shape[i])
			normTarget += float64(target[i]) * float64(target[i])
			normQuantized += float64(shape[i]) * float64(shape[i])
		}
		if normTarget == 0 || normQuantized == 0 {
			continue
		}
		total += dot / (math.Sqrt(normTarget) * math.Sqrt(normQuantized))
	}

	return total / trials
}
