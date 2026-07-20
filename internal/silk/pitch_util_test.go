// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResamplerDown2(t *testing.T) {
	in := make([]int16, 64)
	for i := range in {
		in[i] = 1000
	}
	out := make([]int16, len(in)/2)
	var state [2]int32
	resamplerDown2(&state, out, in)

	// A DC input should settle near the same DC level after the transient.
	assert.InDelta(t, 1000, out[len(out)-1], 30)

	// Deterministic for identical state and input.
	out2 := make([]int16, len(in)/2)
	var state2 [2]int32
	resamplerDown2(&state2, out2, in)
	assert.Equal(t, out, out2)
}

func TestFloatShortConversion(t *testing.T) {
	out16 := make([]int16, 4)
	float2ShortArray(out16, []float32{0.4, 1.6, -2.5, 100000})
	assert.Equal(t, int16(0), out16[0])
	assert.Equal(t, int16(2), out16[1])
	assert.Equal(t, int16(-2), out16[2]) // round half to even
	assert.Equal(t, int16(math.MaxInt16), out16[3])

	outF := make([]float32, 2)
	short2FloatArray(outF, []int16{-5, 7})
	assert.Equal(t, []float32{-5, 7}, outF)
}

func TestEnergyFLP(t *testing.T) {
	assert.InDelta(t, 1+4+9, energyFLP([]float32{1, 2, 3}, 3), 1e-9)
}

func TestPitchXcorr(t *testing.T) {
	// A period-4 signal correlates most strongly at lag 4.
	length := 32
	sig := make([]float32, length+8)
	for i := range sig {
		sig[i] = float32(math.Sin(2 * math.Pi * float64(i) / 4))
	}
	xcorr := make([]float32, 8)
	pitchXcorr(sig, sig, xcorr, length, 8)
	assert.Greater(t, xcorr[4], xcorr[1])
	assert.Greater(t, xcorr[4], xcorr[2])
	assert.Greater(t, xcorr[4], xcorr[3])
}

func TestInsertionSortDecreasingFLP(t *testing.T) {
	a := []float32{3, 1, 4, 1, 5, 9, 2, 6}
	idx := make([]int, 4)
	insertionSortDecreasingFLP(a, idx, len(a), 4)
	assert.Equal(t, []float32{9, 6, 5, 4}, a[:4])
	assert.Equal(t, 5, idx[0]) // 9 was at index 5
	assert.Equal(t, 7, idx[1]) // 6 was at index 7
	assert.Equal(t, 4, idx[2]) // 5 was at index 4
	assert.Equal(t, 2, idx[3]) // 4 was at index 2
}

func TestInsertionSortIncreasing(t *testing.T) {
	a := []int32{3, 1, 4, 1, 5, 9, 2, 6}
	idx := make([]int, 4)
	insertionSortIncreasing(a, idx, len(a), 4)
	require.Equal(t, []int32{1, 1, 2, 3}, a[:4])
	assert.Equal(t, 6, idx[2]) // value 2 was at index 6
	assert.Equal(t, 0, idx[3]) // value 3 was at index 0
}
