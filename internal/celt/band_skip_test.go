// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bandSkipSignal builds a broadband stereo frame: a harmonic stack plus a noise
// floor, so the top bands carry enough energy to reach the skip decision.
func bandSkipSignal(offset int) [][]float32 {
	left := make([]float32, maxFrameSampleCount)
	right := make([]float32, maxFrameSampleCount)
	seed := uint32(12345 + offset) //nolint:gosec // G115: test offsets are small and positive.
	for i := range left {
		t := float64(i+offset) / sampleRate
		var v float64
		for h := 1; h <= 12; h++ {
			v += (1.0 / float64(h)) * math.Sin(2*math.Pi*220*float64(h)*t)
		}
		seed = 1664525*seed + 1013904223
		noise := float64(int32(seed>>8)%2000-1000) / 1000.0
		left[i] = float32(0.25*v + 0.02*noise)
		right[i] = float32(0.25*v*0.9 + 0.02*noise)
	}

	return [][]float32{left, right}
}

// TestBandSkipRoundTrip drives the encoder at a bitrate where the depth
// threshold skips the top band and checks the decoder stays in sync. The skip
// bits are written inside the allocation walk, so a desync here would mean the
// decoder read them in a different order than the encoder wrote them.
func TestBandSkipRoundTrip(t *testing.T) {
	const frameBytes = 240 // 96 kbps at 20 ms

	encoder := NewEncoder()
	decoder := &Decoder{}
	decoder.Reset()

	packet := make([]byte, frameBytes)
	out := make([]float32, 2*maxFrameSampleCount)
	skipped := false
	for frame := range 10 {
		n, err := encoder.EncodeFrame(bandSkipSignal(frame*maxFrameSampleCount), packet, frameBytes, 0, maxBands)
		require.NoErrorf(t, err, "frame %d", frame)

		require.NoErrorf(t,
			decoder.Decode(packet[:n], out, true, 2, maxFrameSampleCount, 0, maxBands), "frame %d", frame)
		require.Equalf(t, encoder.FinalRange(), decoder.FinalRange(), "frame %d: range coder desync", frame)

		if encoder.lastCodedBands < maxBands {
			skipped = true
		}
	}
	assert.True(t, skipped, "expected the depth threshold to skip at least one band at 96 kbps stereo")
}

// TestBandSkipHysteresisSeed checks the first frame seeds lastCodedBands
// directly instead of taking the one-step clamp, which would otherwise pin the
// count near zero for several frames (libopus celt_encoder.c).
func TestBandSkipHysteresisSeed(t *testing.T) {
	encoder := NewEncoder()
	require.Zero(t, encoder.lastCodedBands, "a fresh encoder has no previous frame")

	packet := make([]byte, 240)
	_, err := encoder.EncodeFrame(bandSkipSignal(0), packet, 240, 0, maxBands)
	require.NoError(t, err)

	assert.Greater(t, encoder.lastCodedBands, 1,
		"the first frame must seed the band count directly, not clamp from zero")
}

// TestBandSkipResetClearsHysteresis checks Reset clears the carry-over so a
// reused encoder does not inherit the previous stream's band count.
func TestBandSkipResetClearsHysteresis(t *testing.T) {
	encoder := NewEncoder()
	packet := make([]byte, 240)
	_, err := encoder.EncodeFrame(bandSkipSignal(0), packet, 240, 0, maxBands)
	require.NoError(t, err)
	require.NotZero(t, encoder.lastCodedBands)

	encoder.Reset()
	assert.Zero(t, encoder.lastCodedBands)
}
