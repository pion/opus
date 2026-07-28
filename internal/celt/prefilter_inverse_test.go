// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPrefilterInvertsPostfilter checks the encoder's prefilter and the
// decoder's postfilter cancel out at every gain.
//
// They only do so because the prefilter reads unfiltered input while the
// postfilter reads its own output: running both in place would make each a
// recursive filter, and the pair would amplify by 1/(1-g^2) instead of
// returning the signal unchanged — over 2x at the gains a periodic signal
// reaches.
func TestPrefilterInvertsPostfilter(t *testing.T) {
	const (
		start  = postfilterHistorySampleCount
		count  = maxFrameSampleCount
		period = 48 // one cycle of the 1 kHz tone below
	)

	for _, gain := range []float32{0.09375, 0.25, 0.5, 0.75, 0.9} {
		src := make([]float32, start+count+8)
		for i := range src {
			src[i] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000))
		}
		original := append([]float32(nil), src...)

		filtered := make([]float32, len(src))
		copy(filtered, src)
		applyPrefilter(filtered, src, period, period, count, gain, gain, 0, 0)

		// The postfilter runs in place, the way the decoder applies it.
		combFilter(filtered, filtered, start, period, period, count, gain, gain, 0, 0)

		var signal, residual float64
		for i := start; i < start+count; i++ {
			diff := float64(original[i] - filtered[i])
			signal += float64(original[i]) * float64(original[i])
			residual += diff * diff
		}
		assert.InDeltaf(t, 1.0, math.Sqrt(1+residual/signal), 1e-3,
			"gain %v: prefilter and postfilter must cancel", gain)
	}
}

// TestPrefilterWhitensPeriodicSignal checks the prefilter actually removes
// energy from a strongly periodic signal, which is what frees bits for the
// rest of the spectrum.
func TestPrefilterWhitensPeriodicSignal(t *testing.T) {
	const (
		start  = postfilterHistorySampleCount
		count  = maxFrameSampleCount
		period = 48
	)
	src := make([]float32, start+count+8)
	for i := range src {
		src[i] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000))
	}
	filtered := make([]float32, len(src))
	copy(filtered, src)
	applyPrefilter(filtered, src, period, period, count, 0.75, 0.75, 0, 0)

	var before, after float64
	for i := start; i < start+count; i++ {
		before += float64(src[i]) * float64(src[i])
		after += float64(filtered[i]) * float64(filtered[i])
	}
	assert.Lessf(t, after, before/2, "prefilter should strongly attenuate a periodic signal")
}
