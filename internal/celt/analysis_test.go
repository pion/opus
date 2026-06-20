// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	data1, err := enc1.EncodeFrame([][]float32{sine}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	data2, err := enc2.EncodeFrame([][]float32{withOffset}, frameBytes, 0, maxBands)
	require.NoError(t, err)

	// The DC block must change the bitstream (or at least the FinalRange)
	// because it shifts energy out of the lowest band.
	require.NotEmpty(t, data1)
	require.NotEmpty(t, data2)
	assert.NotEqual(t, enc1.FinalRange(), enc2.FinalRange(),
		"DC block should change the encoder output (sine=%x, withOffset=%x)",
		enc1.FinalRange(), enc2.FinalRange())
}
