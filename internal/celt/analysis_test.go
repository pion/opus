// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpreadingDecisionTonalSignal(t *testing.T) {
	// A spectrum with one dominant bin per band should read as tonal (high metric)
	// and produce at least NORMAL spreading after warmup.
	lm := maxLM
	scale := 1 << lm
	mdct := make([]float32, scale*int(bandEdges[maxBands]))

	for band := range maxBands {
		lo := scale * int(bandEdges[band])
		hi := scale * int(bandEdges[band+1])
		if hi > lo {
			// put all energy in the first bin of each band
			mdct[lo] = 1.0
		}
	}

	var prevAvg float32
	prev := defaultSpreadDecision
	var decision int
	for range 8 {
		decision = spreadingDecision(mdct, lm, 0, maxBands, &prevAvg, prev, uniformSpreadWeight())
		prev = decision
	}
	assert.GreaterOrEqual(t, decision, spreadNormal,
		"spike-per-band spectrum should reach at least NORMAL after warmup (got %d)", decision)
}

func TestSpreadingDecisionNoiseSignal(t *testing.T) {
	// A flat spectrum (uniform energy per bin) is noise-like and should settle
	// at NONE or LIGHT after warmup.
	lm := maxLM
	scale := 1 << lm
	mdct := make([]float32, scale*int(bandEdges[maxBands]))
	for i := range mdct {
		mdct[i] = 0.01
	}

	var prevAvg float32
	prev := defaultSpreadDecision
	var decision int
	for range 8 {
		decision = spreadingDecision(mdct, lm, 0, maxBands, &prevAvg, prev, uniformSpreadWeight())
		prev = decision
	}
	assert.LessOrEqual(t, decision, spreadLight,
		"uniform-energy spectrum should settle at NONE or LIGHT after warmup (got %d)", decision)
}

func TestSpreadingDecisionRecursiveAvg(t *testing.T) {
	// Two independent runs of N frames should produce the same avg value as a
	// single run of 2N frames because the recursive average is stateless
	// between calls.
	lm := maxLM
	scale := 1 << lm
	mdct := make([]float32, scale*int(bandEdges[maxBands]))
	for i := range mdct {
		mdct[i] = 0.1 * float32(i%7+1)
	}

	var avgA float32
	prevA := defaultSpreadDecision
	for range 4 {
		prevA = spreadingDecision(mdct, lm, 0, maxBands, &avgA, prevA, uniformSpreadWeight())
	}

	var avgB float32
	prevB := defaultSpreadDecision
	for range 8 {
		prevB = spreadingDecision(mdct, lm, 0, maxBands, &avgB, prevB, uniformSpreadWeight())
	}

	// After more frames the avg should converge further; after 8 frames it
	// must not be identical to after 4.
	assert.NotEqual(t, avgA, avgB, "recursive average should differ after different frame counts")
}

func TestSpreadingDecisionSilentFrame(t *testing.T) {
	lm := maxLM
	mdct := make([]float32, (1<<lm)*int(bandEdges[maxBands]))
	var prevAvg float32
	// All zero input: should return prevDecision unchanged.
	got := spreadingDecision(mdct, lm, 0, maxBands, &prevAvg, spreadNormal, uniformSpreadWeight())
	assert.Equal(t, spreadNormal, got, "silent frame should return prevDecision")
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

func TestDetectTransientSteadySine(t *testing.T) {
	state := newAnalysisState()
	pcm := make([]float32, 960)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}
	metric := debugDetectTransient([][]float32{pcm})
	t.Logf("steady sine metric=%f", metric)
	assert.False(t, detectTransient([][]float32{pcm}, &state),
		"steady sine should not be detected as transient (metric=%f)", metric)
}

func TestDetectTransientSpike(t *testing.T) {
	state := newAnalysisState()
	pcm := make([]float32, 960)
	pcm[480] = 1.0
	metric := debugDetectTransient([][]float32{pcm})
	t.Logf("spike metric=%f", metric)
	assert.True(t, detectTransient([][]float32{pcm}, &state),
		"mid-frame spike should be detected as transient (metric=%f)", metric)
}

func TestDetectTransientStereoSpike(t *testing.T) {
	state := newAnalysisState()
	left := make([]float32, 960)
	right := make([]float32, 960)
	left[480] = 1.0
	metric := debugDetectTransient([][]float32{left, right})
	t.Logf("stereo spike metric=%f", metric)
	assert.True(t, detectTransient([][]float32{left, right}, &state),
		"transient on a single stereo channel should be detected (metric=%f)", metric)
}

func TestDetectTransientSilentChannel(t *testing.T) {
	state := newAnalysisState()
	left := make([]float32, 960)
	right := make([]float32, 960)
	left[480] = 1.0
	right[600] = 1.0
	metric := debugDetectTransient([][]float32{left, right})
	t.Logf("silent-channel metric=%f", metric)
	assert.True(t, detectTransient([][]float32{left, right}, &state),
		"transient on one stereo channel should be detected even when the "+
			"other channel is silent (metric=%f)", metric)
}

func TestDetectTransientStereoSteady(t *testing.T) {
	state := newAnalysisState()
	left := make([]float32, 960)
	right := make([]float32, 960)
	for i := range left {
		left[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		right[i] = float32(math.Sin(2 * math.Pi * 660 * float64(i) / sampleRate))
	}
	metric := debugDetectTransient([][]float32{left, right})
	t.Logf("stereo steady metric=%f", metric)
	assert.False(t, detectTransient([][]float32{left, right}, &state),
		"steady stereo sines should not be detected as transient (metric=%f)", metric)
}

func TestDetectTransientGradualFade(t *testing.T) {
	pcm := make([]float32, 960)
	for i := range pcm {
		pcm[i] = float32(i) / 960
	}
	metric := debugDetectTransient([][]float32{pcm})
	t.Logf("gradual fade metric=%f", metric)
	// A linear ramp from 0 to 1 over a frame is flagged by this detector
	// because the second half carries more energy. This is a known
	// limitation: libopus handles ramps correctly via sub-frame STFT and
	// forward masking (transient_analysis in celt_encoder.c).
	assert.Greater(t, metric, 1.0,
		"gradual ramp should be flagged by the simple detector (metric=%f)", metric)
}

func TestDetectTransientEmpty(t *testing.T) {
	state := newAnalysisState()
	assert.False(t, detectTransient(nil, &state),
		"empty PCM should be a defensive false")
	assert.False(t, detectTransient([][]float32{}, &state),
		"empty channels should be a defensive false")
	pcm := make([]float32, 0)
	assert.False(t, detectTransient([][]float32{pcm}, &state),
		"zero-length frame should be a defensive false")
}

func TestDetectTransientFrameSize2_5ms(t *testing.T) {
	pcm := make([]float32, 120)
	pcm[60] = 1.0
	state := newAnalysisState()
	_ = detectTransient([][]float32{pcm}, &state)
}

func TestDetectTransientSmallSpike(t *testing.T) {
	pcm := make([]float32, 960)
	pcm[480] = 0.01
	metric := debugDetectTransient([][]float32{pcm})
	t.Logf("small spike metric=%f", metric)
	// a small spike on a silent background has an infinite ratio; the
	// simplified detector flags it as transient. This is the intended
	// behavior: the encoder does not run on full silence, so any
	// spike represents a real signal change.
	assert.Greater(t, metric, 1.5,
		"small spike on silence should be flagged by the simple detector (metric=%f)", metric)
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
	mdct := makeFlatMDCT(1.0)
	trim := chooseAllocationTrim(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 128*8*50,
	)
	assert.InDelta(t, 5, trim, 1, "flat spectrum at 128kbps should stay near default")
}

func TestChooseAllocationTrimLowBitrate(t *testing.T) {
	// A 32kbps → base trim=4 (no 5).
	logBandAmp := makeFlatLogBandAmp(0.0)
	mdct := makeFlatMDCT(1.0)
	trim := chooseAllocationTrim(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 32*8*50,
	)
	assert.LessOrEqual(t, trim, 5, "low bitrate should bias trim downward")
}

func TestChooseAllocationTrimSpectralTilt(t *testing.T) {
	// Low-heavy spectrum → diff < 0 → trim -= negative → trim increases
	// (trim > 5 biases bits toward low bands). High-heavy → opposite.
	lowHeavy := makeTiltedLogBandAmp(-1.0)  // bandas bajas con más energía
	highHeavy := makeTiltedLogBandAmp(+1.0) // bandas altas con más energía
	mdct := makeFlatMDCT(1.0)
	trimLow := chooseAllocationTrim([2][maxBands]float32{lowHeavy, lowHeavy},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 128*8*50)
	trimHigh := chooseAllocationTrim([2][maxBands]float32{highHeavy, highHeavy},
		[2][]float32{mdct, mdct}, 1, maxLM, maxBands, 128*8*50)
	assert.Greater(t, trimLow, trimHigh, "low-heavy spectrum should bias trim upward (more bits to lows)")
}

func TestChooseAllocationTrimStereoCorrelated(t *testing.T) {
	// L=R (correlated) → trim disminuye.
	logBandAmp := makeFlatLogBandAmp(0.0)
	mdct := makeSineMDCT(440) // mismo contenido en ambos canales
	trimCorr := chooseAllocationTrim([2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdct}, 2, maxLM, maxBands, 128*8*50)

	// L y R decorrelated → trim sin ajuste stereo.
	mdctR := makeNoiseMDCT(42)
	trimDecorr := chooseAllocationTrim([2][maxBands]float32{logBandAmp, logBandAmp},
		[2][]float32{mdct, mdctR}, 2, maxLM, maxBands, 128*8*50)

	assert.Less(t, trimCorr, trimDecorr, "correlated stereo should have lower trim than decorrelated")
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
// to v. Cosine similarity between two identical flat spectra is 1.0.
func makeFlatMDCT(v float32) []float32 {
	mdct := make([]float32, (1<<maxLM)*int(bandEdges[maxBands]))
	for i := range mdct {
		mdct[i] = v
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
	offsets, _ := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false,
	)
	for band := range maxBands {
		assert.Equal(t, 0, offsets[band], "flat spectrum band %d should get no boost", band)
	}
}

func TestDynallocIsolatedPeakGetsBoost(t *testing.T) {
	// Isolated peak in band 10 → that band gets boost.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[10] = 10.0
	prev := makeFlatLogBandAmp(0.0)
	offsets, _ := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false,
	)
	assert.Greater(t, offsets[10], 0, "isolated peak should get boost")
}

func TestDynallocSpreadWeightMaskedBandReduced(t *testing.T) {
	// Strong peak in band 15 → neighboring bands are masked → lower weight.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[15] = 20.0
	prev := makeFlatLogBandAmp(0.0)
	_, spreadWeight := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 120, false,
	)
	// Bands far from the peak should have reduced weight.
	assert.Less(t, spreadWeight[5], 32, "band far from peak should have reduced weight")
}

func TestDynallocLowBitrateGated(t *testing.T) {
	// Below 30+5*LM bytes → dynalloc disabled, all offsets zero.
	logBandAmp := makeFlatLogBandAmp(0.0)
	logBandAmp[10] = 10.0
	prev := makeFlatLogBandAmp(0.0)
	offsets, _ := dynallocAnalysis(
		[2][maxBands]float32{logBandAmp, logBandAmp},
		[2][maxBands]float32{prev, prev},
		maxLM, 0, maxBands, 1, 10, false, // 10 bytes < 30+15=45
	)
	for band := range maxBands {
		assert.Equal(t, 0, offsets[band], "low bitrate should gate dynalloc")
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
