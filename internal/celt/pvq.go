// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // PVQ math uses RFC/reference scalar and vector names.
package celt

import (
	"math"

	"github.com/pion/opus/internal/rangecoding"
)

const (
	spreadNone       = 0
	spreadLight      = 1
	spreadNormal     = 2
	spreadAggressive = 3
	normScaling      = 1
)

func algUnquant(
	x []float32,
	n int,
	k int,
	spread int,
	blocks int,
	rangeDecoder *rangecoding.Decoder,
	gain float32,
) uint {
	iy := make([]int, n)
	decodePulses(iy, n, k, rangeDecoder)

	energy := 0
	for i := range n {
		energy += iy[i] * iy[i]
	}
	normaliseResidual(iy, x, n, energy, gain)
	expRotation(x, n, -1, blocks, k, spread)

	return extractCollapseMask(iy, n, blocks)
}

func normaliseResidual(iy []int, x []float32, n int, energy int, gain float32) {
	if energy <= 0 {
		for i := range n {
			x[i] = 0
		}

		return
	}

	scale := gain / float32(math.Sqrt(float64(energy)))
	for i := range n {
		x[i] = float32(iy[i]) * scale
	}
}

func extractCollapseMask(iy []int, n int, blocks int) uint {
	if blocks <= 1 {
		return 1
	}

	blockSize := n / blocks
	mask := uint(0)
	for block := range blocks {
		for i := range blockSize {
			if iy[block*blockSize+i] != 0 {
				mask |= 1 << block
			}
		}
	}

	return mask
}

func renormaliseVector(x []float32, n int, gain float32) {
	energy := float32(1e-27)
	for i := range n {
		energy += x[i] * x[i]
	}

	scale := gain / float32(math.Sqrt(float64(energy)))
	for i := range n {
		x[i] *= scale
	}
}

func expRotation(x []float32, length int, direction int, stride int, pulses int, spread int) {
	if 2*pulses >= length || spread == spreadNone {
		return
	}

	factors := [...]int{15, 10, 5}
	factor := factors[spread-1]
	gain := float64(length) / float64(length+factor*pulses)
	theta := 0.5 * gain * gain
	c := float32(math.Cos(0.5 * math.Pi * theta))
	s := float32(math.Sin(0.5 * math.Pi * theta))

	stride2 := 0
	if length >= 8*stride {
		stride2 = 1
		for (stride2*stride2+stride2)*stride+(stride>>2) < length {
			stride2++
		}
	}

	blockLen := length / stride
	for block := range stride {
		segment := x[block*blockLen : (block+1)*blockLen]
		if direction < 0 {
			if stride2 != 0 {
				expRotation1(segment, blockLen, stride2, s, c)
			}
			expRotation1(segment, blockLen, 1, c, s)
		} else {
			expRotation1(segment, blockLen, 1, c, -s)
			if stride2 != 0 {
				expRotation1(segment, blockLen, stride2, s, -c)
			}
		}
	}
}

func expRotation1(x []float32, length int, stride int, c float32, s float32) {
	for i := 0; i < length-stride; i++ {
		x1 := x[i]
		x2 := x[i+stride]
		x[i+stride] = c*x2 + s*x1
		x[i] = c*x1 - s*x2
	}
	for i := length - 2*stride - 1; i >= 0; i-- {
		x1 := x[i]
		x2 := x[i+stride]
		x[i+stride] = c*x2 + s*x1
		x[i] = c*x1 - s*x2
	}
}
