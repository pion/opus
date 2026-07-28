// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alignedSNR returns the best SNR over a delay search with the best single gain
// removed, so the codec's constant delay and any overall level offset cannot
// show up as reconstruction error.
func alignedSNR(ref, got []float32, maxDelay int) float64 {
	best := math.Inf(-1)
	for delay := range maxDelay {
		var signal, dot, energy float64
		for i := 0; i+delay < len(got) && i < len(ref); i++ {
			r := float64(ref[i])
			g := float64(got[i+delay])
			signal += r * r
			dot += r * g
			energy += g * g
		}
		if energy == 0 || signal == 0 {
			continue
		}
		gain := dot / energy
		residual := signal - 2*gain*dot + gain*gain*energy
		if residual <= 0 {
			continue
		}
		best = math.Max(best, 10*math.Log10(signal/residual))
	}

	return best
}

// TestReconstructionWithoutQuantization runs the encoder's analysis and the
// decoder's synthesis back to back with quantization skipped: the MDCT
// coefficients are handed over with unit band energies, so denormaliseBands is
// a no-op and nothing is rounded anywhere.
//
// Any error left here is structural — windowing, overlap-add, emphasis — and no
// bitrate can remove it. The MDCT pair only reconstructs if the forward fold is
// the exact adjoint of the inverse unfold, so this catches a broken fold that
// per-band energy checks and FinalRange comparisons both miss.
func TestReconstructionWithoutQuantization(t *testing.T) {
	const (
		frameCount  = 8
		frameLength = maxFrameSampleCount
	)

	mode := DefaultMode()
	state := newAnalysisState()
	var mdctScratch forwardMDCTScratch
	var fftScratch []complex32
	decoder := &Decoder{}
	decoder.Reset()

	source := make([]float32, frameLength*frameCount)
	for i := range source {
		source[i] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/sampleRate))
	}

	var unitEnergy [2][maxBands]float32
	for band := range maxBands {
		unitEnergy[0][band] = 1
		unitEnergy[1][band] = 1
	}

	decoded := make([]float32, 0, frameLength*frameCount)
	for frame := range frameCount {
		pcm := [][]float32{source[frame*frameLength : (frame+1)*frameLength]}
		// No transient and no pre-filter: one long MDCT per frame, and no
		// post-filter needed on the way back.
		res, err := analyzeFrame(
			mode, pcm, 0, maxBands, &state, &mdctScratch, &fftScratch, false, false, 0, 0, 0)
		require.NoErrorf(t, err, "frame %d", frame)

		info := res.info
		info.outputChannelCount = 1
		info.outputSampleRate = sampleRate

		out := make([]float32, frameLength)
		decoder.denormaliseAndSynthesize(&info, res.mdct[0], nil, unitEnergy, out)
		decoded = append(decoded, out...)
	}

	// Skip the first frame: the MDCT overlap and the emphasis memories are
	// still filling there.
	snr := alignedSNR(source[frameLength:], decoded[frameLength:], 300)
	assert.Greaterf(t, snr, 40.0,
		"transform chain must reconstruct without quantization, got %.2f dB", snr)
}
