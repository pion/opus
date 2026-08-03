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

type analysisState struct {
	prevPCM           [2][]float32
	preemphasisMem    [2]float32
	dcBlockMem        [2]float32
	preScratch        [2][]float32
	mdctInput         [2][]float32
	transientMDCT     [2][]float32
	prefilterMem      [2][]float32
	prefilter         postFilterState
	prefilterBuf      [2][]float32
	prefilterOut      [2][]float32
	transientProbe    [2][]float32
	transientEnvelope [2][]float32
	// transientHistory holds the last shortBlockSampleCount samples of the
	// DC-blocked + pre-emphasized (but not pitch-whitened) signal, updated by
	// detectTransient every frame. prevPCM can't serve this role: it's
	// captured after the pitch pre-filter runs, so it goes whitened whenever
	// the pre-filter is active, and libopus's own history source for this
	// (prefilter_mem) is specifically the unwhitened tail.
	transientHistory [2][]float32
	// transientDCMem/transientPreMem are the probe's own filter memories,
	// separate from dcBlockMem/preemphasisMem. detectTransient runs before
	// analyzeFrame each frame, so borrowing the real memories would only
	// stay correct if analyzeFrame always ran right after with the same
	// samples — true in the real encoder, but a landmine for anything that
	// calls detectTransient on its own (tests included).
	transientDCMem  [2]float32
	transientPreMem [2]float32
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
		prefilterOut: [2][]float32{
			make([]float32, postfilterHistorySampleCount+maxFrame),
			make([]float32, postfilterHistorySampleCount+maxFrame),
		},
		transientProbe: [2][]float32{
			make([]float32, shortBlockSampleCount+maxFrame),
			make([]float32, shortBlockSampleCount+maxFrame),
		},
		transientEnvelope: [2][]float32{
			make([]float32, shortBlockSampleCount+maxFrame),
			make([]float32, shortBlockSampleCount+maxFrame),
		},
		transientHistory: [2][]float32{
			make([]float32, shortBlockSampleCount),
			make([]float32, shortBlockSampleCount),
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
			src := state.prefilterBuf[ch][:postfilterHistorySampleCount+len(pre)]
			copy(src, state.prefilterMem[ch])
			copy(src[postfilterHistorySampleCount:], pre)
			dst := state.prefilterOut[ch][:len(src)]
			applyPrefilter(
				dst, src,
				state.prefilter.oldPeriod, prefilterPeriod,
				len(pre),
				state.prefilter.oldGain, prefilterGain,
				state.prefilter.oldTapset, prefilterTapset,
			)
			// Carry the unfiltered tail, since the taps read the input signal
			// (libopus keeps prefilter_mem from pre, not from the output).
			copy(state.prefilterMem[ch], src[len(pre):len(pre)+postfilterHistorySampleCount])
			copy(pre, dst[postfilterHistorySampleCount:])
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

const (
	// transientForwardDecay/transientBackwardDecay set the post-echo (6.7
	// dB/ms) and pre-echo (13.9 dB/ms) masking decay of the envelope
	// follower in transientMaskMetric, matching libopus's non-hybrid
	// transient_analysis (celt_encoder.c).
	transientForwardDecay  = 0.0625
	transientBackwardDecay = 0.875
	// transientMaskThreshold is libopus's mask_metric>200 cutoff.
	transientMaskThreshold = 200
)

// transientInverseTable is libopus's inv_table: 6*64/x, trained on real data
// to minimize the average error when turning a per-sample masking ratio into
// a harmonic-mean-friendly weight (celt_encoder.c).
var transientInverseTable = [128]int{ //nolint:gochecknoglobals
	255, 255, 156, 110, 86, 70, 59, 51, 45, 40, 37, 33, 31, 28, 26, 25,
	23, 22, 21, 20, 19, 18, 17, 16, 16, 15, 15, 14, 13, 13, 12, 12,
	12, 12, 11, 11, 11, 10, 10, 10, 9, 9, 9, 9, 9, 9, 8, 8,
	8, 8, 8, 7, 7, 7, 7, 7, 7, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 2,
}

// detectTransient reports whether the frame needs short MDCT blocks, mirroring
// libopus's transient_analysis (celt_encoder.c, float build). It runs its own
// DC-block + pre-emphasis pass over dedicated state (transientDCMem/PreMem/
// History), independent of dcBlockMem/preemphasisMem/prevPCM, so it works the
// same whether or not analyzeFrame follows it.
//
// tone_freq/toneishness (the low-frequency-tone guard) and allow_weak_transients
// (hybrid low-bitrate mode) aren't ported — pion has no tonality analysis or
// hybrid mode yet, matching how signalBandwidth defaults to end-1 elsewhere.
func detectTransient(pcm [][]float32, state *analysisState) bool {
	return transientFrameMetric(pcm, state) > transientMaskThreshold
}

func transientFrameMetric(pcm [][]float32, state *analysisState) int {
	if len(pcm) == 0 {
		return 0
	}

	maskMetric := 0
	for ch := range pcm {
		if len(pcm[ch]) == 0 {
			return 0
		}

		n := shortBlockSampleCount + len(pcm[ch])
		probe := state.transientProbe[ch][:n]
		copy(probe, state.transientHistory[ch])

		tail := probe[shortBlockSampleCount:]
		copy(tail, pcm[ch])
		applyDCBlock(tail, sampleRate, &state.transientDCMem[ch])
		applyPreemphasis(tail, tail, &state.transientPreMem[ch])
		copy(state.transientHistory[ch], tail[len(tail)-shortBlockSampleCount:])

		if m := transientMaskMetric(probe, state.transientEnvelope[ch][:n]); m > maskMetric {
			maskMetric = m
		}
	}

	return maskMetric
}

// transientMaskMetric returns libopus's post/pre-echo masking metric for one
// channel: the ratio of the frame's energy over the harmonic mean of a
// masking envelope built from a 2nd-order high-passed version of the signal.
// scratch must be at least len(signal) long; it's used as scratch and left
// with unspecified contents on return.
func transientMaskMetric(signal, scratch []float32) int {
	n := len(signal)
	tmp := scratch[:n]

	var mem0, mem1 float32
	for i, x := range signal {
		y := mem0 + x
		prevMem0 := mem0
		mem0 = mem0 - x + 0.5*mem1
		mem1 = x - prevMem0
		tmp[i] = y
	}
	// The first few samples are unreliable since the filter memory hasn't
	// propagated yet.
	clear(tmp[:min(12, n)])

	len2 := n / 2
	if len2 < 18 {
		return 0
	}

	var mean float32
	mem0 = 0
	for i := range len2 {
		x2 := tmp[2*i]*tmp[2*i] + tmp[2*i+1]*tmp[2*i+1]
		mean += x2
		mem0 = x2 + (1-transientForwardDecay)*mem0
		tmp[i] = transientForwardDecay * mem0
	}

	mem0 = 0
	var maxE float32
	for i := len2 - 1; i >= 0; i-- {
		mem0 = tmp[i] + transientBackwardDecay*mem0
		tmp[i] = (1 - transientBackwardDecay) * mem0
		maxE = max(maxE, tmp[i])
	}

	// Frame energy is the geometric mean of the average envelope and half the
	// peak — a compromise with the older, simpler detector this replaces.
	frameEnergy := float32(math.Sqrt(float64(mean * maxE * 0.5 * float32(len2))))
	norm := float32(len2) / (1e-15 + frameEnergy)

	unmask := 0
	for i := 12; i < len2-5; i += 4 {
		id := int(math.Floor(float64(64 * norm * (tmp[i] + 1e-15))))
		id = min(max(id, 0), 127)
		unmask += transientInverseTable[id]
	}

	// Compensate for the 1/4th sampling above and the factor of 6 baked into
	// the inverse table.
	return 64 * unmask * 4 / (6 * (len2 - 17))
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
