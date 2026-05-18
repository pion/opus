// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
)

// forwardComplexDFT inverts inverseComplexDFT using the standard
// conjugate-sign change and 1/N scaling factor.
func forwardComplexDFT(in []complex32) []complex32 {
	n := len(in)
	out := make([]complex32, n)
	if n == 0 {
		return out
	}

	scale := float32(1) / float32(n)
	for k := range n {
		sumR := float32(0)
		sumI := float32(0)
		for m, value := range in {
			angle := 2 * math.Pi * float64(k*m) / float64(n)
			cosine := float32(math.Cos(angle))
			sine := float32(math.Sin(angle))
			sumR += value.r*cosine + value.i*sine
			sumI += value.i*cosine - value.r*sine
		}
		out[k] = complex32{
			r: sumR * scale,
			i: sumI * scale,
		}
	}

	return out
}

// forwardMDCT inverts the current inverseMDCT implementation.
//
// The input shape matches the decoder's IMDCT output: a CELT frame of N MDCT
// bins maps to N+overlap time samples, where overlap is always 120 samples.
// This helper expects exactly that time-domain layout and returns the original
// N MDCT bins.
//
// A later optimization can replace the O(N^2) complex DFT with an FFT without
// changing the surrounding packing/rotation steps.
//
//nolint:cyclop // Mirrors the RFC 6716 Section 4.3.7 IMDCT structure step-for-step.
func forwardMDCT(time []float32) []float32 {
	overlap := shortBlockSampleCount
	if len(time) <= overlap {
		return nil
	}

	frameSampleCount := len(time) - overlap
	if frameSampleCount <= 0 || frameSampleCount%2 != 0 {
		return nil
	}

	n2 := frameSampleCount
	n := 2 * n2
	n4 := n >> 2
	leftPlain := n4 - overlap/2
	sine := float32(2 * math.Pi * 0.125 / float64(n))

	deshuffled := make([]float32, n2)

	for i := 0; i < overlap/2; i++ {
		windowValue := celtWindow(i)
		if windowValue == 0 {
			continue
		}
		// inverseMDCT: out[i] = -celtWindow(i) * deshuffled[overlap/2-1-i]
		deshuffled[overlap/2-1-i] = -time[i] / windowValue
	}

	for i := overlap / 2; i < n4; i++ {
		deshuffled[i] = time[overlap/2+i]
	}

	for i := range leftPlain {
		deshuffled[n4+i] = time[n4+overlap/2+i]
	}

	for i := 0; i < overlap/2; i++ {
		windowValue := celtWindow(i)
		if windowValue == 0 {
			continue
		}
		// inverseMDCT: out[n2+overlap-1-i] = celtWindow(i) * deshuffled[n2-overlap/2+i]
		deshuffled[n2-overlap/2+i] = time[n2+overlap-1-i] / windowValue
	}

	postRotated := make([]float32, n2)
	for i := range n4 {
		postRotated[2*i] = -deshuffled[2*i]
		postRotated[n2-1-2*i] = deshuffled[2*i+1]
	}

	fftOut := make([]complex32, n4)
	for i := range n4 {
		yr, yi := undoMDCTShear(postRotated[2*i], postRotated[2*i+1], sine)
		cosine := float32(math.Cos(2 * math.Pi * float64(i) / float64(n)))
		sineQuarter := float32(math.Cos(2 * math.Pi * float64(n4-i) / float64(n)))

		fftOut[i] = complex32{
			r: yr*cosine + yi*sineQuarter,
			i: yi*cosine - yr*sineQuarter,
		}
	}

	preRotated := forwardComplexDFT(fftOut)
	freq := make([]float32, n2)

	for i, value := range preRotated {
		yr, yi := undoMDCTShear(value.r, value.i, sine)
		cosine := float32(math.Cos(2 * math.Pi * float64(i) / float64(n)))
		sineQuarter := float32(math.Cos(2 * math.Pi * float64(n4-i) / float64(n)))
		xp1 := sineQuarter*yr - cosine*yi
		xp2 := -cosine*yr - sineQuarter*yi
		freq[2*i] = xp1
		freq[n2-1-2*i] = xp2
	}

	return freq
}

func undoMDCTShear(a, b, sine float32) (float32, float32) {
	denominator := 1 + sine*sine

	return (a + b*sine) / denominator, (b - a*sine) / denominator
}
