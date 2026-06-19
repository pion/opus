// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

type forwardMDCTScratch struct {
	deshuffled  [2][]float32
	postRotated [2][]float32
	fftOut      [2][]complex32
	preRotated  [2][]complex32
	freq        [2][]float32
}

// forwardComplexDFT computes the forward complex DFT via fft and 1/N scaling.
func forwardComplexDFT(in []complex32) []complex32 {
	scratch := []complex32(nil)

	return forwardComplexDFTWithScratch(in, &scratch)
}

// forwardComplexDFTWithScratch reuses the supplied scratch buffer for the
// intermediate FFT allocation.
func forwardComplexDFTWithScratch(in []complex32, scratch *[]complex32) []complex32 {
	out := make([]complex32, len(in))
	copy(out, in)
	fftWithScratch(out, scratch)
	n := float32(len(out))
	for i := range out {
		out[i].r /= n
		out[i].i /= n
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
// The complex DFT step is handled by forwardComplexDFT, which delegates to fft.
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
	// The pre/post rotation tables are identical for analysis and synthesis,
	// so the forward MDCT reuses the inverse transform plan.
	plan := inverseTransformPlanForFrameSampleCount(frameSampleCount)

	deshuffled := make([]float32, n2)

	for i := 0; i < overlap/2; i++ {
		windowValue := celtWindow(i)
		// Apply analysis window: multiply (not divide) to mirror the synthesis
		// windowing in inverseMDCT for TDAC.
		deshuffled[overlap/2-1-i] = -time[i] * windowValue
	}

	for i := overlap / 2; i < n4; i++ {
		deshuffled[i] = time[overlap/2+i]
	}

	for i := range leftPlain {
		deshuffled[n4+i] = time[n4+overlap/2+i]
	}

	for i := 0; i < overlap/2; i++ {
		windowValue := celtWindow(i)
		deshuffled[n2-overlap/2+i] = time[n2+overlap-1-i] * windowValue
	}

	postRotated := make([]float32, n2)
	for i := range n4 {
		postRotated[2*i] = -deshuffled[2*i]
		postRotated[n2-1-2*i] = deshuffled[2*i+1]
	}

	fftOut := make([]complex32, n4)
	for i := range n4 {
		yr, yi := undoMDCTShear(postRotated[2*i], postRotated[2*i+1], plan.sine)
		cosine := plan.rotateCos[i]
		sineQuarter := plan.rotateSinQuarter[i]

		fftOut[i] = complex32{
			r: yr*cosine + yi*sineQuarter,
			i: yi*cosine - yr*sineQuarter,
		}
	}

	preRotated := forwardComplexDFT(fftOut)
	freq := make([]float32, n2)

	for i, value := range preRotated {
		yr, yi := undoMDCTShear(value.r, value.i, plan.sine)
		cosine := plan.rotateCos[i]
		sineQuarter := plan.rotateSinQuarter[i]
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

func ensureMDCTScratch(scratch *forwardMDCTScratch, n2 int) {
	for ch := range scratch.deshuffled {
		if cap(scratch.deshuffled[ch]) < n2 {
			scratch.deshuffled[ch] = make([]float32, n2)
			scratch.postRotated[ch] = make([]float32, n2)
			scratch.freq[ch] = make([]float32, n2)
		}
		if cap(scratch.fftOut[ch]) < n2/2 {
			scratch.fftOut[ch] = make([]complex32, n2/2)
			scratch.preRotated[ch] = make([]complex32, n2/2)
		}
		scratch.deshuffled[ch] = scratch.deshuffled[ch][:n2]
		scratch.postRotated[ch] = scratch.postRotated[ch][:n2]
		scratch.freq[ch] = scratch.freq[ch][:n2]
		scratch.fftOut[ch] = scratch.fftOut[ch][:n2/2]
		scratch.preRotated[ch] = scratch.preRotated[ch][:n2/2]
	}
}

//nolint:cyclop // Mirrors the RFC 6716 Section 4.3.7 IMDCT structure step-for-step.
func forwardMDCTWithScratch(
	time []float32, channel int, scratch *forwardMDCTScratch, fftScratch *[]complex32,
) []float32 {
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
	plan := inverseTransformPlanForFrameSampleCount(frameSampleCount)

	ensureMDCTScratch(scratch, n2)
	deshuffled := scratch.deshuffled[channel]
	postRotated := scratch.postRotated[channel]
	fftOut := scratch.fftOut[channel]
	freq := scratch.freq[channel]

	for i := 0; i < overlap/2; i++ {
		windowValue := celtWindow(i)
		deshuffled[overlap/2-1-i] = -time[i] * windowValue
	}

	for i := range leftPlain {
		deshuffled[n4+i] = time[n4+overlap/2+i]
	}

	for i := 0; i < overlap/2; i++ {
		windowValue := celtWindow(i)
		deshuffled[n2-overlap/2+i] = time[n2+overlap-1-i] * windowValue
	}

	for i := range n4 {
		postRotated[2*i] = -deshuffled[2*i]
		postRotated[n2-1-2*i] = deshuffled[2*i+1]
	}

	for i := range n4 {
		yr, yi := undoMDCTShear(postRotated[2*i], postRotated[2*i+1], plan.sine)
		cosine := plan.rotateCos[i]
		sineQuarter := plan.rotateSinQuarter[i]

		fftOut[i] = complex32{
			r: yr*cosine + yi*sineQuarter,
			i: yi*cosine - yr*sineQuarter,
		}
	}

	// Copy fftOut into the preRotated scratch buffer and run the FFT in-place
	// to avoid a per-frame heap allocation.
	preRotated := scratch.preRotated[channel]
	copy(preRotated, fftOut)
	fftWithScratch(preRotated, fftScratch)
	scale := 1 / float32(len(preRotated))
	for i := range preRotated {
		preRotated[i].r *= scale
		preRotated[i].i *= scale
	}

	for i, value := range preRotated {
		yr, yi := undoMDCTShear(value.r, value.i, plan.sine)
		cosine := plan.rotateCos[i]
		sineQuarter := plan.rotateSinQuarter[i]
		xp1 := sineQuarter*yr - cosine*yi
		xp2 := -cosine*yr - sineQuarter*yi
		freq[2*i] = xp1
		freq[n2-1-2*i] = xp2
	}

	return freq
}
