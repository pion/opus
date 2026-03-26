// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

import "math"

func silkSMULWB(a32, b32 int32) int32 {
	return ((a32 >> 16) * int32(int16(b32))) + // #nosec G115
		int32((int64(int32(uint32(a32)&0xffff))*int64(int32(int16(b32))))>>16) // #nosec G115
}

func silkSMLAWB(a32, b32, c32 int32) int32 {
	return a32 + silkSMULWB(b32, c32)
}

func silkSMULBB(a32, b32 int32) int32 {
	return int32(int16(a32)) * int32(int16(b32)) // #nosec G115
}

func silkSMLABB(a32, b32, c32 int32) int32 {
	return a32 + silkSMULBB(b32, c32)
}

func silkSMULWW(a32, b32 int32) int32 {
	return silkSMULWB(a32, b32) + (a32 * silkRShiftRound(b32, 16))
}

func silkRShiftRound(a32 int32, shift int) int32 {
	if shift == 1 {
		return (a32 >> 1) + (a32 & 1)
	}

	return ((a32 >> (shift - 1)) + 1) >> 1
}

func silkSAT16(a32 int32) int16 {
	if a32 > math.MaxInt16 {
		return math.MaxInt16
	}
	if a32 < math.MinInt16 {
		return math.MinInt16
	}

	return int16(a32)
}
