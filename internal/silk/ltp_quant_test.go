// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLog2Lin checks log2lin against its float inverse, math.Log2, over a
// representative Q7 range (silk_log2lin approximates 2^(x/128)).
func TestLog2Lin(t *testing.T) {
	assert.Zero(t, log2lin(-1))
	assert.Equal(t, int32(math.MaxInt32), log2lin(3967))

	for _, inLogQ7 := range []int32{0, 128, 256, 1000, 2048, 3000} {
		got := float64(log2lin(inLogQ7))
		want := math.Pow(2, float64(inLogQ7)/128.0)
		assert.InEpsilonf(t, want, got, 0.03, "log2lin(%d)", inLogQ7)
	}
}

// TestVQWMatEC feeds a real (not synthetic) correlation matrix/vector — built
// from an actual signal via corrMatrixFLP/corrVectorFLP, the same way
// quantLTPGains will once it's wired to *Encoder — and checks the returned
// index, energy, and gain are self-consistent, exercising the fixed-point
// codebook search on physically meaningful input rather than arbitrary noise.
func TestVQWMatEC(t *testing.T) {
	const (
		subfrLength = 40
		order       = ltpOrder
	)
	// A short periodic-ish signal so the correlation matrix isn't degenerate.
	x := make([]float32, subfrLength+order-1)
	for i := range x {
		x[i] = float32(10 * math.Sin(2*math.Pi*float64(i)/17))
	}

	xxFLP := make([]float32, order*order)
	xXFLP := make([]float32, order)
	corrMatrixFLP(x, subfrLength, order, xxFLP)
	corrVectorFLP(x, x[order-1:], subfrLength, order, xXFLP)

	xxQ17 := make([]int32, order*order)
	for i, v := range xxFLP {
		xxQ17[i] = int32(math.RoundToEven(float64(v) * 131072.0))
	}
	xXQ17 := make([]int32, order)
	for i, v := range xXFLP {
		xXQ17[i] = int32(math.RoundToEven(float64(v) * 131072.0))
	}

	for k := range nLTPCodebooks {
		cb := ltpCodebook(k)
		cbGain := ltpGainTable(k)
		clQ5 := ltpBitsTable(k)
		size := ltpVQSizes[k]

		ind, resNrgQ15, rateDistQ8, gainQ7 := vqWMatEC(xxQ17, xXQ17, cb, cbGain, clQ5, subfrLength, 1<<20, size)

		require.GreaterOrEqualf(t, ind, 0, "codebook %d index", k)
		require.Lessf(t, ind, size, "codebook %d index", k)
		assert.NotEqual(t, int32(math.MaxInt32), resNrgQ15, "codebook %d: no candidate scored", k)
		assert.NotEqual(t, int32(math.MaxInt32), rateDistQ8, "codebook %d: no candidate scored", k)
		assert.Equal(t, int32(cbGain[ind]), gainQ7, "codebook %d: gain matches winning row", k)
	}
}
