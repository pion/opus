// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// Fixed-point log helpers needed by the encoder. silk_log2lin() is not here
// because it is spelled out inline wherever gains are dequantized.

// ror32 rotates a 32-bit value right by rot bits; a negative rot rotates left.
func ror32(a32 int32, rot int) int32 {
	x := uint32(a32) //nolint:gosec // G115: bit-level rotate, sign is irrelevant.
	switch {
	case rot == 0:
		return a32
	case rot < 0:
		m := uint(-rot)

		return int32((x << m) | (x >> (32 - m))) //nolint:gosec // G115
	default:
		r := uint(rot)

		return int32((x << (32 - r)) | (x >> r)) //nolint:gosec // G115
	}
}

// clzFrac returns the leading-zero count of in and the 7 bits after the
// leading one.
func clzFrac(in int32) (lz, fracQ7 int32) {
	lz = int32(clz32(in))
	fracQ7 = ror32(in, 24-int(lz)) & 0x7f

	return lz, fracQ7
}

// smlawb returns a + smulwb(b, c).
func smlawb(a, b, c int32) int32 {
	return a + smulwb(b, c)
}

// lin2log approximates 128*log2(inLin) with a piecewise-parabolic fit; it is
// the near-inverse of the inline log2lin used for gains.
func lin2log(inLin int32) int32 {
	lz, fracQ7 := clzFrac(inLin)

	// silk_ADD_LSHIFT32(silk_SMLAWB(frac_Q7, silk_MUL(frac_Q7, 128-frac_Q7), 179), 31-lz, 7)
	return smlawb(fracQ7, fracQ7*(128-fracQ7), 179) + ((31 - lz) << 7)
}
