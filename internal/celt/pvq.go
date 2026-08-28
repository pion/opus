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

// pvqSearch finds the nearest PVQ lattice point to x with k pulses and writes
// it into yScratch[:n]. I clear y before the greedy loop because the scratch
// slice is reused across frames and stale values from a wider band would
// corrupt the pulse counts. All scratch slices must have cap >= n.
func pvqSearch(
	x []float32,
	n, k int,
	yScratch []int,
	yFloatScratch, absXScratch, signScratch []float32,
) []int {
	y := yScratch[:n:n]
	yf := yFloatScratch[:n:n]
	absX := absXScratch[:n:n]
	sign := signScratch[:n:n]
	clear(y)
	for i := range n {
		yf[i] = 1
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
			newEner := ener + yf[i]
			score := (newDot * newDot) / newEner
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		dot += absX[bestIdx]
		ener += yf[bestIdx]
		yf[bestIdx] += 2
		y[bestIdx]++
	}

	for i := range n {
		if sign[i] < 0 {
			y[i] = -y[i]
		}
	}

	return y
}

// algQuant quantises x onto the PVQ lattice with k pulses and writes the
// codeword to the range encoder. It is the encoder-side mirror of algUnquant.
func algQuant(
	x []float32,
	n, k int,
	spread int,
	blocks int,
	rangeEncoder *rangecoding.Encoder,
	gain float32,
	yScratch []int,
	yFloatScratch, absXScratch, signScratch []float32,
	cwrsScratch []uint32,
) uint {
	expRotation(x, n, 1, blocks, k, spread)

	iy := pvqSearch(x, n, k, yScratch, yFloatScratch, absXScratch, signScratch)
	encodePulses(iy, n, k, rangeEncoder, cwrsScratch)

	energy := 0
	for i := range n {
		energy += iy[i] * iy[i]
	}
	normaliseResidual(iy, x, n, energy, gain)
	expRotation(x, n, -1, blocks, k, spread)

	return extractCollapseMask(iy, n, blocks)
}

//nolint:cyclop // The unrolled forward and backward hot paths stay adjacent.
func expRotation1(x []float32, length int, stride int, c float32, s float32) {
	if length <= stride {
		return
	}

	lower := x[:length-stride]
	upper := x[stride:length]
	if stride >= 4 {
		n := len(lower)
		i := 0
		for ; i+4 <= n; i += 4 {
			l0, l1, l2, l3 := lower[i], lower[i+1], lower[i+2], lower[i+3]
			u0, u1, u2, u3 := upper[i], upper[i+1], upper[i+2], upper[i+3]
			upper[i], upper[i+1], upper[i+2], upper[i+3] = c*u0+s*l0, c*u1+s*l1, c*u2+s*l2, c*u3+s*l3
			lower[i], lower[i+1], lower[i+2], lower[i+3] = c*l0-s*u0, c*l1-s*u1, c*l2-s*u2, c*l3-s*u3
		}
		for ; i < n; i++ {
			x1 := lower[i]
			x2 := upper[i]
			upper[i] = c*x2 + s*x1
			lower[i] = c*x1 - s*x2
		}
	} else {
		for i := range lower {
			x1 := lower[i]
			x2 := upper[i]
			upper[i] = c*x2 + s*x1
			lower[i] = c*x1 - s*x2
		}
	}

	backwardLength := len(lower) - stride
	if backwardLength <= 0 {
		return
	}
	backwardLower := lower[:backwardLength]
	backwardUpper := upper[:backwardLength]
	// slices.Backward adds iterator overhead in this hot loop.
	//nolint:modernize
	if stride >= 4 {
		i := backwardLength - 1
		for ; i >= 3; i -= 4 {
			l0, l1, l2, l3 := backwardLower[i], backwardLower[i-1], backwardLower[i-2], backwardLower[i-3]
			u0, u1, u2, u3 := backwardUpper[i], backwardUpper[i-1], backwardUpper[i-2], backwardUpper[i-3]
			backwardUpper[i] = c*u0 + s*l0
			backwardUpper[i-1] = c*u1 + s*l1
			backwardUpper[i-2] = c*u2 + s*l2
			backwardUpper[i-3] = c*u3 + s*l3
			backwardLower[i] = c*l0 - s*u0
			backwardLower[i-1] = c*l1 - s*u1
			backwardLower[i-2] = c*l2 - s*u2
			backwardLower[i-3] = c*l3 - s*u3
		}
		for ; i >= 0; i-- {
			x1 := backwardLower[i]
			x2 := backwardUpper[i]
			backwardUpper[i] = c*x2 + s*x1
			backwardLower[i] = c*x1 - s*x2
		}
	} else {
		for i := backwardLength - 1; i >= 0; i-- {
			x1 := backwardLower[i]
			x2 := backwardUpper[i]
			backwardUpper[i] = c*x2 + s*x1
			backwardLower[i] = c*x1 - s*x2
		}
	}
}
