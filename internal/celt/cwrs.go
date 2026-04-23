// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // CWRS notation follows RFC/reference vector names.
package celt

import "github.com/pion/opus/internal/rangecoding"

// decodePulses implements the RFC 6716 Section 4.3.4.2 CWRS index decode for
// the PVQ pulse vector. The row buffer stores one recurrence row of V(N,K).
func decodePulses(y []int, n, k int, rangeDecoder *rangecoding.Decoder) {
	for i := range n {
		y[i] = 0
	}
	if k <= 0 {
		return
	}

	u := cwrsUrow(n, k)
	total := u[k] + u[k+1]
	index, _ := rangeDecoder.DecodeUniform(total)
	cwrsDecode(y, n, k, index, u)
}

// cwrsUrow initializes the recurrence row needed to count PVQ codewords for a
// vector of n dimensions and up to k pulses.
func cwrsUrow(n, k int) []uint32 {
	row := make([]uint32, k+2)
	if n == 0 {
		row[0] = 1

		return row
	}
	row[0] = 0
	if len(row) > 1 {
		row[1] = 1
	}
	if n == 1 {
		for i := 2; i < len(row); i++ {
			row[i] = 1
		}

		return row
	}
	for pulses := 2; pulses < len(row); pulses++ {
		row[pulses] = uint32((pulses << 1) - 1)
	}
	for rowIndex := 2; rowIndex < n; rowIndex++ {
		cwrsNextRow(row[1:], 1)
	}

	return row
}

// cwrsNextRow advances the V(N,K) recurrence by one dimension.
func cwrsNextRow(u []uint32, value0 uint32) {
	value := value0
	for j := 1; j < len(u); j++ {
		next := u[j] + u[j-1] + value
		u[j-1] = value
		value = next
	}
	u[len(u)-1] = value
}

// cwrsDecode walks the recurrence row to recover signs and pulse magnitudes
// from the uniformly decoded codeword index.
func cwrsDecode(y []int, n, k int, index uint32, u []uint32) {
	for j := range n {
		p := u[k+1]
		negative := index >= p
		if negative {
			index -= p
		}

		yj := k
		p = u[k]
		for p > index {
			k--
			p = u[k]
		}
		index -= p
		yj -= k
		if negative {
			y[j] = -yj
		} else {
			y[j] = yj
		}
		cwrsPreviousRow(u, k+2, 0)
	}
}

// cwrsPreviousRow rewinds the recurrence after one coefficient has been
// decoded, matching the row update used by the reference CWRS decoder.
func cwrsPreviousRow(u []uint32, n int, value0 uint32) {
	value := value0
	for j := 1; j < n; j++ {
		next := u[j] - u[j-1] - value
		u[j-1] = value
		value = next
	}
	u[n-1] = value
}
