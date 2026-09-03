// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !arm64 || !go1.27 || purego

package silkresample

// Builds without a selected SIMD kernel use the scalar interpolator.
func resamplerPrivateIIRFIRInterpolate(out, buf []int16, maxIndexQ16, indexIncrementQ16 int32) int {
	return resamplerPrivateIIRFIRInterpolateGeneric(out, buf, maxIndexQ16, indexIncrementQ16)
}
