// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// normalisedBands builds a [2][]float32 whoseeach band has unit norm, using per
// bin values from gen.
func normalisedBands(lm int, gen func(band, i, n int) float32) [2][]float32 {
	scale := 1 << lm
	buf := make([]float32, scale*int(bandEdges[maxBands]))
	for band := range maxBands {
		lo := scale * int(bandEdges[band])
		n := scale * int(bandEdges[band+1]-bandEdges[band])
		var norm float64
		for i := range n {
			v := gen(band, i, n)
			buf[lo+i] = v
			norm += float64(v) * float64(v)
		}
		if norm > 0 {
			inv := float32(1 / math.Sqrt(norm))
			for i := range n {
				buf[lo+i] *= inv
			}
		}
	}

	return [2][]float32{buf, buf}
}

func runSpreading(bands [2][]float32, lm, frames int) int {
	avg, hf, tapset := 0, 0, 0
	decision := defaultSpreadDecision
	for range frames {
		decision = spreadingDecision(bands, lm, 0, maxBands, 1, &avg, &hf, &tapset,
			decision, true, uniformSpreadWeight())
	}

	return decision
}

func TestSpreadingDecisionTonalSignal(t *testing.T) {
	// One dominant bin per band: almost every bin falls below all three CDF
	// thresholds, so the metric saturates and no spreading is applied.
	bands := normalisedBands(maxLM, func(_, i, _ int) float32 {
		if i == 0 {
			return 1.0
		}

		return 0
	})
	assert.Equal(t, spreadNone, runSpreading(bands, maxLM, 8),
		"a spike per band is tonal and should end up at SPREAD_NONE")
}

func TestSpreadingDecisionNoiseSignal(t *testing.T) {
	// Flat band: every bin sits at 1/sqrt(N), so x²·N is 1 and clears every
	// threshold. That is the noisiest reading and asks for full spreading.
	bands := normalisedBands(maxLM, func(_, _, _ int) float32 { return 1 })
	assert.Equal(t, spreadAggressive, runSpreading(bands, maxLM, 8),
		"a flat band is noise-like and should end up at SPREAD_AGGRESSIVE")
}

func TestSpreadingDecisionRecursiveAvg(t *testing.T) {
	// Half the bands tonal, half flat, so the metric lands mid-range and the
	// recursive average needs several frames to converge.
	bands := normalisedBands(maxLM, func(band, i, _ int) float32 {
		if band%2 == 0 {
			if i == 0 {
				return 1.0
			}

			return 0
		}

		return 1
	})

	avgA, hfA, tapA := 0, 0, 0
	spreadingDecision(bands, maxLM, 0, maxBands, 1, &avgA, &hfA, &tapA,
		defaultSpreadDecision, true, uniformSpreadWeight())

	avgB, hfB, tapB := 0, 0, 0
	prev := defaultSpreadDecision
	for range 8 {
		prev = spreadingDecision(bands, maxLM, 0, maxBands, 1, &avgB, &hfB, &tapB, prev, true, uniformSpreadWeight())
	}

	assert.NotEqual(t, avgA, avgB, "the recursive average should keep converging past the first frame")
}

func TestSpreadingDecisionNarrowLastBand(t *testing.T) {
	// libopus bails out to SPREAD_NONE when the last band is too narrow to
	// carry usable statistics (celt/bands.c:484).
	bands := normalisedBands(0, func(_, _, _ int) float32 { return 1 })
	avg, hf, tapset := 0, 0, 0
	got := spreadingDecision(bands, 0, 0, 2, 1, &avg, &hf, &tapset,
		spreadNormal, true, uniformSpreadWeight())
	assert.Equal(t, spreadNone, got, "a narrow last band should short-circuit to SPREAD_NONE")
}

func TestSpreadingDecisionUpdatesTapset(t *testing.T) {
	// The high-band CDF drives tapset_decision, which the pre-filter reads.
	bands := normalisedBands(maxLM, func(_, i, _ int) float32 {
		if i == 0 {
			return 1.0
		}

		return 0
	})
	avg, hf, tapset := 0, 0, 0
	for range 8 {
		spreadingDecision(bands, maxLM, 0, maxBands, 1, &avg, &hf, &tapset,
			defaultSpreadDecision, true, uniformSpreadWeight())
	}
	assert.Equal(t, 2, tapset, "a tonal high band should push tapset to 2")
}

func TestAnalyzeFrameAdaptiveSpread(t *testing.T) {
	// A pure sine and white-noise-like PCM should produce different spread
	// decisions, which changes the encoded symbol and therefore the FinalRange.
	enc1 := NewEncoder()
	enc2 := NewEncoder()
	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	sine := make([]float32, frameSampleCount)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / float64(sampleRate)))
	}

	// A square wave at period 7 (~6.8 kHz) has its energy spread across many
	// harmonics, giving a flatter MDCT spectrum than the 440 Hz sine.
	noise := make([]float32, frameSampleCount)
	for i := range noise {
		if i%7 < 4 {
			noise[i] = 0.1
		} else {
			noise[i] = -0.1
		}
	}

	// Warm both encoders up for several frames so the recursive average settles.
	const warmup = 5
	dstSine := make([]byte, frameBytes)
	dstNoise := make([]byte, frameBytes)
	for range warmup {
		_, err := enc1.EncodeFrame([][]float32{sine}, dstSine, frameBytes, 0, maxBands)
		require.NoError(t, err)
		_, err = enc2.EncodeFrame([][]float32{noise}, dstNoise, frameBytes, 0, maxBands)
		require.NoError(t, err)
	}

	assert.NotEqual(t, enc1.FinalRange(), enc2.FinalRange(),
		"sine and noise should produce different spread decisions after warmup "+
			"(sine=%x, noise=%x)", enc1.FinalRange(), enc2.FinalRange())
}

// warmTransientState runs a few frames through detectTransient so
// transientHistory settles into steady state, matching how the real encoder
// calls it every frame rather than in isolation.
func warmTransientState(state *analysisState, gen func(frame int) [][]float32) {
	const warmupFrames = 4
	for f := range warmupFrames {
		detectTransient(gen(f), state)
	}
}

func TestDetectTransientSteadySine(t *testing.T) {
	state := newAnalysisState()
	gen := func(f int) [][]float32 {
		pcm := make([]float32, 960)
		for i := range pcm {
			pcm[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(f*960+i)/sampleRate))
		}

		return [][]float32{pcm}
	}
	warmTransientState(&state, gen)

	metric, _ := transientFrameMetric(gen(4), &state)
	t.Logf("steady 440 Hz sine metric=%d", metric)
	assert.LessOrEqual(t, metric, transientMaskThreshold,
		"a steady tone should not be detected as transient once history settles (metric=%d)", metric)
}

func TestDetectTransientWhiteNoiseNotFlagged(t *testing.T) {
	state := newAnalysisState()
	seed := uint32(12345)
	noise := func() float32 {
		seed = 1664525*seed + 1013904223

		return float32(int32(seed>>8)%2000-1000) / 1000.0 //nolint:gosec // G115: bounded LCG output.
	}
	gen := func(int) [][]float32 {
		pcm := make([]float32, 960)
		for i := range pcm {
			pcm[i] = 0.3 * noise()
		}

		return [][]float32{pcm}
	}
	warmTransientState(&state, gen)

	metric, _ := transientFrameMetric(gen(4), &state)
	t.Logf("white noise metric=%d", metric)
	assert.LessOrEqual(t, metric, transientMaskThreshold,
		"stationary broadband noise should not be detected as transient (metric=%d)", metric)
}

func TestDetectTransientSpikeAfterSteadySignal(t *testing.T) {
	state := newAnalysisState()
	amp := 0.3
	gen := func(f int) [][]float32 {
		pcm := make([]float32, 960)
		for i := range pcm {
			pcm[i] = float32(amp * math.Sin(2*math.Pi*440*float64(f*960+i)/sampleRate))
		}

		return [][]float32{pcm}
	}
	warmTransientState(&state, gen)

	spike := gen(4)
	spike[0][480] += 1.0
	metric, _ := transientFrameMetric(spike, &state)
	t.Logf("spike-after-steady metric=%d", metric)
	assert.Greater(t, metric, transientMaskThreshold,
		"a sharp mid-frame spike after steady content should be detected as transient (metric=%d)", metric)
}

func TestDetectTransientStereoSpikeOnOneChannel(t *testing.T) {
	state := newAnalysisState()
	gen := func(f int) [][]float32 {
		left := make([]float32, 960)
		right := make([]float32, 960)
		for i := range left {
			left[i] = float32(0.3 * math.Sin(2*math.Pi*440*float64(f*960+i)/sampleRate))
		}

		return [][]float32{left, right}
	}
	warmTransientState(&state, gen)

	frame := gen(4)
	frame[0][480] += 1.0 // right channel stays silent
	metric, _ := transientFrameMetric(frame, &state)
	assert.Greater(t, metric, transientMaskThreshold,
		"a spike on either stereo channel should be detected, even with the other channel silent (metric=%d)", metric)
}

// TestDetectTransientColdStart documents intentional, expected behavior: the
// very first call sees all-zero history, so a signal starting abruptly reads
// as a real silence-to-signal transient. This isn't a bug — a fresh Encoder's
// first frame has no prior context to compare against.
func TestDetectTransientColdStart(t *testing.T) {
	state := newAnalysisState()
	pcm := make([]float32, 960)
	for i := range pcm {
		pcm[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}
	isTransient, _, _ := detectTransient([][]float32{pcm}, &state)
	assert.True(t, isTransient,
		"a signal starting from zero history is a legitimate transient")
}

func TestDetectTransientEmpty(t *testing.T) {
	state := newAnalysisState()
	nilTransient, _, _ := detectTransient(nil, &state)
	assert.False(t, nilTransient, "empty PCM should be a defensive false")
	emptyTransient, _, _ := detectTransient([][]float32{}, &state)
	assert.False(t, emptyTransient, "empty channels should be a defensive false")
	pcm := make([]float32, 0)
	zeroTransient, _, _ := detectTransient([][]float32{pcm}, &state)
	assert.False(t, zeroTransient, "zero-length frame should be a defensive false")
}

func TestDetectTransientFrameSize2_5ms(t *testing.T) {
	pcm := make([]float32, 120)
	pcm[60] = 1.0
	state := newAnalysisState()
	_, _, tfChan := detectTransient([][]float32{pcm}, &state)
	assert.Zero(t, tfChan, "mono only has one channel to pick from")
}

func TestDCBlockRemovesConstantOffset(t *testing.T) {
	// After 1 s of constant input at 48 kHz the filter should settle to <1% of input.
	pcm := make([]float32, 48000)
	for i := range pcm {
		pcm[i] = 1.0
	}
	var mem float32
	applyDCBlock(pcm, sampleRate, &mem)
	var sum float64
	for i := len(pcm) - 4800; i < len(pcm); i++ {
		sum += float64(pcm[i])
	}
	assert.InDelta(t, 0.0, sum/4800, 0.01,
		"DC filter should attenuate constant offset below 1%% (mean=%f)", sum/4800)
}

func TestDCBlockPreservesSine(t *testing.T) {
	// 200 Hz is well above the 3 Hz cutoff; max sample deviation after warmup must stay < 2%.
	const freq = 200.0
	const totalSamples = 96000
	allIn := make([]float32, totalSamples)
	for i := range allIn {
		allIn[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate)))
	}
	allOut := make([]float32, totalSamples)
	copy(allOut, allIn)
	var mem float32
	applyDCBlock(allOut, sampleRate, &mem)
	var maxDiff float32
	for i := totalSamples - 4800; i < totalSamples; i++ {
		diff := allIn[i] - allOut[i]
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	assert.True(t, maxDiff < 0.02,
		"200 Hz sine should pass through DC filter nearly unchanged (maxDiff=%f)", maxDiff)
}

func TestDCBlockMultiFrameState(t *testing.T) {
	// Filter state must persist across frames: two half-frame runs must match one full run.
	pcm := make([]float32, 960)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / float64(sampleRate)))
	}
	fullRun := make([]float32, len(pcm))
	copy(fullRun, pcm)
	var memFull float32
	applyDCBlock(fullRun, sampleRate, &memFull)

	var memSplit float32
	out1 := make([]float32, 480)
	copy(out1, pcm[:480])
	applyDCBlock(out1, sampleRate, &memSplit)
	out2 := make([]float32, 480)
	copy(out2, pcm[480:])
	applyDCBlock(out2, sampleRate, &memSplit)

	splitRun := make([]float32, 0, len(out1)+len(out2))
	splitRun = append(splitRun, out1...)
	splitRun = append(splitRun, out2...)
	for i := range fullRun {
		assert.InDelta(t, fullRun[i], splitRun[i], 1e-6,
			"frame %d: split run must match continuous run", i)
	}
}

func TestAnalyzeFrameAppliesDCBlock(t *testing.T) {
	// A sine with DC offset must produce a different bitstream than the clean sine.
	enc1 := NewEncoder()
	enc2 := NewEncoder()
	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	sine := make([]float32, frameSampleCount)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / float64(sampleRate)))
	}
	withOffset := make([]float32, frameSampleCount)
	for i := range withOffset {
		withOffset[i] = sine[i] + 0.5 // large DC offset relative to the sine
	}

	dst1 := make([]byte, frameBytes)
	_, err := enc1.EncodeFrame([][]float32{sine}, dst1, frameBytes, 0, maxBands)
	require.NoError(t, err)
	dst2 := make([]byte, frameBytes)
	_, err = enc2.EncodeFrame([][]float32{withOffset}, dst2, frameBytes, 0, maxBands)
	require.NoError(t, err)

	assert.NotEqual(t, enc1.FinalRange(), enc2.FinalRange(),
		"DC block should change the encoder output (sine=%x, withOffset=%x)",
		enc1.FinalRange(), enc2.FinalRange())
}

func TestChooseAllocationTrimDefault(t *testing.T) {
	// Espectro plano a 128kbps → trim cerca de 5 (default).
	logBandAmp := makeFlatLogBandAmp(0.0) // todas las bandas iguales
	mdct := makeFlatMDCT()
	trim := chooseAllocationTrim(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 0, 0, new(float32), 128000,
	)
	assert.InDelta(t, 5, trim, 1, "flat spectrum at 128kbps should stay near default")
}

func TestChooseAllocationTrimLowBitrate(t *testing.T) {
	// A 32kbps → base trim=4 (no 5).
	logBandAmp := makeFlatLogBandAmp(0.0)
	mdct := makeFlatMDCT()
	trim := chooseAllocationTrim(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 0, 0, new(float32), 32000,
	)
	assert.LessOrEqual(t, trim, 5, "low bitrate should bias trim downward")
}

func TestChooseAllocationTrimSpectralTilt(t *testing.T) {
	// Low-heavy spectrum → diff < 0 → trim -= negative → trim increases
	// (trim > 5 biases bits toward low bands). High-heavy → opposite.
	lowHeavy := makeTiltedLogBandAmp(-1.0)  // bandas bajas con más energía
	highHeavy := makeTiltedLogBandAmp(+1.0) // bandas altas con más energía
	mdct := makeFlatMDCT()
	trimLow := chooseAllocationTrim([2][maxBands]float32{lowHeavy, lowHeavy},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 0, 0, new(float32), 128000,
	)
	trimHigh := chooseAllocationTrim([2][maxBands]float32{highHeavy, highHeavy},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 0, 0, new(float32), 128000,
	)
	assert.Greater(t, trimLow, trimHigh, "low-heavy spectrum should bias trim upward (more bits to lows)")
}

func TestChooseAllocationTrimStereoCorrelated(t *testing.T) {
	// L=R (correlated) → trim disminuye.
	logBandAmp := makeFlatLogBandAmp(0.0)
	mdct := makeSineMDCT(440) // mismo contenido en ambos canales
	trimCorr := chooseAllocationTrim([2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 2, maxLM, maxBands, 0, 0, new(float32), 128000,
	)

	// L y R decorrelated → trim sin ajuste stereo.
	mdctR := makeNoiseMDCT(42)
	trimDecorr := chooseAllocationTrim([2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdctR}, 2, maxLM, maxBands, 0, 0, new(float32), 128000,
	)

	assert.Less(t, trimCorr, trimDecorr, "correlated stereo should have lower trim than decorrelated")
}

func TestChooseAllocationTrimTFEstimate(t *testing.T) {
	// A frame asking for finer time resolution gets its trim pulled down by
	// twice tf_estimate, so bits move up the spectrum.
	logBandAmp := makeFlatLogBandAmp(0.0)
	mdct := makeFlatMDCT()
	trimFlat := chooseAllocationTrim([2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 0, 0, new(float32), 128000,
	)
	trimTF := chooseAllocationTrim([2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 1.0, 0, new(float32), 128000)
	assert.Equal(t, trimFlat-2, trimTF, "tf_estimate of 1.0 should drop the trim by 2")
}

// makeFlatLogBandAmp returns a per-band log amplitude array with every band
// set to v. Used to feed chooseAllocationTrim a spectrally flat input.
func makeFlatLogBandAmp(v float32) [maxBands]float32 { //nolint:unparam // v is kept for future tests
	var out [maxBands]float32
	for i := range out {
		out[i] = v //nolint:gosec // G602: i is always in bounds, sourced from range out.
	}

	return out
}

// makeTiltedLogBandAmp returns a per-band log amplitude with a linear tilt
// across bands. slope > 0 favors high bands, slope < 0 favors lows.
func makeTiltedLogBandAmp(slope float32) [maxBands]float32 {
	var out [maxBands]float32
	for i := range out {
		out[i] = slope * (float32(i) - float32(maxBands-1)/2.0) //nolint:gosec // G602: i from range.
	}

	return out
}

// makeFlatMDCT returns an MDCT spectrum of the full frame with every bin set
// to one. Cosine similarity between two identical flat spectra is 1.0.
func makeFlatMDCT() []float32 {
	mdct := make([]float32, (1<<maxLM)*int(bandEdges[maxBands]))
	for i := range mdct {
		mdct[i] = 1
	}

	return mdct
}

// makeSineMDCT returns an MDCT spectrum with a peak at the band closest to
// freqHz. Bins outside that band are small but non-zero so the spectrum is
// not degenerate. Two calls with the same freqHz produce identical spectra,
// so cosine similarity is 1.0 (perfectly correlated).
func makeSineMDCT(freqHz float32) []float32 {
	scale := 1 << maxLM
	mdct := make([]float32, scale*int(bandEdges[maxBands]))
	binHz := float32(sampleRate) / float32(2*scale*int(bandEdges[maxBands]))
	targetBin := max(0, int(freqHz/binHz))
	if targetBin >= len(mdct) {
		targetBin = len(mdct) - 1
	}
	for i := range mdct {
		mdct[i] = 0.01
	}
	mdct[targetBin] = 1.0

	return mdct
}

// makeNoiseMDCT returns a deterministic pseudo-random MDCT spectrum seeded by
// seed. Different seeds produce decorrelated spectra.
func makeNoiseMDCT(seed uint32) []float32 {
	scale := 1 << maxLM
	mdct := make([]float32, scale*int(bandEdges[maxBands]))
	state := seed
	for i := range mdct {
		// Linear congruential generator (same constants as libopus celt_lcg_rand).
		state = 1664525*state + 1013904223
		// Map to [-1, 1]. int32 conversion is safe: state is a full-cycle LCG.
		mdct[i] = float32(int32(state)) / float32(1<<31) //nolint:gosec // G115: intentional bit cast
	}

	return mdct
}

func TestDynallocFlatSpectrumNoBoost(t *testing.T) {
	// Flat spectrum → no isolated peaks → all offsets zero.
	logBandAmp := makeFlatLogBandAmp(0.0)
	prev := makeFlatLogBandAmp(0.0)
	dr := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false, false, false,
	)
	for band := range maxBands {
		assert.Equal(t, 0, dr.offsets[band], "flat spectrum band %d should get no boost", band)
	}
}

func TestDynallocIsolatedPeakGetsBoost(t *testing.T) {
	// Isolated peak in band 10 → that band gets boost.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[10] = 10.0
	prev := makeFlatLogBandAmp(0.0)
	dr := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false, false, false,
	)
	assert.Greater(t, dr.offsets[10], 0, "isolated peak should get boost")
}

func TestDynallocSpreadWeightMaskedBandReduced(t *testing.T) {
	// Strong peak in band 15 → neighboring bands are masked → lower weight.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[15] = 20.0
	prev := makeFlatLogBandAmp(0.0)
	dr := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false, false, false,
	)
	// Bands far from the peak should have reduced weight.
	assert.Less(t, dr.spreadWeight[5], 32, "band far from peak should have reduced weight")
}

func TestDynallocSpreadWeightMaskDecayRate(t *testing.T) {
	// The mask spreads by 2 per band upward and 3 downward, in the same log2
	// domain as bandLogE. A peak of 4 stops masking two bands up, so the weight
	// recovers there; reading those constants as dB would halve the step and
	// leave the band masked.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[10] = 4.0
	prev := makeFlatLogBandAmp(0.0)
	dr := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false, false, false,
	)
	assert.Greater(t, dr.spreadWeight[12], dr.spreadWeight[11],
		"weight should recover two bands above the peak")
	assert.Less(t, dr.spreadWeight[9], dr.spreadWeight[8],
		"the band just below the peak stays masked")
}

func TestDynallocLowBitrateGated(t *testing.T) {
	// Below 30+5*LM bytes → dynalloc disabled, all offsets zero.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[10] = 10.0
	prev := makeFlatLogBandAmp(0.0)
	dr := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 10, false, false, false, // 10 bytes < 30+15=45
	)
	for band := range maxBands {
		assert.Equal(t, 0, dr.offsets[band], "low bitrate should gate dynalloc")
	}
}

func TestMedianOf3(t *testing.T) {
	assert.Equal(t, float32(2), medianOf3([3]float32{1, 2, 3}))
	assert.Equal(t, float32(2), medianOf3([3]float32{3, 1, 2}))
	assert.Equal(t, float32(2), medianOf3([3]float32{2, 3, 1}))
	assert.Equal(t, float32(1), medianOf3([3]float32{1, 1, 1}))
}

func TestMedianOf5(t *testing.T) {
	assert.Equal(t, float32(3), medianOf5([5]float32{1, 2, 3, 4, 5}))
	assert.Equal(t, float32(3), medianOf5([5]float32{5, 4, 3, 2, 1}))
	assert.Equal(t, float32(3), medianOf5([5]float32{3, 1, 4, 5, 2}))
	assert.Equal(t, float32(2), medianOf5([5]float32{2, 2, 2, 2, 2}))
}

// uniformSpreadWeight returns a weight array where every band has weight 32
// (no masking). This matches the pre-7c behavior of spreadingDecision.
func uniformSpreadWeight() [maxBands]int {
	var w [maxBands]int
	for i := range w {
		w[i] = 32 //nolint:gosec // G602: i from range.
	}

	return w
}
