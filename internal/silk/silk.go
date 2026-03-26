// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package silk provides a Silk coder
package silk

import (
	"math"
	"math/bits"
)

type (
	// Bandwidth for Silk can be NB (narrowband) MB (medium-band) or WB (wideband).
	Bandwidth byte

	frameSignalType             byte
	frameQuantizationOffsetType byte
)

const (
	maxSubframeCount = 4

	pulsecountLargestPartitionSize = 16

	nanoseconds10Ms = 10000000
	nanoseconds20Ms = 20000000
	nanoseconds40Ms = 40000000
	nanoseconds60Ms = 60000000

	frameSignalTypeInactive frameSignalType = iota + 1
	frameSignalTypeUnvoiced
	frameSignalTypeVoiced

	frameQuantizationOffsetTypeLow frameQuantizationOffsetType = iota + 1
	frameQuantizationOffsetTypeHigh
)

// Bandwidth constants.
const (
	BandwidthNarrowband Bandwidth = iota + 1
	BandwidthMediumband
	BandwidthWideband
)

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}

	return b
}

func maxInt16(a, b int16) int16 {
	if a > b {
		return a
	}

	return b
}

func minUint(a, b uint) uint {
	if a > b {
		return b
	}

	return a
}

func minInt16(a, b int16) int16 {
	if a > b {
		return b
	}

	return a
}

// saturatingAddInt16 mirrors silk_ADD_SAT16() from the RFC 8251 update to
// NLSF_stabilize.c. RFC 8251 section 7 changed this addition to saturate
// instead of wrapping, and this helper keeps that behavior explicit at the
// call site in normalizeLSFStabilization().
func saturatingAddInt16(a, b int16) int16 {
	sum := int32(a) + int32(b)

	return int16(clamp(math.MinInt16, sum, math.MaxInt16))
}

// saturatingSubInt32 mirrors silk_SUB_SAT32() from the RFC 6716 C macros.
// The LPC inverse-gain recurrence uses this for the numerator update in
// silk_LPC_inverse_pred_gain_QA() (LPC_inv_pred_gain.c), where overflow must
// clamp instead of wrap to preserve the reference stability decision.
func saturatingSubInt32(a, b int32) int32 {
	diff := int64(a) - int64(b)
	if diff > math.MaxInt32 {
		return math.MaxInt32
	}
	if diff < math.MinInt32 {
		return math.MinInt32
	}

	return int32(diff)
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}

	return v
}

func clamp(low, in, high int32) int32 {
	if in > high {
		return high
	} else if in < low {
		return low
	}

	return in
}

func clampNegativeOneToOne(v float32) float32 {
	if v <= -1 {
		return -1
	} else if v >= 1 {
		return 1
	}

	return v
}

// The sign of x, i.e.,
//
//	          ( -1,  x < 0
//	sign(x) = <  0,  x == 0
//	          (  1,  x > 0
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-1.1.4
func sign(x int) int {
	switch {
	case x < 0:
		return -1
	case x == 0:
		return 0
	default:
		return 1
	}
}

// The minimum number of bits required to store a positive integer n in
// binary, or 0 for a non-positive integer n.
//
//	          ( 0,                 n <= 0
//	ilog(n) = <
//	          ( floor(log2(n))+1,  n > 0
func ilog(n int) int {
	if n <= 0 {
		return 0
	}

	return int(math.Floor(math.Log2(float64(n)))) + 1
}

func clz32(in int32) int {
	return bits.LeadingZeros32(uint32(in))
}

// rshiftRound64 mirrors silk_RSHIFT_ROUND64() from the RFC 6716 C macros.
// The LPC gain limiter depends on this exact rounding rule when it converts
// 64-bit products back down to fixed-point state.
func rshiftRound64(a int64, shift int) int64 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}

	return ((a >> (shift - 1)) + 1) >> 1
}

// rshiftRound32 mirrors silk_RSHIFT_ROUND() from the RFC 6716 C macros.
// This is used by the bandwidth expander and multiply helpers to match the
// reference fixed-point rounding behavior.
func rshiftRound32(a int32, shift int) int32 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}

	return ((a >> (shift - 1)) + 1) >> 1
}

// smmul mirrors silk_SMMUL() from the RFC 6716 C macros: it returns the high
// 32 bits of a signed 32x32 multiply. RFC 6716 section 4.2.7.5.8 expresses
// several of the LPC inverse-gain updates in terms of this operation.
func smmul(a, b int32) int32 {
	return int32((int64(a) * int64(b)) >> 32)
}

// smulwb mirrors silk_SMULWB() from the RFC 6716 C macros. It multiplies a
// 32-bit value by the low 16 bits of another 32-bit value and is one half of
// the reference implementation's SMULWW/SMLAWW decomposition.
func smulwb(a, b int32) int32 {
	return int32((int64(a) * int64(int16(b))) >> 16)
}

// smulww mirrors silk_SMULWW() from the RFC 6716 C macros. The branch uses
// this in bwexpander32() so the chirp recurrence matches
// silk_bwexpander_32() instead of using a plain 32-bit multiply.
func smulww(a, b int32) int32 {
	return smulwb(a, b) + a*rshiftRound32(b, 16)
}

// smlaWW mirrors silk_SMLAWW() from the RFC 6716 C macros. The inverse helper
// uses it for the Newton-Raphson refinement step in silk_INVERSE32_varQ().
func smlaWW(a, b, c int32) int32 {
	return a + smulwb(b, c) + b*rshiftRound32(c, 16)
}

// inverse32VarQ mirrors silk_INVERSE32_varQ() from the RFC 6716 C reference
// (Inlines.h). The LPC gain limiter uses this to approximate the reciprocal
// of div_Q30 with the same normalization and refinement steps as the
// reference decoder.
func inverse32VarQ(b32 int32, qRes int) int32 {
	bHeadrm := clz32(absInt32(b32)) - 1
	b32Nrm := b32 << bHeadrm
	b32Inv := (math.MaxInt32 >> 2) / (b32Nrm >> 16)
	result := b32Inv << 16
	errQ32 := ((1 << 29) - smulwb(b32Nrm, b32Inv)) << 3
	result = smlaWW(result, errQ32, b32Inv)

	lshift := 61 - bHeadrm - qRes
	if lshift <= 0 {
		shifted := int64(result) << -lshift
		if shifted > math.MaxInt32 {
			return math.MaxInt32
		}
		if shifted < math.MinInt32 {
			return math.MinInt32
		}

		return int32(shifted)
	}
	if lshift < 32 {
		return result >> lshift
	}

	return 0
}

// bwexpander32 mirrors silk_bwexpander_32() from the RFC 6716 C reference
// (bwexpander_32.c). RFC 6716 sections 4.2.7.5.7 and 4.2.7.5.8 both rely on
// this exact chirp recurrence for bandwidth expansion.
func bwexpander32(ar []int32, chirpQ16 int32) {
	chirpMinusOneQ16 := chirpQ16 - 65536
	for i := 0; i < len(ar)-1; i++ {
		ar[i] = smulww(chirpQ16, ar[i])
		chirpQ16 += rshiftRound32(chirpQ16*chirpMinusOneQ16, 16)
	}
	ar[len(ar)-1] = smulww(chirpQ16, ar[len(ar)-1])
}

func subframeCount(nanoseconds int) int {
	switch nanoseconds {
	case nanoseconds10Ms:
		return 2
	case nanoseconds20Ms:
		return 4
	}

	return 0
}

func silkFrameCount(nanoseconds int) int {
	switch nanoseconds {
	case nanoseconds10Ms, nanoseconds20Ms:
		return 1
	case nanoseconds40Ms:
		return 2
	case nanoseconds60Ms:
		return 3
	}

	return 0
}
