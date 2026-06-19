// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
)

const preemphasisCoefficient = 0.85000610

// transientRatioThreshold is the minimum ratio between the energy of the
// second half of the frame and the first half that the detector reports as
// a transient. It was calibrated empirically against the synthetic fixtures
// in analysis_test.go.
const transientRatioThreshold = 1.5

type analysisState struct {
	prevPCM        [2][]float32
	preemphasisMem [2]float32
	preScratch     [2][]float32
	mdctInput      [2][]float32
}

type analysisResult struct {
	info       frameSideInfo
	mdct       [2][]float32
	logBandAmp [2][maxBands]float32
}

func newAnalysisState() analysisState {
	maxFrame := shortBlockSampleCount << maxLM
	state := analysisState{
		prevPCM: [2][]float32{
			make([]float32, shortBlockSampleCount),
			make([]float32, shortBlockSampleCount),
		},
		preScratch: [2][]float32{
			make([]float32, maxFrame),
			make([]float32, maxFrame),
		},
		mdctInput: [2][]float32{
			make([]float32, shortBlockSampleCount+maxFrame),
			make([]float32, shortBlockSampleCount+maxFrame),
		},
	}

	return state
}

// analyzeFrame applies pre-emphasis, builds the MDCT overlap window, runs the
// forward MDCT, and returns per-band log amplitude for each input channel.
func analyzeFrame(
	mode *Mode, pcm [][]float32, startBand, endBand int,
	state *analysisState, mdctScratch *forwardMDCTScratch, fftScratch *[]complex32,
) (analysisResult, error) {
	lm, err := mode.LMForFrameSampleCount(len(pcm[0]))
	if err != nil {
		return analysisResult{}, err
	}

	res := analysisResult{
		info: frameSideInfo{
			lm:             lm,
			startBand:      startBand,
			endBand:        endBand,
			channelCount:   len(pcm),
			transient:      false,
			spread:         defaultSpreadDecision,
			allocationTrim: defaultAllocationTrim,
		},
	}

	for ch := range pcm {
		pre := state.preScratch[ch][:len(pcm[ch])]
		applyPreemphasis(pcm[ch], pre, &state.preemphasisMem[ch])

		mdctInput := state.mdctInput[ch][:shortBlockSampleCount+len(pre)]
		copy(mdctInput, state.prevPCM[ch])
		copy(mdctInput[shortBlockSampleCount:], pre)

		res.mdct[ch] = forwardMDCTWithScratch(mdctInput, ch, mdctScratch, fftScratch)
		if res.mdct[ch] == nil {
			return analysisResult{}, errInvalidFrameSize
		}

		res.logBandAmp[ch] = computeBandLogAmp(res.mdct[ch], lm, startBand, endBand)
		copy(state.prevPCM[ch], pre[len(pre)-shortBlockSampleCount:])
	}

	return res, nil
}

// detectTransient reports whether any channel of the input PCM contains a
// transient using a half-frame energy ratio. The bitstream still hardcodes
// transient=false in 6a; PR 6b connects this and adds the short-block path.
//
// A mid-frame impulse concentrates energy in the second half of the frame;
// a steady sine distributes it roughly evenly. A channel is transient when
// the ratio exceeds transientRatioThreshold; the frame is transient if any
// channel is. The threshold was chosen against the synthetic fixtures in
// analysis_test.go (steady sine: 0.92, stereo sine: 1.01, impulse: >>1).
//
// The libopus detector (celt_encoder.c: transient_analysis) uses a
// sub-frame STFT with 6.7 dB/ms forward masking and is more accurate on
// gradual ramps — known limitation of this simpler approach.
func detectTransient(pcm [][]float32, _ *analysisState) bool {
	return debugDetectTransient(pcm) > transientRatioThreshold
}

// debugDetectTransient returns the maximum half-frame energy ratio across
// channels. Test-only helper used to calibrate transientRatioThreshold
// against the synthetic fixtures.
func debugDetectTransient(pcm [][]float32) float64 {
	if len(pcm) == 0 || len(pcm[0]) < 4 {
		return 0
	}

	frameSize := len(pcm[0])
	half := frameSize / 2

	var maxRatio float64
	for ch := range pcm {
		// Skip the first and last shortBlockSampleCount samples: those are
		// the MDCT overlap regions shared with the neighboring frames and
		// don't belong to this frame's "clean" content.
		start := shortBlockSampleCount
		if start >= half {
			continue
		}
		end := frameSize - shortBlockSampleCount
		if end <= start {
			continue
		}

		var e1, e2 float64
		for i := start; i < half; i++ {
			x := float64(pcm[ch][i]) //nolint:gosec // bounds checked: i < half <= frameSize = len(pcm[ch])
			e1 += x * x
		}
		for i := half; i < end; i++ {
			x := float64(pcm[ch][i]) //nolint:gosec // bounds checked: i < end <= frameSize = len(pcm[ch])
			e2 += x * x
		}

		ratio := e2 / math.Max(e1, 1e-30)
		if ratio > maxRatio {
			maxRatio = ratio
		}
	}

	return maxRatio
}

func applyPreemphasis(in []float32, out []float32, mem *float32) {
	prev := *mem
	for i := range in {
		current := in[i] * 32768
		out[i] = current - preemphasisCoefficient*prev
		prev = current
	}
	*mem = prev
}

// computeBandLogAmp returns the encoder-side quantity that matches the decoder's
// previousLogE domain: log2(sqrt(sum(x^2))) minus the static mean per band.
func computeBandLogAmp(freq []float32, lm int, startBand int, endBand int) [maxBands]float32 {
	logAmp := [maxBands]float32{}
	scale := 1 << lm

	for band := startBand; band < endBand; band++ {
		bandStart := scale * int(bandEdges[band])
		bandEnd := scale * int(bandEdges[band+1])

		energy := float64(1e-27)
		for i := bandStart; i < bandEnd; i++ {
			value := float64(freq[i])
			energy += value * value
		}

		amplitude := math.Sqrt(energy)
		logAmp[band] = float32(math.Log2(amplitude)) - energyMeans[band]
	}

	return logAmp
}
