// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-FileCopyrightText: 2003-2004 Mark Borgerding
// SPDX-FileCopyrightText: 2007-2008 CSIRO
// SPDX-FileCopyrightText: 2007-2010 Xiph.Org Foundation
// SPDX-License-Identifier: MIT AND BSD-2-Clause

//nolint:forbidigo,gosec,nlreturn,varnamelen // Static table invariants and fixed FFT indices mirror pinned libopus.
package celt

import "math"

type decoderTransformPlan struct {
	frameSampleCount int
	trig             []float32
	twiddles         []complex32
	bitrev           []int
	factors          []fftFactor
}

var decoderTransformPlans = [maxLM + 1]decoderTransformPlan{ //nolint:gochecknoglobals
	newDecoderTransformPlan(shortBlockSampleCount),
	newDecoderTransformPlan(shortBlockSampleCount << 1),
	newDecoderTransformPlan(shortBlockSampleCount << 2),
	newDecoderTransformPlan(shortBlockSampleCount << 3),
}

func newDecoderTransformPlan(frameSampleCount int) decoderTransformPlan {
	var table *decoderTransformTable
	for i := range decoderTransformTables {
		if decoderTransformTables[i].frameSampleCount == frameSampleCount {
			table = &decoderTransformTables[i]
			break
		}
	}
	if table == nil {
		panic("unsupported CELT decoder transform size")
	}
	factors := make([]fftFactor, len(table.factors)/2)
	for i := range factors {
		factors[i] = fftFactor{radix: int(table.factors[2*i]), size: int(table.factors[2*i+1])}
	}
	plan := decoderTransformPlan{
		frameSampleCount: frameSampleCount,
		trig:             make([]float32, len(table.trig)),
		twiddles:         make([]complex32, len(table.twiddleR)),
		bitrev:           make([]int, len(table.bitrev)),
		factors:          factors,
	}
	for i, bits := range table.trig {
		plan.trig[i] = math.Float32frombits(bits)
	}
	for i := range table.twiddleR {
		plan.twiddles[i] = complex32{
			r: math.Float32frombits(table.twiddleR[i]),
			i: math.Float32frombits(table.twiddleI[i]),
		}
	}
	for i, value := range table.bitrev {
		plan.bitrev[i] = int(value)
	}

	return plan
}

func decoderTransformPlanForFrameSampleCount(frameSampleCount int) *decoderTransformPlan {
	for i := range decoderTransformPlans {
		if decoderTransformPlans[i].frameSampleCount == frameSampleCount {
			return &decoderTransformPlans[i]
		}
	}
	panic("unsupported CELT decoder transform size")
}

func decoderMDCTBackward(
	input []float32,
	inputOffset int,
	inputStride int,
	output []float32,
	plan *decoderTransformPlan,
	scratch *mdctScratch,
) {
	frameSampleCount := plan.frameSampleCount
	complexSampleCount := frameSampleCount / 2
	transform := scratch.fftOut[:complexSampleCount]
	for i := range complexSampleCount {
		x1 := input[inputOffset+2*i*inputStride]
		x2 := input[inputOffset+(frameSampleCount-1-2*i)*inputStride]
		product20 := float32(x2 * plan.trig[i])
		product11 := float32(x1 * plan.trig[complexSampleCount+i])
		yr := float32(product20 + product11)
		product10 := float32(x1 * plan.trig[i])
		product21 := float32(x2 * plan.trig[complexSampleCount+i])
		yi := float32(product10 - product21)
		transform[plan.bitrev[i]] = complex32{r: yi, i: yr}
	}
	decoderFFT(transform, plan)

	deshuffled := scratch.deshuffled[:frameSampleCount]
	for i := 0; i < (complexSampleCount+1)/2; i++ {
		left := transform[i]
		yr := float32(float32(left.i*plan.trig[i]) + float32(left.r*plan.trig[complexSampleCount+i]))
		yi := float32(float32(left.i*plan.trig[complexSampleCount+i]) - float32(left.r*plan.trig[i]))
		deshuffled[2*i] = yr
		deshuffled[frameSampleCount-1-2*i] = yi

		rightIndex := complexSampleCount - i - 1
		right := transform[rightIndex]
		yr = float32(float32(right.i*plan.trig[rightIndex]) +
			float32(right.r*plan.trig[complexSampleCount+rightIndex]))
		yi = float32(float32(right.i*plan.trig[complexSampleCount+rightIndex]) -
			float32(right.r*plan.trig[rightIndex]))
		deshuffled[frameSampleCount-2-2*i] = yr
		deshuffled[2*i+1] = yi
	}

	halfOverlap := shortBlockSampleCount / 2
	for i := range halfOverlap {
		x1 := deshuffled[halfOverlap-1-i]
		x2 := output[i]
		output[i] = float32(float32(x2*celtWindow120[shortBlockSampleCount-1-i]) -
			float32(x1*celtWindow120[i]))
		output[shortBlockSampleCount-1-i] = float32(float32(x2*celtWindow120[i]) +
			float32(x1*celtWindow120[shortBlockSampleCount-1-i]))
	}
	copy(output[shortBlockSampleCount:], deshuffled[halfOverlap:])
}

func decoderFFT(samples []complex32, plan *decoderTransformPlan) {
	fstrides := [6]int{1}
	for i, factor := range plan.factors {
		fstrides[i+1] = fstrides[i] * factor.radix
	}
	m := plan.factors[len(plan.factors)-1].size
	for stage := len(plan.factors) - 1; stage >= 0; stage-- {
		m2 := 1
		if stage != 0 {
			m2 = plan.factors[stage-1].size
		}
		switch plan.factors[stage].radix {
		case 2:
			decoderButterfly2(samples, m, fstrides[stage])
		case 3:
			decoderButterfly3(samples, plan.twiddles, fstrides[stage], m, fstrides[stage], m2)
		case 4:
			decoderButterfly4(samples, plan.twiddles, fstrides[stage], m, fstrides[stage], m2)
		case 5:
			decoderButterfly5(samples, plan.twiddles, fstrides[stage], m, fstrides[stage], m2)
		default:
			panic("unsupported CELT decoder FFT radix")
		}
		m = m2
	}
}

func decoderComplexMultiply(a, b complex32) complex32 {
	real0 := float32(a.r * b.r)
	real1 := float32(a.i * b.i)
	imag0 := float32(a.r * b.i)
	imag1 := float32(a.i * b.r)

	return complex32{r: float32(real0 - real1), i: float32(imag0 + imag1)}
}

func decoderButterfly2(samples []complex32, m, groupCount int) {
	if m != 4 {
		panic("unexpected CELT radix-2 stage")
	}
	twiddle := float32(0.7071067812)
	for group := range groupCount {
		base := group * 8
		for i := range 4 {
			left, right := samples[base+i], samples[base+4+i]
			var rotated complex32
			switch i {
			case 0:
				rotated = right
			case 1:
				rotated = complex32{
					r: float32(float32(right.r+right.i) * twiddle),
					i: float32(float32(right.i-right.r) * twiddle),
				}
			case 2:
				rotated = complex32{r: right.i, i: -right.r}
			case 3:
				rotated = complex32{
					r: float32(float32(right.i-right.r) * twiddle),
					i: float32(-float32(right.i+right.r) * twiddle),
				}
			}
			samples[base+4+i] = subtractComplex32(left, rotated)
			samples[base+i] = addComplex32(left, rotated)
		}
	}
}

func decoderButterfly4(
	samples []complex32,
	twiddles []complex32,
	fstride, m, groupCount, groupStride int,
) {
	if m == 1 {
		for group := range groupCount {
			base := group * 4
			out0, out1 := samples[base], samples[base+1]
			out2, out3 := samples[base+2], samples[base+3]
			difference02 := subtractComplex32(out0, out2)
			out0 = addComplex32(out0, out2)
			sum13 := addComplex32(out1, out3)
			samples[base+2] = subtractComplex32(out0, sum13)
			samples[base] = addComplex32(out0, sum13)
			difference13 := subtractComplex32(out1, out3)
			samples[base+1] = complex32{
				r: float32(difference02.r + difference13.i),
				i: float32(difference02.i - difference13.r),
			}
			samples[base+3] = complex32{
				r: float32(difference02.r - difference13.i),
				i: float32(difference02.i + difference13.r),
			}
		}

		return
	}
	for group := range groupCount {
		base := group * groupStride
		for i := range m {
			index := base + i
			out0 := samples[index]
			out1 := decoderComplexMultiply(samples[index+m], twiddles[i*fstride])
			out2 := decoderComplexMultiply(samples[index+2*m], twiddles[2*i*fstride])
			out3 := decoderComplexMultiply(samples[index+3*m], twiddles[3*i*fstride])
			difference02 := subtractComplex32(out0, out2)
			out0 = addComplex32(out0, out2)
			sum13 := addComplex32(out1, out3)
			samples[index+2*m] = subtractComplex32(out0, sum13)
			samples[index] = addComplex32(out0, sum13)
			difference13 := subtractComplex32(out1, out3)
			samples[index+m] = complex32{
				r: float32(difference02.r + difference13.i),
				i: float32(difference02.i - difference13.r),
			}
			samples[index+3*m] = complex32{
				r: float32(difference02.r - difference13.i),
				i: float32(difference02.i + difference13.r),
			}
		}
	}
}

func decoderButterfly3(
	samples []complex32,
	twiddles []complex32,
	fstride, m, groupCount, groupStride int,
) {
	epi3 := twiddles[fstride*m].i
	for group := range groupCount {
		base := group * groupStride
		for i := range m {
			index := base + i
			out1 := decoderComplexMultiply(samples[index+m], twiddles[i*fstride])
			out2 := decoderComplexMultiply(samples[index+2*m], twiddles[2*i*fstride])
			sum := addComplex32(out1, out2)
			difference := subtractComplex32(out1, out2)
			mid := complex32{
				r: float32(samples[index].r - float32(0.5*sum.r)),
				i: float32(samples[index].i - float32(0.5*sum.i)),
			}
			difference.r = float32(difference.r * epi3)
			difference.i = float32(difference.i * epi3)
			samples[index] = addComplex32(samples[index], sum)
			samples[index+2*m] = complex32{
				r: float32(mid.r + difference.i), i: float32(mid.i - difference.r),
			}
			samples[index+m] = complex32{
				r: float32(mid.r - difference.i), i: float32(mid.i + difference.r),
			}
		}
	}
}

func decoderButterfly5(
	samples []complex32,
	twiddles []complex32,
	fstride, m, groupCount, groupStride int,
) {
	ya, yb := twiddles[fstride*m], twiddles[2*fstride*m]
	for group := range groupCount {
		base := group * groupStride
		for i := range m {
			index := base + i
			out0 := samples[index]
			out1 := decoderComplexMultiply(samples[index+m], twiddles[i*fstride])
			out2 := decoderComplexMultiply(samples[index+2*m], twiddles[2*i*fstride])
			out3 := decoderComplexMultiply(samples[index+3*m], twiddles[3*i*fstride])
			out4 := decoderComplexMultiply(samples[index+4*m], twiddles[4*i*fstride])
			sum14, difference14 := addComplex32(out1, out4), subtractComplex32(out1, out4)
			sum23, difference23 := addComplex32(out2, out3), subtractComplex32(out2, out3)
			samples[index] = addComplex32(out0, addComplex32(sum14, sum23))
			base14 := complex32{
				r: float32(out0.r + float32(float32(sum14.r*ya.r)+float32(sum23.r*yb.r))),
				i: float32(out0.i + float32(float32(sum14.i*ya.r)+float32(sum23.i*yb.r))),
			}
			rotate14 := complex32{
				r: float32(float32(difference14.i*ya.i) + float32(difference23.i*yb.i)),
				i: -float32(float32(difference14.r*ya.i) + float32(difference23.r*yb.i)),
			}
			samples[index+m] = subtractComplex32(base14, rotate14)
			samples[index+4*m] = addComplex32(base14, rotate14)
			base23 := complex32{
				r: float32(out0.r + float32(float32(sum14.r*yb.r)+float32(sum23.r*ya.r))),
				i: float32(out0.i + float32(float32(sum14.i*yb.r)+float32(sum23.i*ya.r))),
			}
			rotate23 := complex32{
				r: float32(float32(difference23.i*ya.i) - float32(difference14.i*yb.i)),
				i: float32(float32(difference14.r*yb.i) - float32(difference23.r*ya.i)),
			}
			samples[index+2*m] = addComplex32(base23, rotate23)
			samples[index+3*m] = subtractComplex32(base23, rotate23)
		}
	}
}
