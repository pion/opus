// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
