// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import "math"

//nolint:cyclop // Cooley-Tukey mixed-radix FFT: three passes + un-permute.
func fft(in []complex32) {
	n := len(in)
	if n <= 1 {
		return
	}

	pow2 := 1
	for n%(pow2*2) == 0 {
		pow2 *= 2
	}
	odd := n / pow2

	if odd == 1 {
		fftRadix2(in)

		return
	}

	scratch := make([]complex32, n+odd)

	for col := range pow2 {
		colBuf := scratch[col*odd : (col+1)*odd]
		for row := range odd {
			colBuf[row] = in[row*pow2+col]
		}
		directDFT(colBuf, scratch[n:])
		for row := range odd {
			in[row*pow2+col] = scratch[n+row]
		}
	}

	for row := range odd {
		for col := range pow2 {
			angle := -2 * math.Pi * float64(row*col) / float64(n)
			wr := float32(math.Cos(angle))
			wi := float32(math.Sin(angle))

			idx := row*pow2 + col
			r := in[idx].r*wr - in[idx].i*wi
			i := in[idx].r*wi + in[idx].i*wr
			in[idx] = complex32{r: r, i: i}
		}
	}

	for row := range odd {
		fftRadix2(in[row*pow2 : (row+1)*pow2])
	}

	for k1 := range odd {
		for k2 := range pow2 {
			p := k1*pow2 + k2
			q := k1 + k2*odd
			scratch[q] = in[p]
		}
	}
	copy(in, scratch[:n])
}

func fftRadix2(in []complex32) {
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

	for length := 2; length <= n; length <<= 1 {
		halfLen := length >> 1
		for i := 0; i < n; i += length {
			for j := range halfLen {
				angle := -2 * math.Pi * float64(j) / float64(length)
				wr := float32(math.Cos(angle))
				wi := float32(math.Sin(angle))

				ar := in[i+j].r
				ai := in[i+j].i
				br := in[i+j+halfLen].r*wr - in[i+j+halfLen].i*wi
				bi := in[i+j+halfLen].r*wi + in[i+j+halfLen].i*wr

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

// directDFT computes the O(N²) DFT directly, used for small odd factors that
// are not handled by the radix-2 path. Inputs and outputs are separate buffers.
func directDFT(in, out []complex32) {
	n := len(in)
	for k := range n {
		var sumR, sumI float32
		for m, val := range in {
			angle := -2 * math.Pi * float64(k*m) / float64(n)
			c := float32(math.Cos(angle))
			s := float32(math.Sin(angle))
			sumR += val.r*c - val.i*s
			sumI += val.r*s + val.i*c
		}
		out[k] = complex32{r: sumR, i: sumI}
	}
}
