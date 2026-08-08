// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntensityStereoUnbalancedRoundTrip drives the itheta==0 path with a
// strongly unbalanced pair, where the energy-weighted downmix differs from a
// plain mid/side rotation, and checks the range coder stays in sync.
func TestIntensityStereoUnbalancedRoundTrip(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	left := make([]float32, frameSampleCount)
	right := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		v := float32(math.Sin(2 * math.Pi * 700 * float64(i) / sampleRate))
		left[i] = v
		right[i] = 0.05 * v
	}

	out := make([]float32, frameSampleCount*2)
	for frame := range 3 {
		data := encodeFrame(t, &encoder, [][]float32{left, right}, 120)
		require.NotEmptyf(t, data, "frame %d", frame)
		require.NoErrorf(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands),
			"frame %d", frame)
		assert.Equalf(t, encoder.FinalRange(), decoder.FinalRange(),
			"range coder out of sync at frame %d", frame)
	}
}
