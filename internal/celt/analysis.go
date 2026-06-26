// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
)

const preemphasisCoefficient = 0.85000610

// dcBlockCutoffHz is the 3 Hz high-pass cutoff for the DC-removal filter,
// matching libopus dc_reject (src/opus_encoder.c:479-507).
const dcBlockCutoffHz = 3.0

// transientRatioThreshold is the minimum ratio between the energy of the
// second half of the frame and the first half that the detector reports as
// a transient. It was calibrated empirically against the synthetic fixtures
// in analysis_test.go.
const transientRatioThreshold = 1.5

type analysisState struct {
	prevPCM        [2][]float32
	preemphasisMem [2]float32
	dcBlockMem     [2]float32
	preScratch     [2][]float32
	mdctInput      [2][]float32
	transientMDCT  [2][]float32
	prefilterMem   [2][]float32
	prefilter      postFilterState
	prefilterBuf   [2][]float32
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
		transientMDCT: [2][]float32{
			make([]float32, maxFrame),
			make([]float32, maxFrame),
		},
		prefilterMem: [2][]float32{
			make([]float32, postfilterHistorySampleCount),
			make([]float32, postfilterHistorySampleCount),
		},
		prefilterBuf: [2][]float32{
			make([]float32, postfilterHistorySampleCount+maxFrame),
			make([]float32, postfilterHistorySampleCount+maxFrame),
		},
	}

	return state
}

// analyzeFrame applies pre-emphasis, builds the MDCT overlap window, runs the
// forward MDCT, and returns per-band log amplitude for each input channel.
//
// When transient is true and lm > 0, I run (1<<lm) short MDCTs of 2.5 ms each
// and interleave their spectra so inverseTransformChannel can split them back —
// RFC 6716 §4.3.7 defines the interleaved layout for transient frames.
func analyzeFrame(
	mode *Mode, pcm [][]float32, startBand, endBand int,
	state *analysisState, mdctScratch *forwardMDCTScratch, fftScratch *[]complex32,
	transient bool,
	prefilterEnabled bool, prefilterPeriod int, prefilterGain float32, prefilterTapset int,
) (analysisResult, error) {
	lm, err := mode.LMForFrameSampleCount(len(pcm[0]))
	if err != nil {
		return analysisResult{}, err
	}

	useShortBlocks := transient && lm > 0
	res := analysisResult{
		info: frameSideInfo{
			lm:             lm,
			startBand:      startBand,
			endBand:        endBand,
			channelCount:   len(pcm),
			transient:      useShortBlocks,
			spread:         defaultSpreadDecision,
			allocationTrim: defaultAllocationTrim,
		},
	}
	if useShortBlocks {
		res.info.shortBlockCount = 1 << lm
	}

	for ch := range pcm {
		// Work on a scratch copy so the caller's PCM is never modified.
		pre := state.preScratch[ch][:len(pcm[ch])]
		copy(pre, pcm[ch])
		applyDCBlock(pre, mode.SampleRate(), &state.dcBlockMem[ch])
		applyPreemphasis(pre, pre, &state.preemphasisMem[ch])

		// Apply pitch pre-filter (whitening) before MDCT, mirroring
		// libopus run_prefilter. Reuses combFilter with negated gains.
		if prefilterEnabled {
			buf := state.prefilterBuf[ch][:postfilterHistorySampleCount+len(pre)]
			copy(buf, state.prefilterMem[ch])
			copy(buf[postfilterHistorySampleCount:], pre)
			applyPrefilter(
				buf,
				state.prefilter.oldPeriod, prefilterPeriod,
				len(pre),
				state.prefilter.oldGain, prefilterGain,
				state.prefilter.oldTapset, prefilterTapset,
			)
			copy(pre, buf[postfilterHistorySampleCount:])
			copy(state.prefilterMem[ch], buf[len(pre):len(pre)+postfilterHistorySampleCount])
		}

		if useShortBlocks {
			analyzeTransientChannel(
				pre, state.prevPCM[ch], ch,
				state.transientMDCT[ch], state.mdctInput[ch],
				mdctScratch, fftScratch, lm,
			)
			res.mdct[ch] = state.transientMDCT[ch][:len(pre)]
		} else {
			mdctInput := state.mdctInput[ch][:shortBlockSampleCount+len(pre)]
			copy(mdctInput, state.prevPCM[ch])
			copy(mdctInput[shortBlockSampleCount:], pre)

			res.mdct[ch] = forwardMDCTWithScratch(mdctInput, ch, mdctScratch, fftScratch)
			if res.mdct[ch] == nil {
				return analysisResult{}, errInvalidFrameSize
			}
		}

		res.logBandAmp[ch] = computeBandLogAmp(res.mdct[ch], lm, startBand, endBand)
		copy(state.prevPCM[ch], pre[len(pre)-shortBlockSampleCount:])
	}

	return res, nil
}

// analyzeTransientChannel runs (1<<lm) short MDCTs over successive 2.5 ms
// sub-frames and writes the interleaved result into out. I use the same layout
// as inverseTransformChannel (RFC 6716 §4.3.7): bin i of sub-frame b lands at
// out[b + i*(1<<lm)].
func analyzeTransientChannel(
	pre []float32,
	prevOverlap []float32,
	ch int,
	out []float32,
	mdctInputScratch []float32,
	scratch *forwardMDCTScratch,
	fftScratch *[]complex32,
	lm int,
) {
	numBlocks := 1 << lm
	shortInput := mdctInputScratch[:2*shortBlockSampleCount]
	for block := range numBlocks {
		if block == 0 {
			copy(shortInput[:shortBlockSampleCount], prevOverlap)
		} else {
			copy(shortInput[:shortBlockSampleCount], pre[(block-1)*shortBlockSampleCount:block*shortBlockSampleCount])
		}
		copy(shortInput[shortBlockSampleCount:], pre[block*shortBlockSampleCount:(block+1)*shortBlockSampleCount])
		bins := forwardMDCTWithScratch(shortInput, ch, scratch, fftScratch)
		for i := range shortBlockSampleCount {
			out[block+i*numBlocks] = bins[i]
		}
	}
}

// detectTransient reports whether any channel of the input PCM contains a
// transient using a half-frame energy ratio.
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

// spreadingDecision computes the spread level for one frame from the MDCT
// spectrum and updates the inter-frame average in prevAvg.
//
// The metric is the weighted mean of (1 - bandMean/bandPeak) across coded
// bands, where spreadWeight[band] controls each band's contribution. A band
// where one bin dominates gives a value near 1 (tonal); a band with uniform
// energy gives a value near 0 (noise-like). This is the floating-point
// equivalent of the per-band CDF step inside libopus spreading_decision
// (celt_encoder.c). spreadWeight comes from dynallocAnalysis masking model.
func spreadingDecision(
	mdct []float32, lm, startBand, endBand int,
	prevAvg *float32, prevDecision int,
	spreadWeight [maxBands]int,
) int {
	scale := 1 << lm
	var sum float32
	nBands := 0

	for band := startBand; band < endBand; band++ {
		lo := scale * int(bandEdges[band])
		hi := scale * int(bandEdges[band+1])
		n := hi - lo
		if n < 2 {
			continue
		}

		weight := spreadWeight[band]
		if weight == 0 {
			continue
		}

		var energy, maxE float32
		for i := lo; i < hi; i++ {
			e := mdct[i] * mdct[i]
			energy += e
			if e > maxE {
				maxE = e
			}
		}

		mean := energy / float32(n)
		if mean < 1e-30 || maxE < 1e-30 {
			continue
		}

		// 0 when all bins are equal (noise), near 1 when one bin dominates (tonal).
		tonality := 1 - mean/maxE
		sum += tonality * float32(weight)
		nBands += weight
	}

	if nBands == 0 {
		return prevDecision
	}

	avg := sum / float32(nBands)
	// Recursive inter-frame average damps single-frame spikes.
	avg = 0.5 * (avg + *prevAvg)
	*prevAvg = avg

	return hysteresisDecision(avg, prevDecision, [3]float32{0.15, 0.40, 0.65})
}

// applyDCBlock applies a first-order IIR high-pass at dcBlockCutoffHz to
// remove DC bias. mem must persist across frames. Not normative — encoder-only
// pre-processing (libopus dc_reject, src/opus_encoder.c:479-507).
func applyDCBlock(pcm []float32, sampleRate int, mem *float32) {
	coef := 6.3 * dcBlockCutoffHz / float32(sampleRate)
	coef2 := float32(1) - coef
	for i := range pcm {
		x := pcm[i]
		pcm[i] = x - *mem
		*mem = coef*x + coef2**mem
	}
}
