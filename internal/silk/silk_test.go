// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIlog(t *testing.T) {
	assert.Equal(t, 0, ilog(-1))
	assert.Equal(t, 0, ilog(0))
	assert.Equal(t, 1, ilog(1))
	assert.Equal(t, 2, ilog(2))
	assert.Equal(t, 2, ilog(3))
	assert.Equal(t, 3, ilog(4))
	assert.Equal(t, 3, ilog(7))
}

func TestSaturatingAddInt16(t *testing.T) {
	assert.Equal(t, int16(123), saturatingAddInt16(100, 23))
	assert.Equal(t, int16(32767), saturatingAddInt16(32760, 100))
	assert.Equal(t, int16(-32768), saturatingAddInt16(-32760, -100))
}

func TestSaturatingSubInt32(t *testing.T) {
	assert.Equal(t, int32(77), saturatingSubInt32(100, 23))
	assert.Equal(t, int32(2147483647), saturatingSubInt32(2147483640, -100))
	assert.Equal(t, int32(-2147483648), saturatingSubInt32(-2147483640, 100))
}

func TestFixedPointHelpers(t *testing.T) {
	assert.Equal(t, int32(5), absInt32(-5))
	assert.Equal(t, int64(3), rshiftRound64(5, 1))
	assert.Equal(t, int32(3), rshiftRound32(5, 1))
	assert.Equal(t, int32(0), inverse32VarQ(1<<30, 29))
}
