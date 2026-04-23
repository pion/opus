// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCWRSRows(t *testing.T) {
	assert.Equal(t, []uint32{0, 1, 3, 5, 7}, cwrsUrow(2, 3))

	row := []uint32{1, 3, 5, 7}
	cwrsNextRow(row, 1)
	assert.Equal(t, []uint32{1, 5, 13, 25}, row)

	cwrsPreviousRow(row, 4, 1)
	assert.Equal(t, []uint32{1, 3, 5, 7}, row)
}

func TestCWRSDecode(t *testing.T) {
	y := []int{99, 99, 99}
	decodePulses(y, len(y), 0, nil)
	assert.Equal(t, []int{0, 0, 0}, y)

	row := cwrsUrow(3, 2)
	cwrsDecode(y, len(y), 2, 0, row)
	assert.Equal(t, []int{2, 0, 0}, y)

	decoder := rangeDecoderWithCDFSymbol(0, cwrsUrow(3, 2)[2]+cwrsUrow(3, 2)[3])
	decodePulses(y, len(y), 2, &decoder)
	assert.Equal(t, []int{2, 0, 0}, y)
}
