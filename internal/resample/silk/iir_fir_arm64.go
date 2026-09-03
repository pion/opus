// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build arm64 && go1.27 && !purego

package silkresample

import "math"

// Each phase stores the forward taps followed by the reversed complementary
// taps, so all eight coefficients fit in one vector load. The source ROM is immutable.
var resamplerFracFIR12NEON = func() [12][8]int16 { //nolint:gochecknoglobals
	var coefficients [12][8]int16
	for phase := range coefficients {
		copy(coefficients[phase][:4], resamplerFracFIR12[phase][:])
		for tap := range 4 {
			coefficients[phase][4+tap] = resamplerFracFIR12[11-phase][3-tap]
		}
	}

	return coefficients
}()

// resamplerPrivateIIRFIRInterpolate processes a block per assembly call.
// NEON is part of the arm64 baseline.
func resamplerPrivateIIRFIRInterpolate(out, buf []int16, maxIndexQ16, indexIncrementQ16 int32) int {
	// An empty range needs no samples or buffer pointers.
	if maxIndexQ16 <= 0 {
		return 0
	}
	// The vector kernel requires a positive step.
	if indexIncrementQ16 <= 0 {
		return resamplerPrivateIIRFIRInterpolateGeneric(out, buf, maxIndexQ16, indexIncrementQ16)
	}

	// Positive progression makes the last eight-sample window the furthest
	// read. Check both buffers and int32 overflow, including the increment
	// after the final output, before passing raw pointers to assembly.
	sampleCount := (int64(maxIndexQ16)-1)/int64(indexIncrementQ16) + 1
	lastIndexQ16 := (sampleCount - 1) * int64(indexIncrementQ16)
	if sampleCount > int64(len(out)) || (lastIndexQ16>>16)+8 > int64(len(buf)) ||
		sampleCount*int64(indexIncrementQ16) > math.MaxInt32 {
		return resamplerPrivateIIRFIRInterpolateGeneric(out, buf, maxIndexQ16, indexIncrementQ16)
	}

	resamplerPrivateIIRFIRInterpolateNEON(
		&out[0], &buf[0], &resamplerFracFIR12NEON[0][0], int(sampleCount), uint32(indexIncrementQ16),
	)

	return int(sampleCount)
}

//go:noescape
func resamplerPrivateIIRFIRInterpolateNEON(out, buf, coefficients *int16, sampleCount int, indexIncrementQ16 uint32)
