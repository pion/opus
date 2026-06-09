// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
)

const preemphasisCoefficient = 0.85000610

type analysisState struct {
	prevPCM        [2][]float32
	preemphasisMem [2]float32
}

type analysisResult struct {
	info       frameSideInfo
	mdct       [2][]float32
	logBandAmp [2][maxBands]float32
}

func newAnalysisState() analysisState {
	return analysisState{
		prevPCM: [2][]float32{
			make([]float32, shortBlockSampleCount),
			make([]float32, shortBlockSampleCount),
		},
	}
}

// analyzeFrame applies pre-emphasis, builds the MDCT overlap window, runs the
// forward MDCT, and returns per-band log amplitude for each input channel.
func analyzeFrame(mode *Mode, pcm [][]float32, startBand, endBand int, state *analysisState) (analysisResult, error) {
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
		pre := make([]float32, len(pcm[ch]))
		applyPreemphasis(pcm[ch], pre, &state.preemphasisMem[ch])

		mdctInput := make([]float32, shortBlockSampleCount+len(pre))
		copy(mdctInput, state.prevPCM[ch])
		copy(mdctInput[shortBlockSampleCount:], pre)

		res.mdct[ch] = forwardMDCT(mdctInput)
		if res.mdct[ch] == nil {
			return analysisResult{}, errInvalidFrameSize
		}

		res.logBandAmp[ch] = computeBandLogAmp(res.mdct[ch], lm, startBand, endBand)
		copy(state.prevPCM[ch], pre[len(pre)-shortBlockSampleCount:])
	}

	return res, nil
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
