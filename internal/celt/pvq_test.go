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

func TestExpRotation1BlockOfFour(t *testing.T) {
	for _, test := range []struct {
		length int
		stride int
	}{
		{length: 8, stride: 4},
		{length: 13, stride: 4},
		{length: 18, stride: 5},
		{length: 20, stride: 7},
	} {
		got := make([]float32, test.length)
		for i := range got {
			got[i] = float32(i) - 10
		}
		want := make([]float32, len(got))
		copy(want, got)

		expRotation1Scalar(want, test.length, test.stride, 0.9, 0.4)
		expRotation1(got, test.length, test.stride, 0.9, 0.4)

		assert.Equal(t, want, got)
	}
}

func expRotation1Scalar(x []float32, length int, stride int, cosine float32, sine float32) {
	lower := x[:length-stride]
	upper := x[stride:length]
	for i := range lower {
		x1 := lower[i]
		x2 := upper[i]
		upper[i] = cosine*x2 + sine*x1
		lower[i] = cosine*x1 - sine*x2
	}

	backwardLength := len(lower) - stride
	if backwardLength <= 0 {
		return
	}
	for i := backwardLength - 1; i >= 0; i-- {
		x1 := lower[i]
		x2 := upper[i]
		upper[i] = cosine*x2 + sine*x1
		lower[i] = cosine*x1 - sine*x2
	}
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
	yScratch := make([]int, len(x))
	yFloat := make([]float32, len(x))
	absX := make([]float32, len(x))
	sign := make([]float32, len(x))
	iy := pvqSearch(x, len(x), 3, yScratch, yFloat, absX, sign)

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
	yScratch := make([]int, len(x))
	yFloat := make([]float32, len(x))
	absX := make([]float32, len(x))
	sign := make([]float32, len(x))
	iy := pvqSearch(x, len(x), 0, yScratch, yFloat, absX, sign)
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
	yScratch := make([]int, n)
	yFloat := make([]float32, n)
	absX := make([]float32, n)
	sign := make([]float32, n)
	cwrsScratch := make([]uint32, cwrsMaxPulseCount+2)
	mask := algQuant(xEnc, n, pulseCount, spread, 1, &enc, gain, yScratch, yFloat, absX, sign, cwrsScratch)
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

func TestPVQSearchMatchesScalarReference(t *testing.T) {
	wide := make([]float32, 48)
	for i := range wide {
		wide[i] = float32(i%9-4) / 4
	}

	cases := []struct {
		name       string
		input      []float32
		pulseCount int
	}{
		{name: "zero pulses", input: []float32{1, -2, 3}, pulseCount: 0},
		{name: "mixed signs", input: []float32{3, -2, 1, -0.5}, pulseCount: 5},
		{name: "repeated pulse", input: []float32{8, 1, -0.5, 0.25}, pulseCount: 12},
		{name: "wide band", input: wide, pulseCount: 48},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := len(tc.input)
			got := pvqSearch(
				tc.input,
				n,
				tc.pulseCount,
				make([]int, n),
				make([]float32, n),
				make([]float32, n),
				make([]float32, n),
			)
			want := pvqSearchScalarReference(tc.input, n, tc.pulseCount)
			assert.Equal(t, want, got)
		})
	}
}

func vectorEnergy(x []float32) float64 {
	energy := float64(0)
	for _, value := range x {
		energy += math.Pow(float64(value), 2)
	}

	return energy
}

func pvqSearchScalarReference(input []float32, n, pulseCount int) []int {
	vector := make([]int, n)
	absX := make([]float32, n)
	sign := make([]float32, n)
	for i := range n {
		if input[i] >= 0 {
			absX[i] = input[i]
			sign[i] = 1
		} else {
			absX[i] = -input[i]
			sign[i] = -1
		}
	}

	var dot, ener float32
	for range pulseCount {
		bestScore := float32(-1)
		bestIdx := 0
		for i := range n {
			newDot := dot + absX[i]
			newEner := ener + float32(2*vector[i]+1)
			score := (newDot * newDot) / newEner
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		vector[bestIdx]++
		dot += absX[bestIdx]
		ener += float32(2*vector[bestIdx] - 1)
	}
	for i := range n {
		if sign[i] < 0 {
			vector[i] = -vector[i]
		}
	}

	return vector
}
