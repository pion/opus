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

func TestCWRSEncodeZeroPulses(t *testing.T) {
	assert.Equal(t, uint32(0), cwrsEncode([]int{0, 0, 0}, 3, 0))
}

func TestCWRSEncodeRoundTrip(t *testing.T) {
	n := 4
	k := 3
	row := cwrsUrow(n, k)
	total := row[k] + row[k+1]

	for index := range total {
		vector := make([]int, n)
		cwrsDecode(vector, n, k, index, append([]uint32(nil), row...))

		encoded := cwrsEncode(vector, n, k)
		assert.Equal(t, index, encoded)
	}
}

func TestCWRSEncodeDecodeRoundTrip(t *testing.T) {
	vectors := [][]int{
		{2, 0, 0},
		{-2, 0, 0},
		{1, 1, 0},
		{1, -1, 0},
		{0, 1, 1},
		{0, -1, 1},
	}

	for _, expected := range vectors {
		index := cwrsEncode(expected, len(expected), 2)

		got := make([]int, len(expected))
		row := cwrsUrow(3, 2)
		cwrsDecode(got, len(got), 2, index, row)

		assert.Equal(t, expected, got)
	}
}
