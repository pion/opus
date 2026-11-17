// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

const (
	pulseInverseRowCount    = 23
	pulseInverseBudgetCount = 256
)

type pulseInverseTable struct {
	rowIndex [len(pulseCacheIndex)]int8
	values   [pulseInverseRowCount][pulseInverseBudgetCount]uint8
}

// pulseInverseCosts maps budgets 1..256 straight to a pulse count, with one row
// per distinct cache start. Larger budgets still search.
//
//nolint:gochecknoglobals // Bounded immutable inverse of the static CELT cost tables.
var pulseInverseCosts = buildPulseInverseTable()

//nolint:gosec // G115: row < 23 and pulse index <= 40 in the fixed CELT tables.
func buildPulseInverseTable() pulseInverseTable {
	var table pulseInverseTable
	var starts [pulseInverseRowCount]int16
	rowCount := 0
	for index, start := range pulseCacheIndex {
		if start < 0 {
			table.rowIndex[index] = -1

			continue
		}
		row := 0
		for row < rowCount && starts[row] != start {
			row++
		}
		if row == rowCount {
			starts[row] = start
			cache := pulseCacheBits[start:]
			for budget := 1; budget <= pulseInverseBudgetCount; budget++ {
				table.values[row][budget-1] = uint8(bitsToPulsesFromCache(cache, budget))
			}
			rowCount++
		}
		table.rowIndex[index] = int8(row)
	}

	return table
}
