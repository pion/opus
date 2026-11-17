// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // CWRS notation follows the recurrence dimensions.
package celt

import "math/bits"

// U(N,K) is symmetric, and every CELT codebook that fits in 32 bits has a small
// minimum dimension, so a few rows cover them. Anything else uses the row cache.
const (
	cwrsTableRowCount = 16
	cwrsTableMaxIndex = 208
)

// Built once at init and never written. Overflow wraps like the generic path.
var cwrsTableRows = func() [cwrsTableRowCount][cwrsTableMaxIndex + 1]uint32 { //nolint:gochecknoglobals // Immutable shared codebook table.
	var rows [cwrsTableRowCount][cwrsTableMaxIndex + 1]uint32
	for n := range rows {
		cwrsUrowInto(rows[n][:], n)
	}

	return rows
}()

// cwrsTableDecodeMaxK[n] is the largest K whose codebook fits in 32 bits, which
// libopus's pulse cache guarantees for every band. Larger codebooks wrap, and
// only the generic decoder reproduces that.
var cwrsTableDecodeMaxK = func() [cwrsTableMaxIndex + 1]uint8 { //nolint:gochecknoglobals // Immutable table bound.
	var maxK [cwrsTableMaxIndex + 1]uint8
	const saturated = uint64(1) << 40
	row := make([]uint64, cwrsMaxPulseCount+2) // U(n, 0..K+1), saturating
	for n := 1; n <= cwrsTableMaxIndex; n++ {
		// U(n,k) = U(n-1,k) + U(n,k-1) + U(n-1,k-1), with U(n,0) = 0.
		diagonal := uint64(0)
		if n == 1 {
			diagonal = 1 // U(0,0)
		}
		for k := 1; k < len(row); k++ {
			above := row[k]
			row[k] = min(saturated, above+row[k-1]+diagonal)
			diagonal = above
		}
		for k := 1; k <= cwrsMaxPulseCount && row[k]+row[k+1] < 1<<32; k++ {
			maxK[n] = uint8(k)
		}
	}

	return maxK
}()

func cwrsTableSupports(n, k int) bool {
	return n > 0 && n <= cwrsTableMaxIndex && k > 0 && k <= cwrsMaxPulseCount &&
		min(n, k+1) < cwrsTableRowCount
}

func cwrsTableCount(n, k int) uint32 {
	return cwrsTableRows[min(n, k)][max(n, k)]
}

// cwrsEncodeTable computes the same index as cwrsEncode, looking U(N,K) up
// instead of rewinding a recurrence row per coefficient. Once every pulse is
// placed, the rest of the vector only has to be zero.
func cwrsEncodeTable(y []int, n, k int) uint32 {
	var index uint32
	for j := range n {
		magnitude := y[j]
		sign := magnitude >> (bits.UintSize - 1)
		magnitude = (magnitude ^ sign) - sign
		if magnitude > k {
			return 0
		}
		remaining := k - magnitude
		dimension := n - j
		// Signs are unpredictable, so mask the negative term instead of branching.
		negative := -uint32(uint(sign) >> (bits.UintSize - 1))
		if dimension < cwrsTableRowCount {
			row := &cwrsTableRows[dimension]
			index += row[remaining]
			index += row[k+1] & negative
		} else {
			// k+1 < cwrsTableRowCount here, so the pulse counts pick the rows.
			index += cwrsTableRows[remaining][dimension]
			index += cwrsTableRows[k+1][dimension] & negative
		}
		k = remaining
		if k == 0 {
			for _, coefficient := range y[j+1 : n] {
				if coefficient != 0 {
					return 0
				}
			}

			return index
		}
	}

	return index
}

// cwrsDecodeTable is libopus cwrsi (cwrs.c) over the shared table: the same
// vector as cwrsDecode without building a recurrence row.
//
//nolint:cyclop,gocognit,nestif // Mirrors the two branches of libopus cwrsi inline; helpers cost ~1.5% decode.
func cwrsDecodeTable(y []int, n, k int, index uint32) {
	y = y[:n]
	j := 0
	for ; n > 2; n-- {
		var val int
		if k >= n {
			// Lots of pulses: the row of this dimension holds every count.
			row := &cwrsTableRows[n]
			p := row[k+1]
			sign := 0
			if index >= p {
				index -= p
				sign = -1
			}
			k0 := k
			if q := row[n]; q > index {
				k = n
				for {
					k--
					p = cwrsTableRows[k][n]
					if p <= index {
						break
					}
				}
			} else {
				for p = row[k]; p > index; p = row[k] {
					k--
				}
			}
			index -= p
			val = (k0 - k + sign) ^ sign
		} else {
			// Lots of dimensions: the pulse count selects the row.
			p := cwrsTableRows[k][n]
			q := cwrsTableRows[k+1][n]
			if p <= index && index < q {
				index -= p
			} else {
				sign := 0
				if index >= q {
					index -= q
					sign = -1
				}
				k0 := k
				for {
					k--
					p = cwrsTableRows[k][n]
					if p <= index {
						break
					}
				}
				index -= p
				val = (k0 - k + sign) ^ sign
			}
		}
		y[j] = val
		j++
	}
	// n == 2.
	p := uint32(2*k + 1) //nolint:gosec // k <= cwrsMaxPulseCount.
	sign := 0
	if index >= p {
		index -= p
		sign = -1
	}
	k0 := k
	k = int((index + 1) >> 1)
	if k != 0 {
		index -= uint32(2*k - 1) //nolint:gosec // k <= cwrsMaxPulseCount.
	}
	y[j] = (k0 - k + sign) ^ sign
	// n == 1.
	sign = -int(index)
	y[j+1] = (k + sign) ^ sign
}
