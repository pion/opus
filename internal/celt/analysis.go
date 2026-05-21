// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
)

const preemphasisCoefficient = 0.85000610

type analysisState struct {
	prevPCM        []float32
	preemphasisMem float32
}

type analysisResult struct {
	info          frameSideInfo
	preemphasized []float32
	mdct          []float32
	logBandAmp    [maxBands]float32
}

func newAnalysisState() analysisState {
	return analysisState{
		prevPCM: make([]float32, shortBlockSampleCount),
	}
}

// analyzeFrame prepares the mono CELT encoder input: applies pre-emphasis,
// extends the frame with the previous overlap for the MDCT window, runs the
// forward MDCT, and returns per-band log amplitude for coarse energy coding.
func analyzeFrame(
	mode *Mode,
	frame []float32,
	startBand int,
	endBand int,
	state *analysisState,
) (analysisResult, error) {
	lm, err := mode.LMForFrameSampleCount(len(frame))
	if err != nil {
		return analysisResult{}, err
	}

	result := analysisResult{
		info: frameSideInfo{
			lm:              lm,
			startBand:       startBand,
			endBand:         endBand,
			channelCount:    1,
			transient:       false,
			shortBlockCount: 0,
			intraEnergy:     false,
			spread:          defaultSpreadDecision,
			allocationTrim:  defaultAllocationTrim,
		},
		preemphasized: make([]float32, len(frame)),
	}

	applyPreemphasis(frame, result.preemphasized, &state.preemphasisMem)

	mdctInput := make([]float32, shortBlockSampleCount+len(frame))
	copy(mdctInput, state.prevPCM)
	copy(mdctInput[shortBlockSampleCount:], result.preemphasized)

	result.mdct = forwardMDCT(mdctInput)
	if result.mdct == nil {
		return analysisResult{}, errInvalidFrameSize
	}

	result.logBandAmp = computeBandLogAmp(result.mdct, lm, startBand, endBand)
	copy(state.prevPCM, result.preemphasized[len(result.preemphasized)-shortBlockSampleCount:])

	return result, nil
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
