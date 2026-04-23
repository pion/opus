// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPVQResidualHelpers(t *testing.T) {
	x := []float32{9, 9, 9}
	normaliseResidual([]int{0, 0, 0}, x, len(x), 0, 1)
	assert.Equal(t, []float32{0, 0, 0}, x)

	normaliseResidual([]int{3, 4}, x, 2, 25, 2)
	assert.InDelta(t, 1.2, x[0], 0.000001)
	assert.InDelta(t, 1.6, x[1], 0.000001)

	assert.Equal(t, uint(1), extractCollapseMask([]int{0, 0}, 2, 1))
	assert.Equal(t, uint(0b101), extractCollapseMask([]int{1, 0, 0, 0, -1, 0}, 6, 3))

	renormaliseVector(x[:2], 2, 1)
	assert.InDelta(t, 1, vectorEnergy(x[:2]), 0.000001)
}

func TestPVQRotation(t *testing.T) {
	x := []float32{1, 2, 3, 4}
	expRotation(x, len(x), -1, 1, 1, spreadNone)
	assert.Equal(t, []float32{1, 2, 3, 4}, x)

	expRotation(x, len(x), -1, 1, 1, spreadNormal)
	assert.NotEqual(t, []float32{1, 2, 3, 4}, x)
	assert.InDelta(t, 30, vectorEnergy(x), 0.0001)

	expRotation(x, len(x), 1, 1, 1, spreadNormal)
	assert.InDelta(t, 30, vectorEnergy(x), 0.0001)
}

func TestAlgUnquant(t *testing.T) {
	decoder := rangeDecoderWithCDFSymbol(0, cwrsUrow(4, 2)[2]+cwrsUrow(4, 2)[3])
	state := bandDecodeState{}
	x := make([]float32, 4)

	mask := algUnquant(x, len(x), 2, spreadNormal, 2, &decoder, 1, &state)

	assert.Equal(t, uint(1), mask)
	assert.InDelta(t, 1, vectorEnergy(x), 0.000001)
	assert.Len(t, state.pulseScratch, len(x))
}

func TestStereoMerge(t *testing.T) {
	x := []float32{1, 0}
	y := []float32{1, 0}
	stereoMerge(x, y, 1, len(x))
	assert.Equal(t, x, y)

	x = []float32{1, 0}
	y = []float32{0, 1}
	stereoMerge(x, y, 0.5, len(x))
	assert.InDelta(t, 1, vectorEnergy(x), 0.000001)
	assert.InDelta(t, 1, vectorEnergy(y), 0.000001)
}

func vectorEnergy(x []float32) float64 {
	energy := float64(0)
	for _, value := range x {
		energy += math.Pow(float64(value), 2)
	}

	return energy
}
