// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// arSignal generates a length-n AR(2) process x[i] = a1*x[i-1] + a2*x[i-2] + e.
func arSignal(n int, a1, a2 float32, seed uint32) []float32 {
	x := make([]float32, n)
	var prev1, prev2 float32
	state := seed
	for i := range x {
		state = 1664525*state + 1013904223
		e := float32(int32(state>>16)%2000-1000) / 1000
		cur := a1*prev1 + a2*prev2 + e
		x[i] = cur
		prev2, prev1 = prev1, cur
	}

	return x
}

func TestBurgModifiedFLPRecoversAR(t *testing.T) {
	const (
		a1 = 0.5
		a2 = 0.2
		n  = 512
	)
	x := arSignal(n, a1, a2, 42)

	a := make([]float32, 2)
	resNrg := burgModifiedFLP(a, x, 1e-5, n, 1, 2)

	assert.InDelta(t, a1, a[0], 0.12)
	assert.InDelta(t, a2, a[1], 0.12)
	assert.Greater(t, resNrg, float32(0))
	assert.Less(t, resNrg, float32(energyFLP(x, n)), "AR signal should be predictable")
}

func TestBurgModifiedFLPWhiteNoise(t *testing.T) {
	const n = 512
	x := make([]float32, n)
	state := uint32(777)
	for i := range x {
		state = 1664525*state + 1013904223
		x[i] = float32(int32(state>>16)%2000-1000) / 1000
	}

	a := make([]float32, 4)
	burgModifiedFLP(a, x, 1e-5, n, 1, 4)

	// White noise has no predictable structure; coefficients should be small.
	for k, c := range a {
		require.InDeltaf(t, 0, c, 0.2, "coefficient %d", k)
	}
}

func TestFindLPC(t *testing.T) {
	const (
		order       = 16
		nbSubfr     = 4
		subfrLength = 40 + order
	)
	x := arSignal(nbSubfr*subfrLength, 0.6, 0.2, 99)

	nlsf := make([]int16, order)
	findLPC(nlsf, x, 1e-4, subfrLength, nbSubfr, order)

	// A valid NLSF vector: non-decreasing and non-negative (Q15, <= int16 max).
	for k := range nlsf {
		require.GreaterOrEqual(t, nlsf[k], int16(0))
	}
	for k := 1; k < order; k++ {
		require.GreaterOrEqual(t, nlsf[k], nlsf[k-1])
	}
}
