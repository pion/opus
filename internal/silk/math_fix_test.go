// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
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
