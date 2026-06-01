// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // CWRS notation follows RFC/reference vector names.
package celt

import "github.com/pion/opus/internal/rangecoding"

// The static CELT pulse cache tops out at getPulses(40) == 128.
const cwrsMaxPulseCount = 128

type cwrsRowKey uint32

// decodePulses implements the RFC 6716 Section 4.3.4.2 CWRS index decode for
// the PVQ pulse vector. The row buffer stores one recurrence row of V(N,K).
func decodePulses(
	y []int,
	n,
	k int,
	rangeDecoder *rangecoding.Decoder,
	cwrsRows map[cwrsRowKey][]uint32,
) {
	if k <= 0 {
		clear(y[:n])

		return
	}

	switch n {
	case 2:
		index, _ := rangeDecoder.DecodeUniform(cwrsCodewordCount2(k))
		cwrsDecode2(y, k, index)

		return
	case 3:
		index, _ := rangeDecoder.DecodeUniform(cwrsCodewordCount3(k))
		cwrsDecode3(y, k, index)

		return
	case 4:
		index, _ := rangeDecoder.DecodeUniform(cwrsCodewordCount4(k))
		cwrsDecode4(y, k, index)

		return
	}

	var row [cwrsMaxPulseCount + 2]uint32
	var u []uint32
	if k > cwrsMaxPulseCount {
		u = make([]uint32, k+2)
	} else {
		u = row[:k+2]
	}
	if cwrsRows == nil {
		cwrsUrowInto(u, n)
	} else {
		copy(u, cachedCWRSRow(cwrsRows, n, k))
	}
	total := u[k] + u[k+1]
	index, _ := rangeDecoder.DecodeUniform(total)
	cwrsDecode(y, n, k, index, u)
}

func cachedCWRSRow(cwrsRows map[cwrsRowKey][]uint32, n, k int) []uint32 {
	key := cwrsRowKey(cwrsCodebookUint32(n)<<8 | cwrsCodebookUint32(k))
	row := cwrsRows[key]
	if row == nil {
		row = cwrsUrow(n, k)
		cwrsRows[key] = row
	}

	return row
}

// cwrsUrow initializes the recurrence row needed to count PVQ codewords for a
// vector of n dimensions and up to k pulses.
func cwrsUrow(n, k int) []uint32 {
	row := make([]uint32, k+2)
	cwrsUrowInto(row, n)

	return row
}

func cwrsCodewordCount2(k int) uint32 {
	return 4 * cwrsCodebookUint32(k)
}

func cwrsCodewordCount3(k int) uint32 {
	pulses := cwrsCodebookUint32(k)

	return 2 * (2*pulses*pulses + 1)
}

func cwrsCodewordCount4(k int) uint32 {
	pulses := cwrsCodebookUint32(k)

	return ((pulses*pulses + 2) * pulses / 3) << 3
}

func cwrsDecode1(y []int, k int, index uint32) {
	if len(y) == 0 {
		return
	}
	if index == 0 {
		y[0] = k

		return
	}

	y[0] = -k
}

func cwrsDecode2(y []int, k int, index uint32) {
	p := cwrsU2(k + 1)
	signMask := 0
	if index >= p {
		index -= p
		signMask = -1
	}
	yj := k
	k = int((index + 1) >> 1)
	if k != 0 {
		index -= cwrsU2(k)
	}
	yj -= k
	y[0] = (yj + signMask) ^ signMask
	cwrsDecode1(y[1:], k, index)
}

func cwrsDecode3(y []int, k int, index uint32) {
	p := cwrsU3(k + 1)
	signMask := 0
	if index >= p {
		index -= p
		signMask = -1
	}
	yj := k
	if index != 0 {
		k = int((isqrt32(2*index-1) + 1) >> 1)
	} else {
		k = 0
	}
	if k != 0 {
		index -= cwrsU3(k)
	}
	yj -= k
	y[0] = (yj + signMask) ^ signMask
	cwrsDecode2(y[1:], k, index)
}

func cwrsDecode4(y []int, k int, index uint32) {
	p := cwrsU4(k + 1)
	signMask := 0
	if index >= p {
		index -= p
		signMask = -1
	}
	yj := k
	low := 0
	high := k
	for {
		k = (low + high) >> 1
		if k != 0 {
			p = cwrsU4(k)
		} else {
			p = 0
		}
		switch {
		case p < index:
			if k >= high {
				goto decoded
			}
			low = k + 1
		case p > index:
			high = k - 1
		default:
			goto decoded
		}
	}

decoded:
	index -= p
	yj -= k
	y[0] = (yj + signMask) ^ signMask
	cwrsDecode3(y[1:], k, index)
}

func cwrsU2(k int) uint32 {
	return cwrsCodebookUint32((k << 1) - 1)
}

func cwrsU3(k int) uint32 {
	pulses := cwrsCodebookUint32(k)

	return (2*pulses-2)*pulses + 1
}

func cwrsU4(k int) uint32 {
	pulses := cwrsCodebookUint32(k)

	return (2*pulses*((2*pulses-3)*pulses+4) - 3) / 3
}

// CELT codebook dimensions and pulse counts are small non-negative values.
func cwrsCodebookUint32(value int) uint32 {
	return uint32(value) //nolint:gosec
}

func cwrsUrowInto(row []uint32, n int) {
	if n == 0 {
		row[0] = 1

		return
	}
	row[0] = 0
	if len(row) > 1 {
		row[1] = 1
	}
	if n == 1 {
		for i := 2; i < len(row); i++ {
			row[i] = 1
		}

		return
	}
	for pulses := 2; pulses < len(row); pulses++ {
		row[pulses] = uint32((pulses << 1) - 1)
	}
	for rowIndex := 2; rowIndex < n; rowIndex++ {
		cwrsNextRow(row[1:], 1)
	}
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
	genericLength := n
	if n > 4 {
		genericLength -= 4
	}
	for j := range genericLength {
		p := u[k+1]
		signMask := 0
		if index >= p {
			index -= p
			signMask = -1
		}

		yj := k
		p = u[k]
		for p > index {
			k--
			p = u[k]
		}
		index -= p
		yj -= k
		y[j] = (yj + signMask) ^ signMask
		cwrsPreviousRow(u, k+2, 0)
	}
	if genericLength < n {
		cwrsDecode4(y[genericLength:], k, index)
	}
}

// cwrsPreviousRow rewinds the recurrence after one coefficient has been
// decoded, matching the row update used by the reference CWRS decoder.
func cwrsPreviousRow(u []uint32, n int, value0 uint32) {
	u = u[:n]
	value := value0
	for j := 1; j < len(u); j++ {
		next := u[j] - u[j-1] - value
		u[j-1] = value
		value = next
	}
	u[len(u)-1] = value
}

// encodePulses writes a CWRS index for the PVQ pulse vector y to the range
// encoder. It is the inverse of decodePulses.
func encodePulses(y []int, n, k int, rangeEncoder *rangecoding.Encoder) {
	if k <= 0 {
		return
	}
	index := cwrsEncode(y, n, k)
	u := cwrsUrow(n, k)
	total := u[k] + u[k+1]
	rangeEncoder.EncodeUniform(total, index)
}

// cwrsEncode maps a PVQ pulse vector to its unique CWRS codeword index.
// It is the exact inverse of cwrsDecode for a fixed (n, k) codebook.
func cwrsEncode(y []int, n, k int) uint32 {
	if n <= 0 || k <= 0 {
		return 0
	}

	u := cwrsUrow(n, k)
	var index uint32

	for j := range n {
		magnitude := y[j]
		if magnitude < 0 {
			magnitude = -magnitude
		}
		if magnitude > k {
			return 0
		}

		remaining := k - magnitude
		index += u[remaining]
		if y[j] < 0 {
			index += u[k+1]
		}

		cwrsPreviousRow(u, remaining+2, 0)
		k = remaining
	}

	return index
}
