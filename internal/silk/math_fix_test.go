// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiv32VarQ(t *testing.T) {
	cases := []struct {
		a, b int32
		q    int
	}{
		{1000, 7, 16},
		{-500000, 13, 10},
		{123456, 789, 10},
		{1 << 20, 3, 12},
	}
	for _, c := range cases {
		got := float64(div32VarQ(c.a, c.b, c.q))
		want := float64(c.a) * float64(int64(1)<<c.q) / float64(c.b)
		assert.InEpsilonf(t, want, got, 1e-3, "div32VarQ(%d,%d,%d)", c.a, c.b, c.q)
	}
}

func TestSmulwt(t *testing.T) {
	// smulwt(a, b) = (a * (b>>16)) >> 16
	assert.Equal(t, int32(int64(1000000)*int64(0x00030000>>16)>>16), smulwt(1000000, 0x00030000))
}

func TestAddSat32(t *testing.T) {
	assert.Equal(t, int32(3), addSat32(1, 2))
	assert.Equal(t, int32(2147483647), addSat32(2147483647, 100))
	assert.Equal(t, int32(-2147483648), addSat32(-2147483648, -100))
}

func TestSilkRandDeterministic(t *testing.T) {
	// Matches the decoder's LCG update: seed = 196314165*seed + 907633515.
	seed := int32(12345)
	want := int32(uint32(907633515) + uint32(seed)*uint32(196314165)) //nolint:gosec
	assert.Equal(t, want, silkRand(seed))
}

func TestSmulbb(t *testing.T) {
	assert.Equal(t, int32(6), smulbb(2, 3))
	assert.Equal(t, int32(-6), smulbb(-2, 3))
	// Only the low 16 bits of each operand matter (Q-format truncation).
	assert.Equal(t, smulbb(1, 2), smulbb(0x10000+1, 2))
}

func TestSmlabb(t *testing.T) {
	assert.Equal(t, int32(100+6), smlabb(100, 2, 3))
	assert.Equal(t, int32(100-6), smlabb(100, -2, 3))
}

func TestSmulww(t *testing.T) {
	assert.Equal(t, int32((int64(1000000)*int64(1000000))>>16), smulww(1000000, 1000000))
	assert.Equal(t, int32(0), smulww(0, 12345))
}

func TestSmlawt(t *testing.T) {
	got := smlawt(100, 1000000, 0x00030000)
	want := int32(100) + smulwt(1000000, 0x00030000)
	assert.Equal(t, want, got)
}

func TestSub32OvflwAdd32Ovflw(t *testing.T) {
	assert.Equal(t, int32(5), sub32Ovflw(8, 3))
	assert.Equal(t, int32(5), add32Ovflw(2, 3))
	// Two's-complement wrap at the int32 boundary, unlike a saturating add.
	assert.Equal(t, int32(math.MinInt32), add32Ovflw(math.MaxInt32, 1))
	assert.Equal(t, int32(math.MaxInt32), sub32Ovflw(math.MinInt32, 1))
}

func TestAddLShift32(t *testing.T) {
	assert.Equal(t, int32(5+(3<<2)), addLShift32(5, 3, 2))
	// A zero shift still adds b unshifted, it doesn't zero it out.
	assert.Equal(t, int32(8), addLShift32(5, 3, 0))
}

func TestLshiftSat32(t *testing.T) {
	assert.Equal(t, int32(4), lshiftSat32(1, 2))
	// Saturates instead of wrapping when the shift overflows int32.
	assert.Equal(t, int32(math.MaxInt32), lshiftSat32(math.MaxInt32, 4))
	assert.Equal(t, int32(math.MinInt32), lshiftSat32(math.MinInt32, 4))
}

func TestLshiftOvflw(t *testing.T) {
	assert.Equal(t, int32(4), lshiftOvflw(1, 2))
	// Wraps instead of saturating (the counterpart to lshiftSat32).
	maxAsUint32 := uint32(math.MaxInt32)
	assert.Equal(t, int32(maxAsUint32<<4), lshiftOvflw(math.MaxInt32, 4)) //nolint:gosec
}

func TestClampI64(t *testing.T) {
	assert.Equal(t, int64(5), clampI64(5, 0, 10))
	assert.Equal(t, int64(0), clampI64(-5, 0, 10))
	assert.Equal(t, int64(10), clampI64(15, 0, 10))
}

func TestSmlabbOvflw(t *testing.T) {
	assert.Equal(t, int32(100+6), smlabbOvflw(100, 2, 3))
	// Wraps on overflow instead of saturating, unlike a plain saturating add.
	assert.Equal(t, add32Ovflw(math.MaxInt32, 6), smlabbOvflw(math.MaxInt32, 2, 3))
}

func TestSat16(t *testing.T) {
	assert.Equal(t, int32(100), sat16(100))
	assert.Equal(t, int32(math.MaxInt16), sat16(math.MaxInt16+1))
	assert.Equal(t, int32(math.MinInt16), sat16(math.MinInt16-1))
}

func TestAddPosSat32(t *testing.T) {
	assert.Equal(t, int32(30), addPosSat32(10, 20))
	assert.Equal(t, int32(math.MaxInt32), addPosSat32(math.MaxInt32, 100))
}

func TestSqrtApprox(t *testing.T) {
	assert.Zero(t, sqrtApprox(0))
	assert.Zero(t, sqrtApprox(-5))

	// silk_SQRT_APPROX's own doc comment (Inlines.h) claims <10% error for
	// *output* values above 15 and <2.5% for output values above 120 — the
	// bound is on sqrt(x), not on x itself, so the inputs below are chosen
	// so each output value actually falls in the tier it's meant to check.
	cases := []int32{400, 20000, 1 << 20, math.MaxInt32} // sqrt: 20, ~141, 1024, ~46341
	for _, x := range cases {
		got := float64(sqrtApprox(x))
		want := math.Sqrt(float64(x))
		tolerance := 0.10
		if want > 120 {
			tolerance = 0.025
		}
		assert.InEpsilonf(t, want, got, tolerance, "sqrtApprox(%d)", x)
	}
}

func TestRor32(t *testing.T) {
	assert.Equal(t, int32(1), ror32(1, 0))
	// Rotating a single set bit by 1 moves it to the top bit.
	assert.Equal(t, int32(math.MinInt32), ror32(1, 1))
	// A negative rot rotates the other way: right-by-r then right-by-(-r)
	// (i.e. left-by-r) is a round trip back to the original value.
	assert.Equal(t, int32(12345), ror32(ror32(12345, 7), -7))
}

func TestClzFrac(t *testing.T) {
	lz, frac := clzFrac(1)
	assert.Equal(t, int32(31), lz)
	assert.Equal(t, int32(0), frac)

	// A value with the leading bit set (after normalization) has lz=0.
	lz, _ = clzFrac(math.MinInt32 + 1) // 0x80000001, top bit set
	assert.Equal(t, int32(0), lz)
}

func TestSmlawb(t *testing.T) {
	got := smlawb(100, 1000000, 0x00030000)
	want := int32(100) + smulwb(1000000, 0x00030000)
	assert.Equal(t, want, got)
}

func TestLin2Log(t *testing.T) {
	// lin2log approximates 128*log2(x); libopus doesn't document a formal
	// error bound for this one, but the fit is tight enough in practice to
	// hold to 1% for representative inputs.
	for _, x := range []int32{1, 2, 100, 1 << 10, 1 << 20, math.MaxInt32} {
		got := float64(lin2log(x)) / 128.0
		want := math.Log2(float64(x))
		assert.InDeltaf(t, want, got, 0.05, "lin2log(%d)", x)
	}
}

func TestSilkLog2(t *testing.T) {
	for _, x := range []float64{1, 2, 10, 1000} {
		want := float32(3.32192809488736 * math.Log10(x))
		assert.Equal(t, want, silkLog2(x))
	}
}

func TestSigmQ15(t *testing.T) {
	// Midpoint: sigma(0) = 0.5, i.e. 16384 in Q15.
	assert.Equal(t, int32(16384), sigmQ15(0))
	// Clips at the LUT boundary (in_Q5 = 6*32 = 192).
	assert.Equal(t, int32(32767), sigmQ15(192))
	assert.Equal(t, int32(0), sigmQ15(-192))
	// One step inside the boundary, from the last LUT segment.
	assert.Equal(t, 219-smulbb(7, 31), sigmQ15(-191))
	assert.Equal(t, 32548+smulbb(7, 31), sigmQ15(191))
	// Interior point on the second LUT segment (positive and negative side
	// use separate, independently-fitted LUTs, so these aren't exact
	// mirrors of each other — check each directly).
	assert.Equal(t, 23955+smulbb(153, 18), sigmQ15(50))
	assert.Equal(t, 8812-smulbb(153, 18), sigmQ15(-50))
}
