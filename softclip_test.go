// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSoftClipKeepsSamplesInRange(t *testing.T) {
	const n = 480
	x := make([]float32, n*2)
	for i := range n {
		v := float32(1.4 * math.Sin(2*math.Pi*300*float64(i)/48000))
		x[i*2] = v
		x[i*2+1] = -v
	}

	var mem softClipMemory
	softClip(x, 2, &mem)

	for i, v := range x {
		assert.LessOrEqualf(t, math.Abs(float64(v)), 1.0,
			"sample %d left the valid range: %v", i, v)
	}
}

func TestSoftClipLeavesQuietSignalUntouched(t *testing.T) {
	const n = 480
	x := make([]float32, n)
	want := make([]float32, n)
	for i := range n {
		x[i] = float32(0.5 * math.Sin(2*math.Pi*300*float64(i)/48000))
		want[i] = x[i]
	}

	var mem softClipMemory
	softClip(x, 1, &mem)

	assert.Equal(t, want, x, "a signal already inside [-1,1] must pass through unchanged")
}

func TestSoftClipIsContinuousAcrossFrames(t *testing.T) {
	// A peak that straddles the frame boundary must not pick up a step: the
	// carried-over curve is what prevents it.
	const n = 240
	sig := make([]float32, n*2)
	for i := range sig {
		sig[i] = float32(1.3 * math.Sin(2*math.Pi*200*float64(i)/48000))
	}

	var mem softClipMemory
	first := append([]float32(nil), sig[:n]...)
	second := append([]float32(nil), sig[n:]...)
	softClip(first, 1, &mem)
	softClip(second, 1, &mem)

	joined := append(first, second...) //nolint:gocritic // deliberately building the joined signal
	var maxStep float64
	for i := 1; i < len(joined); i++ {
		if step := math.Abs(float64(joined[i] - joined[i-1])); step > maxStep {
			maxStep = step
		}
	}
	assert.Lessf(t, maxStep, 0.1, "soft clipping introduced a discontinuity of %v", maxStep)
}
