// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"math/rand"
	"strconv"
	"testing"
)

func TestFFTRoundTrip(t *testing.T) {
	sizes := []int{1, 2, 4, 8, 16, 32, 60, 120, 240, 480}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := randomComplex(n, 42)
			work := cloneComplex(input)
			fft(work)
			for i := range work {
				work[i].i = -work[i].i
			}
			fft(work)
			for i := range work {
				work[i].i = -work[i].i
			}
			scale := 1 / float32(n)
			for i := range work {
				work[i].r *= scale
				work[i].i *= scale
			}
			assertComplexSliceClose(t, input, work, 1e-6)
		})
	}
}

func TestFFTMatchesNaive(t *testing.T) {
	sizes := []int{1, 2, 4, 8, 16, 32, 60, 120, 240, 480}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := randomComplex(n, 42)
			expected := naiveComplexDFT(input)
			actual := cloneComplex(input)
			fft(actual)
			tolerance := 1e-6
			if n >= 32 {
				tolerance = 5e-5
			}
			assertComplexSliceClose(t, expected, actual, tolerance)
		})
	}
}

func TestFFTPureOddSizes(t *testing.T) {
	sizes := []int{3, 5, 15}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := randomComplex(n, 7)
			expected := naiveComplexDFT(input)
			actual := cloneComplex(input)
			fft(actual)
			assertComplexSliceClose(t, expected, actual, 1e-6)
		})
	}
}

func TestFFTPureRadix2Sizes(t *testing.T) {
	sizes := []int{2, 4, 8, 16, 32, 64, 128, 256, 512}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := randomComplex(n, 11)
			expected := naiveComplexDFT(input)
			actual := cloneComplex(input)
			fft(actual)
			tolerance := 1e-6
			if n > 16 {
				tolerance = 5e-5
			}
			assertComplexSliceClose(t, expected, actual, tolerance)
		})
	}
}

func TestInverseFFTMatchesNaive(t *testing.T) {
	sizes := []int{1, 2, 4, 8, 16, 32, 60, 120, 240, 480}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := randomComplex(n, 42)
			expected := make([]complex32, n)
			copy(expected, input)
			for i := range expected {
				expected[i].i = -expected[i].i
			}
			expected = naiveComplexDFT(expected)
			for i := range expected {
				expected[i].i = -expected[i].i
			}
			actual := cloneComplex(input)
			for i := range actual {
				actual[i].i = -actual[i].i
			}
			fft(actual)
			for i := range actual {
				actual[i].i = -actual[i].i
			}
			tolerance := 1e-6
			if n >= 32 {
				tolerance = 5e-5
			}
			assertComplexSliceClose(t, expected, actual, tolerance)
		})
	}
}

func TestForwardInverseRoundTrip(t *testing.T) {
	sizes := []int{60, 120, 240, 480}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := randomComplex(n, 42)
			forward := forwardComplexDFT(input)
			recovered := inverseComplexDFT(forward)
			assertComplexSliceClose(t, input, recovered, 1e-6)
		})
	}
}

func TestFFTZero(t *testing.T) {
	sizes := []int{60, 120, 240, 480}
	for _, n := range sizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			input := make([]complex32, n)
			result := forwardComplexDFT(input)
			assertComplexSliceClose(t, input, result, 1e-7)
		})
	}
}

func naiveComplexDFT(in []complex32) []complex32 {
	n := len(in)
	out := make([]complex32, n)
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

	return out
}

func BenchmarkFFT(b *testing.B) {
	for _, n := range []int{120, 240, 480} {
		input := randomComplex(n, 1)
		b.Run("fft_n="+strconv.Itoa(n), func(b *testing.B) {
			for range b.N {
				work := cloneComplex(input)
				fft(work)
			}
		})
		b.Run("naive_n="+strconv.Itoa(n), func(b *testing.B) {
			for range b.N {
				naiveComplexDFT(input)
			}
		})
	}
}

//nolint:gosec // Deterministic test vector generation does not need crypto/rand.
func randomComplex(n int, seed int64) []complex32 {
	rng := rand.New(rand.NewSource(seed))
	out := make([]complex32, n)
	for i := range n {
		out[i] = complex32{
			r: float32(rng.Float64()*2 - 1),
			i: float32(rng.Float64()*2 - 1),
		}
	}

	return out
}

func cloneComplex(in []complex32) []complex32 {
	out := make([]complex32, len(in))
	copy(out, in)

	return out
}
