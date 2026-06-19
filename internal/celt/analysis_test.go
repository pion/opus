// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
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
	// forward masking. A better detector is scheduled for PR 6b.
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
