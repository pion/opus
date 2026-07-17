// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrVectorFLP(t *testing.T) {
	order := 3
	l := 4
	x := []float32{1, 2, 3, 4, 5, 6} // length l+order-1
	target := []float32{1, 1, 1, 1}
	xt := make([]float32, order)
	corrVectorFLP(x, target, l, order, xt)

	// lag 0 sums x[order-1 .. order-1+l) = x[2..6) = 3+4+5+6
	assert.InDelta(t, 3+4+5+6, xt[0], 1e-4)
	assert.InDelta(t, 2+3+4+5, xt[1], 1e-4)
	assert.InDelta(t, 1+2+3+4, xt[2], 1e-4)
}

func TestCorrMatrixFLP(t *testing.T) {
	order := 3
	l := 4
	x := []float32{1, 2, 3, 4, 5, 6}
	xx := make([]float32, order*order)
	corrMatrixFLP(x, l, order, xx)

	// Diagonal [0][0] = energy of x[2..6) = 9+16+25+36 = 86.
	assert.InDelta(t, 86, xx[0], 1e-3)
	// Symmetric.
	for i := range order {
		for j := range order {
			require.InDeltaf(t, xx[i*order+j], xx[j*order+i], 1e-3, "asymmetry at %d,%d", i, j)
		}
	}
}

func TestFindLTPFLP(t *testing.T) {
	const (
		nbSubfr     = 4
		subfrLength = 40
		maxLag      = 120
	)
	// Residual buffer with history ahead of the frame start.
	total := maxLag + ltpOrder + nbSubfr*subfrLength + ltpOrder
	r := make([]float32, total)
	state := uint32(2024)
	for i := range r {
		state = 1664525*state + 1013904223
		r[i] = float32(int32(state>>16)%2000-1000) / 1000
	}
	rOffset := maxLag + ltpOrder
	lag := []int{80, 82, 78, 81}

	xx := make([]float32, nbSubfr*ltpMatrixSize)
	xX := make([]float32, nbSubfr*ltpOrder)
	findLTPFLP(xx, xX, r, rOffset, lag, subfrLength, nbSubfr)

	// Each subframe matrix is finite and symmetric.
	for k := range nbSubfr {
		base := k * ltpMatrixSize
		for i := range ltpOrder {
			for j := range ltpOrder {
				v := xx[base+i*ltpOrder+j]
				require.Falsef(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0), "non-finite at subfr %d", k)
				require.InDeltaf(t, xx[base+i*ltpOrder+j], xx[base+j*ltpOrder+i], 1e-5, "asymmetry subfr %d", k)
			}
		}
	}
}
