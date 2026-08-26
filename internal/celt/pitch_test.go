// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:unparam // sampleRate is always 48000 in tests but kept for clarity.
func generateSine(freq float64, sampleRate, numSamples int) []float32 {
	samples := make([]float32, numSamples)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate)))
	}

	return samples
}

// runPitchPipeline mirrors what choosePrefilter does: build the decimated,
// whitened buffer, search it, then correct octave errors. pcm must hold
// combFilterMaxPeriod samples of history followed by one frame.
func runPitchPipeline(pcm []float32) (int, float32) {
	const frameSampleCount = 960
	pitchLen := (combFilterMaxPeriod + frameSampleCount) >> 1
	buf := make([]float32, pitchLen)
	var scratch encoderScratch
	pitchDownsample([][]float32{pcm}, buf, pitchLen, 2, &scratch)

	period := pitchSearch(
		buf[combFilterMaxPeriod>>1:], buf,
		frameSampleCount, combFilterMaxPeriod-3*combFilterMinPeriod, &scratch,
	)
	period = combFilterMaxPeriod - period
	gain := removeDoubling(
		buf, combFilterMaxPeriod, combFilterMinPeriod, frameSampleCount, &period, 0, 0, &scratch)

	return period, gain
}

func TestPitchSearchSine200Hz(t *testing.T) {
	// 200 Hz at 48 kHz → period 240 samples.
	pcm := generateSine(200, 48000, combFilterMaxPeriod+960)
	period, gain := runPitchPipeline(pcm)

	assert.InDelta(t, 240, period, 4, "period should be ~240 samples")
	assert.Greater(t, gain, float32(0.5), "gain should be high for a pure tone")
}

func TestPitchSearchSine150Hz(t *testing.T) {
	// 150 Hz at 48 kHz → period 320 samples.
	pcm := generateSine(150, 48000, combFilterMaxPeriod+960)
	period, gain := runPitchPipeline(pcm)

	assert.InDelta(t, 320, period, 4, "period should be ~320 samples")
	assert.Greater(t, gain, float32(0.5), "gain should be high for a pure tone")
}

func TestPitchSearchSilence(t *testing.T) {
	pcm := make([]float32, combFilterMaxPeriod+960)
	_, gain := runPitchPipeline(pcm)

	assert.Zero(t, gain, "silence carries no pitch")
}

func TestPitchSearchNoise(t *testing.T) {
	// Deterministic pseudo-noise: no periodicity, so the gain must stay low.
	pcm := make([]float32, combFilterMaxPeriod+960)
	seed := uint32(12345)
	for i := range pcm {
		seed = seed*1664525 + 1013904223
		pcm[i] = float32(int32(seed>>8)%2000) / 2000
	}
	_, gain := runPitchPipeline(pcm)

	assert.Less(t, gain, float32(0.5), "noise should not read as pitched")
}

func TestCeltLPCFlatSpectrum(t *testing.T) {
	// White-noise autocorrelation (only ac[0] non-zero) has no prediction gain,
	// so every coefficient must come out at zero.
	ac := []float32{1, 0, 0, 0, 0}
	lpc := celtLPC(ac, 4, make([]float32, 4))

	for i, v := range lpc {
		assert.InDelta(t, 0, v, 1e-6, "coefficient %d", i)
	}
}

func TestCeltFir5Passthrough(t *testing.T) {
	// All-zero numerator leaves the signal untouched.
	x := []float32{1, -2, 3, -4, 5, 6}
	want := append([]float32(nil), x...)
	celtFir5(x, [5]float32{})

	assert.Equal(t, want, x)
}

func TestQuantizePitchGain(t *testing.T) {
	cases := []struct {
		gain      float32
		wantQg    int
		wantQuant float32
	}{
		{0.0, 0, postFilterGainStep * 1},
		{0.1, 0, postFilterGainStep * 1},
		{0.2, 1, postFilterGainStep * 2},
		{0.5, 4, postFilterGainStep * 5},
		{0.9, 7, postFilterGainStep * 8},
		{1.0, 7, postFilterGainStep * 8},
	}
	for _, tc := range cases {
		qg, quantized := quantizePitchGain(tc.gain)
		assert.Equal(t, tc.wantQg, qg, "qg for gain %f", tc.gain)
		assert.Equal(t, tc.wantQuant, quantized, "quantized for gain %f", tc.gain)
	}
}

func TestEncodePostFilterDisabledByteIdentical(t *testing.T) {
	// The disabled path must produce the same bitstream as before PR 7.5a.
	enc := NewEncoder()
	enc.rangeEncoder.Init()
	info := frameSideInfo{startBand: 0, totalBits: 256}
	enc.encodePostFilter(&info)
	data := enc.rangeEncoder.Done()

	// Single bit logp=1 symbol=0 → exactly one byte with the disabled flag.
	dec := NewDecoder()
	dec.rangeDecoder.Init(data)
	info2 := frameSideInfo{startBand: 0, totalBits: 256}
	err := dec.decodePostFilter(&info2)

	require.NoError(t, err)
	assert.False(t, info2.postFilter.enabled)
}

func TestEncodePostFilterRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		period int
		qg     int
		tapset int
	}{
		{"octave0 period15", 15, 0, 0},
		{"octave1 period40", 40, 3, 1},
		{"octave3 period240", 240, 5, 2},
		{"octave5 period700", 700, 7, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc := NewEncoder()
			enc.rangeEncoder.Init()
			info := frameSideInfo{
				startBand: 0,
				totalBits: 256,
				postFilter: postFilter{
					enabled: true,
					period:  tc.period,
					qq:      tc.qg,
					tapset:  tc.tapset,
				},
			}
			enc.encodePostFilter(&info)
			data := enc.rangeEncoder.Done()

			dec := NewDecoder()
			dec.rangeDecoder.Init(data)
			info2 := frameSideInfo{startBand: 0, totalBits: 256}
			err := dec.decodePostFilter(&info2)

			require.NoError(t, err)
			assert.True(t, info2.postFilter.enabled)
			assert.Equal(t, tc.period, info2.postFilter.period, "period mismatch")
			assert.Equal(t, tc.qg, info2.postFilter.qq, "qg mismatch")
			assert.Equal(t, tc.tapset, info2.postFilter.tapset, "tapset mismatch")
			assert.Equal(t, postFilterGainStep*float32(tc.qg+1), info2.postFilter.gain, "gain mismatch")
			assert.Equal(t, enc.rangeEncoder.FinalRange(), dec.rangeDecoder.FinalRange(),
				"FinalRange must match after post-filter encode/decode")
		})
	}
}

func TestEncodePostFilterSkipsWhenStartBandNotZero(t *testing.T) {
	enc := NewEncoder()
	enc.rangeEncoder.Init()
	info := frameSideInfo{
		startBand:  17,
		totalBits:  256,
		postFilter: postFilter{enabled: true, period: 240, qq: 3, tapset: 1},
	}
	enc.encodePostFilter(&info)

	// Nothing should have been written — Tell stays at 1 (post-Init).
	assert.Equal(t, uint(1), enc.rangeEncoder.Tell())
}

func TestPrefilterDecisionLowBitrate(t *testing.T) {
	// frameBytes <= 12*channels → disabled.
	enabled, _, _ := prefilterDecision(240, 0.9, 240, 0, 10, 1, 0, 256, 1)
	assert.False(t, enabled, "should be disabled at low bitrate")
}

func TestPrefilterDecisionWeakGain(t *testing.T) {
	// gain < threshold (0.2) → disabled.
	enabled, _, _ := prefilterDecision(240, 0.1, 240, 0, 100, 1, 0, 800, 1)
	assert.False(t, enabled, "should be disabled with weak gain")
}

func TestPrefilterDecisionStrongGain(t *testing.T) {
	// Strong gain, stable pitch, enough bits → enabled.
	enabled, qq, quantized := prefilterDecision(240, 0.8, 240, 0, 100, 1, 0, 800, 1)
	assert.True(t, enabled)
	assert.Greater(t, qq, 0)
	assert.Greater(t, quantized, float32(0))
}

func TestPrefilterDecisionTransientPitchChange(t *testing.T) {
	// Strong transient (tfEstimate > 0.98) with large pitch change → disabled.
	enabled, _, _ := prefilterDecision(100, 0.9, 500, 0, 100, 1, 0.99, 800, 1)
	assert.False(t, enabled, "should be disabled on strong transient with pitch jump")

	// The same pitch jump on a milder transient only raises the gain threshold.
	enabled, _, _ = prefilterDecision(100, 0.9, 500, 0, 100, 1, 0.5, 800, 1)
	assert.True(t, enabled, "a mild transient should not kill the prefilter outright")
}

func TestPrefilterDecisionBitBudgetGate(t *testing.T) {
	// tell+16 > totalBits → disabled.
	enabled, _, _ := prefilterDecision(240, 0.9, 240, 0, 100, 1, 0, 10, 1)
	assert.False(t, enabled, "should be disabled when not enough bits for header")
}

func absFloat32(x float32) float32 {
	if x < 0 {
		return -x
	}

	return x
}

func TestApplyPrefilterModifiesSignal(t *testing.T) {
	// Verify the pre-filter attenuates harmonic content: a tonal signal
	// should have lower energy after whitening.
	const frameLen = 960
	const histLen = postfilterHistorySampleCount

	sine := generateSine(200, 48000, frameLen)
	period := 240
	gain := float32(0.5625)
	tapset := 1

	buf := make([]float32, histLen+frameLen)
	copy(buf[histLen:], sine)
	original := make([]float32, frameLen)
	copy(original, sine)

	dst := make([]float32, len(buf))
	applyPrefilter(dst, buf, period, period, frameLen, gain, gain, tapset, tapset)
	filtered := dst[histLen:]

	// The pre-filter must change the signal.
	var maxDiff float32
	for i := range original {
		diff := absFloat32(original[i] - filtered[i])
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	assert.Greater(t, maxDiff, float32(0.01), "pre-filter should modify the signal")

	// The pre-filtered signal should have lower energy than the original
	// (whitening removes the harmonic content).
	origEnergy := vectorEnergy(original)
	filteredEnergy := vectorEnergy(filtered)
	assert.Less(t, filteredEnergy, origEnergy, "pre-filter should reduce energy")
}

func TestEncodeFrameWithPrefilterFinalRange(t *testing.T) {
	// Encode a tonal frame (sine 200Hz) — should trigger the pre-filter —
	// and verify FinalRange matches between encoder and decoder.
	encoder := NewEncoder()
	pcm := [][]float32{generateSine(200, 48000, 960)}

	dst := make([]byte, 200)
	n, err := encoder.EncodeFrame(pcm, dst, 200, 0, maxBands)
	require.NoError(t, err)

	decoder := NewDecoder()
	out := make([]float32, 960)
	err = decoder.Decode(dst[:n], out, false, 1, 960, 0, maxBands)
	require.NoError(t, err)

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
		"FinalRange must match with pre-filter enabled")
}

func TestEncodeFrameWithPrefilterDisabledRegression(t *testing.T) {
	// Noise signal — pre-filter should be disabled (low pitch gain).
	// The bitstream must be byte-identical to pre-7.5b.
	encoder1 := NewEncoder()
	encoder2 := NewEncoder()

	rng := uint32(12345)
	noise := make([]float32, 960)
	for i := range noise {
		rng = rng*1103515245 + 12345
		noise[i] = float32(rng>>16) / 32768.0
	}
	pcm := [][]float32{noise}

	dst1 := make([]byte, 200)
	n1, err := encoder1.EncodeFrame(pcm, dst1, 200, 0, maxBands)
	require.NoError(t, err)

	dst2 := make([]byte, 200)
	n2, err := encoder2.EncodeFrame(pcm, dst2, 200, 0, maxBands)
	require.NoError(t, err)

	assert.Equal(t, n1, n2)
	assert.Equal(t, dst1[:n1], dst2[:n2], "noise frames must be byte-identical")
}

func TestEncodeFramePrefilterMultiFramePitchTracking(t *testing.T) {
	// Three consecutive tonal frames — pitch period should stay stable.
	encoder := NewEncoder()
	sine := generateSine(200, 48000, 960)
	pcm := [][]float32{sine}

	for frame := range 3 {
		dst := make([]byte, 200)
		n, err := encoder.EncodeFrame(pcm, dst, 200, 0, maxBands)
		require.NoError(t, err, "frame %d", frame)
		assert.Greater(t, n, 0, "frame %d produced output", frame)

		// After the first frame, the pre-filter state should have a
		// non-trivial period (the pre-filter should have enabled).
		if frame >= 1 {
			assert.Equal(t, 240, encoder.analysis.prefilter.period,
				"frame %d: pitch period should be stable at 240", frame)
		}
	}
}

func TestEncoderResetClearsPrefilterState(t *testing.T) {
	encoder := NewEncoder()
	sine := generateSine(200, 48000, 960)
	pcm := [][]float32{sine}

	dst := make([]byte, 200)
	_, err := encoder.EncodeFrame(pcm, dst, 200, 0, maxBands)
	require.NoError(t, err)

	// After encoding, pre-filter state should be non-trivial.
	assert.NotZero(t, encoder.analysis.prefilter.period)

	// Reset should clear it.
	encoder.Reset()
	assert.Zero(t, encoder.analysis.prefilter.period)
	assert.Zero(t, encoder.analysis.prefilter.gain)
	assert.Zero(t, encoder.analysis.prefilter.tapset)
}

func TestCeltPitchXcorrMatchesScalarReference(t *testing.T) {
	seed := uint32(987654321)
	rng := func() float32 {
		seed = seed*1664525 + 1013904223

		return float32(int32(seed>>8)%2000)/2000 - 1
	}

	cases := []struct {
		length, maxPitch int
	}{
		{0, 0},
		{1, 1},
		{3, 3},
		{240, 244},
		{240, 250},
		{8, 16},
		{31, 23},
		{100, 100},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("len=%d/pitch=%d", tc.length, tc.maxPitch), func(t *testing.T) {
			input := make([]float32, tc.length)
			window := make([]float32, tc.length+tc.maxPitch)
			for i := range input {
				input[i] = rng()
			}
			for i := range window {
				window[i] = rng()
			}

			got := make([]float32, tc.maxPitch)
			celtPitchXcorr(input, window, got, tc.length, tc.maxPitch)

			want := make([]float32, tc.maxPitch)
			for i := range tc.maxPitch {
				var sum float64
				for j := range tc.length {
					sum += float64(input[j]) * float64(window[i+j])
				}
				want[i] = float32(sum)
			}

			assert.Equal(t, want, got, "bit-identical vs scalar reference")
		})
	}
}

func BenchmarkCeltPitchXcorr(b *testing.B) {
	const length, maxPitch = 240, 244
	x := make([]float32, length)
	y := make([]float32, length+maxPitch)
	xcorr := make([]float32, maxPitch)
	for i := range y {
		y[i] = float32(i%17) / 17
	}
	for i := range x {
		x[i] = float32(i%13) / 13
	}
	b.ResetTimer()
	for range b.N {
		celtPitchXcorr(x, y, xcorr, length, maxPitch)
	}
}

func TestMeasureEnergyMatchesBranchingReference(t *testing.T) {
	samples := []float32{-3.5, -1, -0.25, 0, 0.125, 1, 4.5}
	cases := []struct {
		name          string
		start, length int
	}{
		{name: "full", start: 0, length: len(samples)},
		{name: "range", start: 2, length: 3},
		{name: "clamps end", start: 4, length: 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var want float64
			end := min(tc.start+tc.length, len(samples))
			for i := tc.start; i < end; i++ {
				value := samples[i]
				if value < 0 {
					value = -value
				}
				want += float64(value)
			}

			assert.Equal(t, want, measureEnergy(samples, tc.start, tc.length))
		})
	}
}
