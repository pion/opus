// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"math/bits"
)

// fftPlan caches the twiddle factors for one FFT length so the trigonometry
// is computed once per size instead of inside the per-frame butterfly loops.
type fftPlan struct {
	n              int
	pow2           int
	odd            int
	outerTwiddles  []complex32
	stageTwiddles  [][]complex32
	directTwiddles []complex32
}

// fftPlans holds the plans for every FFT length the encoder uses in
// production: the forward MDCT transforms n4 = frameSampleCount/2 bins per
// CELT frame size. Mirrors inverseTransformPlans on the decoder side.
var fftPlans = [maxLM + 1]*fftPlan{ //nolint:gochecknoglobals
	newFFTPlan(shortBlockSampleCount >> 1),
	newFFTPlan(shortBlockSampleCount),
	newFFTPlan(shortBlockSampleCount << 1),
	newFFTPlan(shortBlockSampleCount << 2),
}

func fftPlanForLength(n int) *fftPlan {
	for _, plan := range fftPlans {
		if plan.n == n {
			return plan
		}
	}

	return newFFTPlan(n)
}

func newFFTPlan(n int) *fftPlan {
	pow2 := 1
	for n%(pow2*2) == 0 {
		pow2 *= 2
	}
	odd := n / pow2

	plan := &fftPlan{
		n:             n,
		pow2:          pow2,
		odd:           odd,
		outerTwiddles: make([]complex32, n),
	}

	for i := range n {
		angle := -2 * math.Pi * float64(i) / float64(n)
		plan.outerTwiddles[i] = complex32{r: float32(math.Cos(angle)), i: float32(math.Sin(angle))}
	}

	stageCount := bits.Len(uint(pow2)) - 1
	plan.stageTwiddles = make([][]complex32, stageCount)
	for stage := range stageCount {
		length := 2 << stage
		halfLen := length >> 1
		twiddles := make([]complex32, halfLen)
		for j := range halfLen {
			angle := -2 * math.Pi * float64(j) / float64(length)
			twiddles[j] = complex32{r: float32(math.Cos(angle)), i: float32(math.Sin(angle))}
		}
		plan.stageTwiddles[stage] = twiddles
	}

	plan.directTwiddles = make([]complex32, odd*odd)
	for k := range odd {
		for m := range odd {
			angle := -2 * math.Pi * float64(k*m) / float64(odd)
			plan.directTwiddles[k*odd+m] = complex32{r: float32(math.Cos(angle)), i: float32(math.Sin(angle))}
		}
	}

	return plan
}

//nolint:cyclop // Cooley-Tukey mixed-radix FFT: three passes + un-permute.
func fftWithScratch(in []complex32, scratch *[]complex32) {
	n := len(in)
	if n <= 1 {
		return
	}

	plan := fftPlanForLength(n)

	if plan.odd == 1 {
		fftRadix2(in, plan)

		return
	}

	if cap(*scratch) < n+plan.odd {
		*scratch = make([]complex32, n+plan.odd)
	}
	scratchBuf := (*scratch)[:n+plan.odd]

	for col := range plan.pow2 {
		colBuf := scratchBuf[col*plan.odd : (col+1)*plan.odd]
		for row := range plan.odd {
			colBuf[row] = in[row*plan.pow2+col]
		}
		directDFT(colBuf, scratchBuf[n:], plan.directTwiddles, plan.odd)
		for row := range plan.odd {
			in[row*plan.pow2+col] = scratchBuf[n+row]
		}
	}

	for row := range plan.odd {
		for col := range plan.pow2 {
			twiddle := plan.outerTwiddles[row*col]

			idx := row*plan.pow2 + col
			r := in[idx].r*twiddle.r - in[idx].i*twiddle.i
			i := in[idx].r*twiddle.i + in[idx].i*twiddle.r
			in[idx] = complex32{r: r, i: i}
		}
	}

	for row := range plan.odd {
		fftRadix2(in[row*plan.pow2:(row+1)*plan.pow2], plan)
	}

	for k1 := range plan.odd {
		for k2 := range plan.pow2 {
			p := k1*plan.pow2 + k2
			q := k1 + k2*plan.odd
			scratchBuf[q] = in[p]
		}
	}
	copy(in, scratchBuf[:n])
}

//nolint:cyclop // Cooley-Tukey mixed-radix FFT: three passes + un-permute.
func fft(in []complex32) {
	var scratch []complex32
	fftWithScratch(in, &scratch)
}

func fftRadix2(in []complex32, plan *fftPlan) {
	n := len(in)
	if n <= 1 {
		return
	}

	for i := range n {
		j := bitReverse(i, n)
		if i < j {
			in[i], in[j] = in[j], in[i]
		}
	}

	// Stage 0 always multiplies by the identity twiddle {1, 0}.
	for i := 0; i < n; i += 2 {
		pair := in[i : i+2]
		ar := pair[0].r
		ai := pair[0].i
		br := pair[1].r
		bi := pair[1].i
		pair[0] = complex32{r: ar + br, i: ai + bi}
		pair[1] = complex32{r: ar - br, i: ai - bi}
	}

	for stage, length := 1, 4; length <= n; stage, length = stage+1, length<<1 {
		halfLen := length >> 1
		twiddles := plan.stageTwiddles[stage]
		for i := 0; i < n; i += length {
			for j := range halfLen {
				twiddle := twiddles[j]

				ar := in[i+j].r
				ai := in[i+j].i
				br := in[i+j+halfLen].r*twiddle.r - in[i+j+halfLen].i*twiddle.i
				bi := in[i+j+halfLen].r*twiddle.i + in[i+j+halfLen].i*twiddle.r

				in[i+j] = complex32{r: ar + br, i: ai + bi}
				in[i+j+halfLen] = complex32{r: ar - br, i: ai - bi}
			}
		}
	}
}

func bitReverse(x int, n int) int {
	rev := 0
	for bits := n; bits > 1; bits >>= 1 {
		rev = (rev << 1) | (x & 1)
		x >>= 1
	}

	return rev
}

// directDFT computes the odd factor of the mixed-radix FFT. CELT uses an odd
// factor of 15, which can be decomposed into smaller radix-5 and radix-3 DFTs.
func directDFT(in, out, twiddles []complex32, n int) {
	if n == 15 {
		dft15(in, out, twiddles)

		return
	}

	for k := range n {
		var sumR, sumI float32
		rowOffset := k * n
		for m, val := range in {
			twiddle := twiddles[rowOffset+m]
			sumR += val.r*twiddle.r - val.i*twiddle.i
			sumI += val.r*twiddle.i + val.i*twiddle.r
		}
		out[k] = complex32{r: sumR, i: sumI}
	}
}

// dft15 computes a 15-point DFT as three radix-5 transforms followed by five
// radix-3 transforms. The intermediate twiddles preserve the usual DFT order.
func dft15(in, out, twiddles []complex32) {
	var work [15]complex32

	for row := range 3 {
		dft5(
			in[row],
			in[row+3],
			in[row+6],
			in[row+9],
			in[row+12],
			work[row*5:row*5+5],
		)
	}

	for column := range 5 {
		a := work[column]
		b := mulComplex(work[5+column], twiddles[column*15+1])
		c := mulComplex(work[10+column], twiddles[column*15+2])

		var values [3]complex32
		dft3(a, b, c, values[:])

		out[column] = values[0]
		out[5+column] = values[1]
		out[10+column] = values[2]
	}
}

func dft3(a, b, c complex32, out []complex32) {
	sum := addComplex(b, c)
	diff := subComplex(b, c)
	base := subComplex(a, scaleComplex(sum, 0.5))
	imaginary := complex32{r: diff.i * 0.8660254, i: -diff.r * 0.8660254}

	out[0] = addComplex(a, sum)
	out[1] = addComplex(base, imaginary)
	out[2] = subComplex(base, imaginary)
}

//nolint:varnamelen // Butterfly notation follows the five DFT inputs.
func dft5(a, b, c, d, e complex32, out []complex32) {
	sum14 := addComplex(b, e)
	sum23 := addComplex(c, d)
	diff14 := subComplex(b, e)
	diff23 := subComplex(c, d)

	out[0] = addComplex(a, addComplex(sum14, sum23))

	base1 := addComplex(
		a,
		addComplex(
			scaleComplex(sum14, 0.30901699),
			scaleComplex(sum23, -0.80901699),
		),
	)

	imaginary1 := addComplex(
		scaleComplex(complex32{r: diff14.i, i: -diff14.r}, 0.95105654),
		scaleComplex(complex32{r: diff23.i, i: -diff23.r}, 0.58778524),
	)
	out[1] = addComplex(base1, imaginary1)
	out[4] = subComplex(base1, imaginary1)

	base2 := addComplex(
		a,
		addComplex(
			scaleComplex(sum14, -0.80901699),
			scaleComplex(sum23, 0.30901699),
		),
	)
	imaginary2 := subComplex(
		scaleComplex(complex32{r: diff14.i, i: -diff14.r}, 0.58778524),
		scaleComplex(complex32{r: diff23.i, i: -diff23.r}, 0.95105654),
	)
	out[2] = addComplex(base2, imaginary2)
	out[3] = subComplex(base2, imaginary2)
}

func addComplex(a, b complex32) complex32 {
	return complex32{r: a.r + b.r, i: a.i + b.i}
}

func subComplex(a, b complex32) complex32 {
	return complex32{r: a.r - b.r, i: a.i - b.i}
}

func scaleComplex(value complex32, scale float32) complex32 {
	return complex32{r: value.r * scale, i: value.i * scale}
}

func mulComplex(a, b complex32) complex32 {
	return complex32{
		r: a.r*b.r - a.i*b.i,
		i: a.r*b.i + a.i*b.r,
	}
}
