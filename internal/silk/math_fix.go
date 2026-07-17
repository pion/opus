// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// Additional fixed-point primitives from the RFC 6716 C macros, used by the
// analysis stages.

// smulbb returns (int16)a * (int16)b.
func smulbb(a, b int32) int32 {
	return int32(int16(a)) * int32(int16(b))
}

// smlabb returns a + (int16)b * (int16)c.
func smlabb(a, b, c int32) int32 {
	return a + int32(int16(b))*int32(int16(c))
}

// smulww returns (a * b) >> 16 at 64-bit width.
func smulww(a, b int32) int32 {
	return int32((int64(a) * int64(b)) >> 16)
}

// sat16 saturates to the int16 range.
func sat16(a int32) int32 {
	switch {
	case a > math.MaxInt16:
		return math.MaxInt16
	case a < math.MinInt16:
		return math.MinInt16
	default:
		return a
	}
}

// addPosSat32 saturates the sum of two non-negative values to int32.
func addPosSat32(a, b int32) int32 {
	if (uint32(a)+uint32(b))&0x80000000 != 0 { //nolint:gosec // G115: bit test on the sum.
		return math.MaxInt32
	}

	return a + b
}

// sqrtApprox approximates the square root (silk_SQRT_APPROX).
func sqrtApprox(x int32) int32 {
	if x <= 0 {
		return 0
	}
	lz, fracQ7 := clzFrac(x)
	y := int32(46214)
	if lz&1 != 0 {
		y = 32768
	}
	y >>= lz >> 1

	return smlawb(y, y, smulbb(213, fracQ7))
}

//nolint:gochecknoglobals // fixed sigmoid lookup tables from sigm_Q15.c.
var (
	sigmLUTSlopeQ10 = [6]int32{237, 153, 73, 30, 12, 7}
	sigmLUTPosQ15   = [6]int32{16384, 23955, 28861, 31213, 32178, 32548}
	sigmLUTNegQ15   = [6]int32{16384, 8812, 3906, 1554, 589, 219}
)

// sigmQ15 approximates the sigmoid in Q15 (silk_sigm_Q15).
func sigmQ15(inQ5 int32) int32 {
	if inQ5 < 0 {
		inQ5 = -inQ5
		if inQ5 >= 6*32 {
			return 0
		}
		ind := inQ5 >> 5

		return sigmLUTNegQ15[ind] - smulbb(sigmLUTSlopeQ10[ind], inQ5&0x1F)
	}
	if inQ5 >= 6*32 {
		return 32767
	}
	ind := inQ5 >> 5

	return sigmLUTPosQ15[ind] + smulbb(sigmLUTSlopeQ10[ind], inQ5&0x1F)
}
