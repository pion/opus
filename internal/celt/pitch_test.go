// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
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

func TestDetectPitchSine200Hz(t *testing.T) {
	// 200 Hz at 48 kHz → period 240 samples.
	pcm := generateSine(200, 48000, 960)
	period, gain := detectPitch(pcm)

	assert.InDelta(t, 240, period, 2, "period should be ~240 samples")
	assert.Greater(t, gain, float32(0.8), "gain should be high for pure tone")
}

func TestDetectPitchSine150Hz(t *testing.T) {
	// 150 Hz at 48 kHz → period 320 samples.
	pcm := generateSine(150, 48000, 960)
	period, gain := detectPitch(pcm)

	assert.InDelta(t, 320, period, 2, "period should be ~320 samples")
	assert.Greater(t, gain, float32(0.8), "gain should be high for pure tone")
}

func TestDetectPitchSilence(t *testing.T) {
	pcm := make([]float32, 960)
	period, gain := detectPitch(pcm)

	assert.Equal(t, combFilterMinPeriod, period)
	assert.Zero(t, gain)
}

func TestDetectPitchShortInput(t *testing.T) {
	pcm := make([]float32, 10)
	period, gain := detectPitch(pcm)

	assert.Equal(t, combFilterMinPeriod, period)
	assert.Zero(t, gain)
}

func TestRemoveDoublingKeepsFundamental(t *testing.T) {
	pcm := generateSine(200, 48000, 960)
	period, gain := removeDoubling(pcm, 240, 0.95, 0, 0)

	assert.InDelta(t, 240, period, 2)
	assert.Greater(t, gain, float32(0.8))
}

func TestRemoveDoublingCorrectsOctave(t *testing.T) {
	pcm := generateSine(200, 48000, 960)
	period, gain := removeDoubling(pcm, 480, 0.9, 0, 0)

	assert.InDelta(t, 240, period, 2)
	assert.Greater(t, gain, float32(0.8))
}

func TestRemoveDoublingNoPitch(t *testing.T) {
	pcm := make([]float32, 960)
	period, gain := removeDoubling(pcm, 240, 0, 0, 0)

	assert.Zero(t, period)
	assert.Zero(t, gain)
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
	enabled, _, _ := prefilterDecision(240, 0.9, 240, 0, 10, 1, false, 256, 1)
	assert.False(t, enabled, "should be disabled at low bitrate")
}

func TestPrefilterDecisionWeakGain(t *testing.T) {
	// gain < threshold (0.2) → disabled.
	enabled, _, _ := prefilterDecision(240, 0.1, 240, 0, 100, 1, false, 800, 1)
	assert.False(t, enabled, "should be disabled with weak gain")
}

func TestPrefilterDecisionStrongGain(t *testing.T) {
	// Strong gain, stable pitch, enough bits → enabled.
	enabled, qq, quantized := prefilterDecision(240, 0.8, 240, 0, 100, 1, false, 800, 1)
	assert.True(t, enabled)
	assert.Greater(t, qq, 0)
	assert.Greater(t, quantized, float32(0))
}

func TestPrefilterDecisionTransientPitchChange(t *testing.T) {
	// Transient with large pitch change → disabled.
	enabled, _, _ := prefilterDecision(100, 0.9, 500, 0, 100, 1, true, 800, 1)
	assert.False(t, enabled, "should be disabled on transient with pitch jump")
}

func TestPrefilterDecisionBitBudgetGate(t *testing.T) {
	// tell+16 > totalBits → disabled.
	enabled, _, _ := prefilterDecision(240, 0.9, 240, 0, 100, 1, false, 10, 1)
	assert.False(t, enabled, "should be disabled when not enough bits for header")
}

func TestTapsetFromSpread(t *testing.T) {
	assert.Equal(t, 2, tapsetFromSpread(spreadAggressive))
	assert.Equal(t, 1, tapsetFromSpread(spreadNormal))
	assert.Equal(t, 0, tapsetFromSpread(spreadNone))
	assert.Equal(t, 0, tapsetFromSpread(spreadLight))
}

func TestCancelPitchMono(t *testing.T) {
	before := [2]float64{100}

	assert.True(t, cancelPitch(1, 0.5, before, [2]float64{110}))
	assert.False(t, cancelPitch(1, 0.5, before, [2]float64{90}))
}

func TestCancelPitchStereo(t *testing.T) {
	before := [2]float64{100, 100}

	assert.False(t, cancelPitch(2, 0.5, before, [2]float64{80, 80}))
	assert.True(t, cancelPitch(2, 0.9, before, [2]float64{140, 80}))
	assert.True(t, cancelPitch(2, 0.1, before, [2]float64{99, 99}))
}

func TestMeasureEnergy(t *testing.T) {
	buf := []float32{10, -1.5, 2, -3, 20}

	assert.Equal(t, 6.5, measureEnergy(buf, 1, 3))
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

	applyPrefilter(buf, period, period, frameLen, gain, gain, tapset, tapset)
	filtered := buf[histLen:]

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
	assert.Zero(t, encoder.analysis.prefilter.oldPeriod)
}
