// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-FileCopyrightText: 2002-2008 Jean-Marc Valin
// SPDX-FileCopyrightText: 2007-2008 CSIRO
// SPDX-FileCopyrightText: 2007-2009 Xiph.Org Foundation
// SPDX-FileCopyrightText: 2024 Arm Limited
// SPDX-License-Identifier: MIT AND BSD-2-Clause

package celt

import "math"

// decoderCombFilter preserves the grouped tap arithmetic and recursive alias
// behavior of pinned libopus celt.c. The encoder keeps its existing prefilter
// helper because changing its arithmetic is outside this decoder-only work.
//
//nolint:cyclop // Branch order mirrors generic libopus comb_filter_const_c.
func decoderCombFilter(
	destination, source []float32,
	start, period0, period1, count int,
	gain0, gain1 float32,
	tapset0, tapset1 int,
) {
	gains := [3][3]float32{
		{0.3066406250, 0.2170410156, 0.1296386719},
		{0.4638671875, 0.2680664062, 0},
		{0.7998046875, 0.1000976562, 0},
	}
	if gain0 == 0 && gain1 == 0 {
		if len(destination) > 0 && &destination[0] != &source[0] {
			copy(destination[start:start+count], source[start:start+count])
		}

		return
	}
	period0 = max(period0, combFilterMinPeriod)
	period1 = max(period1, combFilterMinPeriod)
	g00 := float32(gain0 * gains[tapset0][0])
	g01 := float32(gain0 * gains[tapset0][1])
	g02 := float32(gain0 * gains[tapset0][2])
	g10 := float32(gain1 * gains[tapset1][0])
	g11 := float32(gain1 * gains[tapset1][1])
	g12 := float32(gain1 * gains[tapset1][2])
	overlap := min(shortBlockSampleCount, count)
	if gain0 == gain1 && period0 == period1 && tapset0 == tapset1 {
		overlap = 0
	}
	x1 := source[start-period1+1]
	x2 := source[start-period1]
	x3 := source[start-period1-1]
	x4 := source[start-period1-2]
	for i := range overlap {
		x0 := source[start+i-period1+2]
		fade := float32(celtWindow120[i] * celtWindow120[i])
		oneMinusFade := float32(1 - fade)
		result := source[start+i]
		result = float32(result + float32(float32(oneMinusFade*g00)*source[start+i-period0]))
		symmetric := float32(source[start+i-period0+1] + source[start+i-period0-1])
		result = float32(result + float32(float32(oneMinusFade*g01)*symmetric))
		symmetric = float32(source[start+i-period0+2] + source[start+i-period0-2])
		result = float32(result + float32(float32(oneMinusFade*g02)*symmetric))
		result = float32(result + float32(float32(fade*g10)*x2))
		symmetric = float32(x1 + x3)
		result = float32(result + float32(float32(fade*g11)*symmetric))
		symmetric = float32(x0 + x4)
		result = float32(result + float32(float32(fade*g12)*symmetric))
		destination[start+i] = maxFloat32(-536870911, minFloat32(536870911, result))
		x4, x3, x2, x1 = x3, x2, x1, x0
	}
	if gain1 == 0 {
		if len(destination) > 0 && &destination[0] != &source[0] {
			copy(destination[start+overlap:start+count], source[start+overlap:start+count])
		}

		return
	}
	for i := overlap; i < count; i++ {
		x0 := source[start+i-period1+2]
		result := source[start+i]
		result = float32(result + float32(g10*x2))
		result = float32(result + float32(g11*float32(x1+x3)))
		result = float32(result + float32(g12*float32(x0+x4)))
		destination[start+i] = maxFloat32(-536870911, minFloat32(536870911, result))
		x4, x3, x2, x1 = x3, x2, x1, x0
	}
}

// Decoder inverse spreading uses the float32 gain and theta of vq.c.
// In particular, reference sine is cos(1-theta), with the subtraction rounded.
func decoderExpRotation(samples []float32, length, stride, pulses, spread int) {
	if 2*pulses >= length || spread == spreadNone {
		return
	}
	factors := [...]int{15, 10, 5}
	gain := float32(length) / float32(length+factors[spread-1]*pulses)
	theta := float32(0.5) * float32(gain*gain)
	cosine := float32(math.Cos((0.5 * math.Pi) * float64(theta)))
	sine := float32(math.Cos((0.5 * math.Pi) * float64(float32(1)-theta)))
	stride2 := 0
	if length >= 8*stride {
		stride2 = 1
		for (stride2*stride2+stride2)*stride+(stride>>2) < length {
			stride2++
		}
	}
	blockLength := length / stride
	for block := range stride {
		segment := samples[block*blockLength : (block+1)*blockLength]
		if stride2 != 0 {
			decoderExpRotation1(segment, blockLength, stride2, sine, cosine)
		}
		decoderExpRotation1(segment, blockLength, 1, cosine, sine)
	}
}

// decoderRotationProducts rounds the four products before the butterfly.
// Go otherwise contracts the multiply-add pairs below to FMADD/FMSUB on ARM64,
// skipping the intermediate rounding performed by pinned generic libopus.
func decoderRotationProducts(cosine, sine, lower, upper float32) (
	cosUpper, sinLower, cosLower, sinUpper float32,
) {
	return decoderRoundedProduct(cosine, upper), decoderRoundedProduct(sine, lower),
		decoderRoundedProduct(cosine, lower), decoderRoundedProduct(sine, upper)
}

// decoderExpRotation1 is decoder-specific because the encoder does not need
// the generic reference's non-contracted floating-point operation order.
func decoderExpRotation1(samples []float32, length, stride int, cosine, sine float32) {
	if length <= stride {
		return
	}
	lower := samples[:length-stride]
	upper := samples[stride:length]
	for i := range lower {
		cosUpper, sinLower, cosLower, sinUpper := decoderRotationProducts(cosine, sine, lower[i], upper[i])
		upper[i] = cosUpper + sinLower
		lower[i] = cosLower - sinUpper
	}

	backwardLength := len(lower) - stride
	for i := backwardLength - 1; i >= 0; i-- {
		cosUpper, sinLower, cosLower, sinUpper := decoderRotationProducts(
			cosine, sine, lower[i], upper[i],
		)
		upper[i] = cosUpper + sinLower
		lower[i] = cosLower - sinUpper
	}
}

func decoderHaarProducts(scale, left, right float32) (float32, float32) {
	return decoderRoundedProduct(scale, left), decoderRoundedProduct(scale, right)
}

func decoderRoundedProduct(left, right float32) float32 {
	return float32(left * right)
}

// decoderHaar1 prevents ARM64 contraction of each scaled input with the
// following butterfly add/subtract. Encoder analysis retains the shared fast
// transform; only reference-compatible decoder reconstruction uses this path.
func decoderHaar1(samples []float32, count, stride int) {
	count >>= 1
	scale := float32(math.Sqrt(0.5))
	for i := range stride {
		for j := range count {
			leftIndex := stride*2*j + i
			rightIndex := stride*(2*j+1) + i
			left, right := decoderHaarProducts(scale, samples[leftIndex], samples[rightIndex])
			samples[leftIndex] = left + right
			samples[rightIndex] = left - right
		}
	}
}

// The reference rounds the reciprocal square root before multiplying by gain.
// gain/sqrt(energy) is algebraically equivalent, but not float32-equivalent.
func decoderNormaliseResidual(pulses []int, out []float32, count, energy int, gain float32) {
	if energy <= 0 {
		clear(out[:count])

		return
	}
	reciprocal := float32(1) / float32(math.Sqrt(float64(float32(energy))))
	scale := reciprocal * gain
	for i := range count {
		out[i] = float32(pulses[i]) * scale
	}
}

// Decoder stereo reconstruction follows the float reference's single-precision
// square root followed by a separately rounded reciprocal. The encoder keeps
// its existing analysis-oriented helper.
//
//nolint:varnamelen // x/y are the CELT normalized mid/side vectors.
func decoderStereoMerge(x, y []float32, mid float32, count int) {
	cross := float32(0)
	sideEnergy := float32(0)
	for i := range count {
		cross = float32(cross + float32(y[i]*x[i]))
		sideEnergy = float32(sideEnergy + float32(y[i]*y[i]))
	}
	cross = float32(mid * cross)
	midEnergy := float32(mid * mid)
	leftEnergy := float32(float32(midEnergy+sideEnergy) - float32(2*cross))
	rightEnergy := float32(float32(midEnergy+sideEnergy) + float32(2*cross))
	if leftEnergy < 6e-4 || rightEnergy < 6e-4 {
		copy(y[:count], x[:count])

		return
	}
	leftRoot := float32(math.Sqrt(float64(leftEnergy)))
	rightRoot := float32(math.Sqrt(float64(rightEnergy)))
	leftScale := float32(1) / leftRoot
	rightScale := float32(1) / rightRoot
	for i := range count {
		left := float32(float32(mid*x[i]) - y[i])
		right := float32(float32(mid*x[i]) + y[i])
		x[i] = float32(leftScale * left)
		y[i] = float32(rightScale * right)
	}
}

// Decoder folding and anti-collapse normalization keep the reference's
// float32 sqrt, reciprocal, and gain multiplication as separate operations.
func decoderRenormaliseVector(samples []float32, count int, gain float32) {
	energy := float32(1e-15)
	for i := range count {
		energy = float32(energy + float32(samples[i]*samples[i]))
	}
	root := float32(math.Sqrt(float64(energy)))
	reciprocal := float32(1) / root
	scale := float32(reciprocal * gain)
	for i := range count {
		samples[i] = float32(scale * samples[i])
	}
}

// decoderExp2 reproduces FLOAT_APPROX celt_exp2 in pinned libopus mathops.h
// (22244de5a79bd1d6d623c32e72bf1954b56235be). Its polynomial is deliberately
// not mathematical exp2: even exact integer powers differ by one float ULP.
// Callers supply finite log amplitudes capped at 32. Explicit rounding of
// every product prevents architecture-dependent fused multiply-add results.
func decoderExp2(x float32) float32 {
	integer := int32(math.Floor(float64(x)))
	if integer < -50 {
		return 0
	}
	frac := x - float32(integer)
	polynomial := float32(1.877576694823801517486572265625e-03)
	polynomial = float32(8.989339694380760192871093750000e-03) + float32(frac*polynomial)
	polynomial = float32(5.582631751894950866699218750000e-02) + float32(frac*polynomial)
	polynomial = float32(2.401536107063293457031250000000e-01) + float32(frac*polynomial)
	polynomial = float32(6.931530833244323730468750000000e-01) + float32(frac*polynomial)
	polynomial = float32(9.999999403953552246093750000000e-01) + float32(frac*polynomial)
	// Reference exponent adjustment intentionally wraps.
	bits := (math.Float32bits(polynomial) + (uint32(integer) << 23)) & 0x7fffffff //nolint:gosec

	return math.Float32frombits(bits)
}
