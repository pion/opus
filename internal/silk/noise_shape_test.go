// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSigmoid(t *testing.T) {
	assert.InDelta(t, 0.5, sigmoid(0), 1e-6)
	assert.Greater(t, sigmoid(2), sigmoid(0))
	assert.Less(t, sigmoid(-2), sigmoid(0))
	assert.InDelta(t, 1, sigmoid(20), 1e-6)
	assert.InDelta(t, 0, sigmoid(-20), 1e-6)
}

// TestWarpedAutocorrelationZeroWarping checks that with no warping the result
// matches the regular autocorrelation.
func TestWarpedAutocorrelationZeroWarping(t *testing.T) {
	const (
		length = 64
		order  = 8
	)
	x := make([]float32, length)
	state := uint32(7)
	for i := range x {
		state = 1664525*state + 1013904223
		x[i] = float32(int32(state>>16)%2000-1000) / 1000
	}

	warped := make([]float32, order+1)
	warpedAutocorrelationFLP(warped, x, 0, length, order)

	regular := make([]float32, order+1)
	autocorrelationFLP(regular, x, length, order+1)

	for i := range warped {
		assert.InDeltaf(t, regular[i], warped[i], 1e-3, "tap %d", i)
	}
}
