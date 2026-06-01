// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // PVQ math uses RFC/reference scalar and vector names.
package celt

import (
	"math"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/pion/opus/internal/slicetools"
)

const (
	spreadNone       = 0
	spreadLight      = 1
	spreadNormal     = 2
	spreadAggressive = 3
	normScaling      = 1
)

// algUnquant decodes the RFC 6716 Section 4.3.4.2 PVQ pulse vector, scales it
// to the requested gain, and applies Section 4.3.4.3 spreading rotation.
func algUnquant(
	x []float32,
	n int,
	k int,
	spread int,
	blocks int,
	rangeDecoder *rangecoding.Decoder,
	gain float32,
	state *bandDecodeState,
) uint {
	iy := slicetools.Resize(&state.pulseScratch, n)
	decodePulses(iy, n, k, rangeDecoder, state.cwrsRows)

	energy, collapseMask := pulseEnergyAndCollapseMask(iy, n, blocks)
	normaliseResidual(iy, x, n, energy, gain)
	expRotation(x, n, -1, blocks, k, spread)

	return collapseMask
}

// normaliseResidual maps integer PVQ pulses back to a floating-point unit
// vector while preserving the band gain supplied by the split decoder.
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

// extractCollapseMask records which transient blocks received non-zero pulses.
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

func pulseEnergyAndCollapseMask(iy []int, n int, blocks int) (energy int, mask uint) {
	if blocks <= 1 {
		for i := range n {
			energy += iy[i] * iy[i]
		}

		return energy, 1
	}

	blockSize := n / blocks
	for block := range blocks {
		for i := range blockSize {
			pulse := iy[block*blockSize+i]
			energy += pulse * pulse
			if pulse != 0 {
				mask |= 1 << block
			}
		}
	}

	return energy, mask
}

// renormaliseVector restores unit energy after lowband folding or noise fill.
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

// expRotation applies RFC 6716 Section 4.3.4.3 spreading rotation. Direction is
// negative when undoing the encoder rotation during decode.
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

// pvqSearch finds the best PVQ pulse vector y for target x with K pulses.
// Uses greedy per-pulse allocation: each pulse goes to the dimension that
// maximizes (x[i]·y[i])^2 / ||y||^2.
func pvqSearch(x []float32, n, k int) []int {
	y := make([]int, n)

	absX := make([]float32, n)
	sign := make([]float32, n)
	for i := range n {
		if x[i] >= 0 {
			absX[i] = x[i]
			sign[i] = 1
		} else {
			absX[i] = -x[i]
			sign[i] = -1
		}
	}

	var dot, ener float32
	for range k {
		bestScore := float32(-1)
		bestIdx := 0
		for i := range n {
			newDot := dot + absX[i]
			newEner := ener + float32(2*y[i]+1)
			score := (newDot * newDot) / newEner
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		y[bestIdx]++
		dot += absX[bestIdx]
		ener += float32(2*y[bestIdx] - 1)
	}

	for i := range n {
		if sign[i] < 0 {
			y[i] = -y[i]
		}
	}

	return y
}

// algQuant encodes the PVQ pulse vector for band shape and writes it to the
// range encoder. It is the encoder-side inverse of algUnquant.
func algQuant(
	x []float32,
	n, k int,
	spread int,
	blocks int,
	rangeEncoder *rangecoding.Encoder,
	gain float32,
) uint {
	expRotation(x, n, 1, blocks, k, spread)

	iy := pvqSearch(x, n, k)
	encodePulses(iy, n, k, rangeEncoder)

	energy := 0
	for i := range n {
		energy += iy[i] * iy[i]
	}
	normaliseResidual(iy, x, n, energy, gain)
	expRotation(x, n, -1, blocks, k, spread)

	return extractCollapseMask(iy, n, blocks)
}

func expRotation1(x []float32, length int, stride int, c float32, s float32) {
	if length <= stride {
		return
	}

	lower := x[:length-stride]
	upper := x[stride:length]
	for i := range lower {
		x1 := lower[i]
		x2 := upper[i]
		upper[i] = c*x2 + s*x1
		lower[i] = c*x1 - s*x2
	}

	backwardLength := len(lower) - stride
	if backwardLength <= 0 {
		return
	}
	backwardLower := lower[:backwardLength]
	backwardUpper := upper[:backwardLength]
	// slices.Backward adds iterator overhead in this hot loop.
	//nolint:modernize
	for i := backwardLength - 1; i >= 0; i-- {
		x1 := backwardLower[i]
		x2 := backwardUpper[i]
		backwardUpper[i] = c*x2 + s*x1
		backwardLower[i] = c*x1 - s*x2
	}
}
