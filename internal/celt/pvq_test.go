// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/pion/opus/internal/rangecoding"
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

func TestPVQSearchBasic(t *testing.T) {
	// Target with energy in first few dimensions
	x := []float32{3, 2, 1, 0}
	iy := pvqSearch(x, len(x), 3)

	pulses := 0
	for _, v := range iy {
		if v < 0 {
			pulses -= v
		} else {
			pulses += v
		}
	}
	assert.Equal(t, 3, pulses)

	// Signs should match target
	assert.Greater(t, iy[0], 0)
	assert.Greater(t, iy[1], 0)
}

func TestPVQSearchZeroPulses(t *testing.T) {
	x := []float32{1, 2, 3}
	iy := pvqSearch(x, len(x), 0)
	for _, v := range iy {
		assert.Equal(t, 0, v)
	}
}

func TestAlgQuantRoundTrip(t *testing.T) {
	n := 4
	pulseCount := 2
	spread := spreadNormal
	gain := float32(2)

	// Original target
	original := []float32{3, 1, 0, -1}

	// Encode
	var enc rangecoding.Encoder
	enc.Init()
	xEnc := make([]float32, n)
	copy(xEnc, original)
	mask := algQuant(xEnc, n, pulseCount, spread, 1, &enc, gain)
	assert.NotZero(t, mask)

	bits := enc.Done()

	// Decode
	var dec rangecoding.Decoder
	dec.Init(bits)
	state := bandDecodeState{}
	xDec := make([]float32, n)
	algUnquant(xDec, n, pulseCount, spread, 1, &dec, gain, &state)

	// Encoder output and decoder output should match
	for i := range n {
		assert.InDelta(t, xEnc[i], xDec[i], 0.0001)
	}
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
