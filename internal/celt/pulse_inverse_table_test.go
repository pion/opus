// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// pulseInverseReference is the six-step search the table replaces.
//
//nolint:dupl,varnamelen // Keep the independent pre-table algorithm literal.
func pulseInverseReference(band, lm, bits int) int {
	if bits <= 0 {
		return 0
	}
	lm++
	cacheStart := int(pulseCacheIndex[lm*maxBands+band])
	if cacheStart < 0 {
		return 0
	}
	cache := pulseCacheBits[cacheStart:]
	lo := 0
	hi := int(cache[0])
	bits--
	for range 6 {
		mid := (lo + hi + 1) >> 1
		if int(cache[mid]) >= bits {
			hi = mid
		} else {
			lo = mid
		}
	}
	loBits := -1
	if lo != 0 {
		loBits = int(cache[lo])
	}
	if bits-loBits <= int(cache[hi])-bits {
		return lo
	}

	return hi
}

func TestPulseInverseAllFlattenedIndicesAndBudgets(t *testing.T) {
	before := pulseInverseCosts
	beforeIndices, beforeCosts := pulseCacheIndex, pulseCacheBits
	maximum := int(^uint(0) >> 1)
	var comparisons int
	for index := range len(pulseCacheIndex) {
		band, lm := index%maxBands, index/maxBands-1
		for budget := -1024; budget <= 1024; budget++ {
			want := pulseInverseReference(band, lm, budget)
			got := bitsToPulses(band, lm, budget)
			if got != want {
				require.FailNowf(t, "inverse mismatch", "index=%d budget=%d got=%d want=%d", index, budget, got, want)
			}
			comparisons++
		}
		for _, budget := range []int{-maximum - 1, -maximum, -32768, 32767, 32768, 65536, maximum / 2, maximum - 1, maximum} {
			require.Equal(t, pulseInverseReference(band, lm, budget), bitsToPulses(band, lm, budget),
				"index=%d budget=%d", index, budget)
			comparisons++
		}
	}
	require.Equal(t, before, pulseInverseCosts)
	require.Equal(t, beforeIndices, pulseCacheIndex)
	require.Equal(t, beforeCosts, pulseCacheBits)
	t.Logf("%d exact conversions over all %d flattened indices", comparisons, len(pulseCacheIndex))
}

func TestPulseInverseTableInitializationAndTies(t *testing.T) {
	before := pulseInverseCosts
	beforeIndices, beforeCosts := pulseCacheIndex, pulseCacheBits
	rebuilt := buildPulseInverseTable()
	require.Equal(t, before, rebuilt)
	rows := make(map[int16]int)
	var cells, ties int
	for index, start := range pulseCacheIndex {
		row := int(rebuilt.rowIndex[index])
		if start < 0 {
			require.Equal(t, -1, row)

			continue
		}
		require.GreaterOrEqual(t, row, 0)
		require.Less(t, row, pulseInverseRowCount)
		if previous, exists := rows[start]; exists {
			require.Equal(t, previous, row)

			continue
		}
		require.Equal(t, len(rows), row, "deduplicated row order")
		rows[start] = row
		for budget := 1; budget <= pulseInverseBudgetCount; budget++ {
			require.Equal(t, pulseInverseReference(index, -1, budget), int(rebuilt.values[row][budget-1]),
				"start=%d budget=%d", start, budget)
			cells++
		}
		cache := pulseCacheBits[start:]
		for pulse := 0; pulse < int(cache[0]); pulse++ {
			lower := 0
			if pulse > 0 {
				lower = int(cache[pulse]) + 1
			}
			upper := int(cache[pulse+1]) + 1
			if lower < upper && (lower+upper)%2 == 0 {
				budget := (lower + upper) / 2
				require.Equal(t, pulse, bitsToPulses(index, -1, budget),
					"lower endpoint on exact tie start=%d pulse=%d budget=%d", start, pulse, budget)
				ties++
			}
		}
	}
	require.Len(t, rows, pulseInverseRowCount)
	require.Equal(t, 5888, cells)
	require.Positive(t, ties)
	// The first row has equal costs for every nonzero pulse count. Preserve
	// the original search's plateau endpoints, not a generic nearest search.
	require.Equal(t, 0, bitsToPulses(8, -1, 4))
	require.Equal(t, 1, bitsToPulses(8, -1, 5))
	require.Equal(t, 1, bitsToPulses(8, -1, 8))
	require.Equal(t, 40, bitsToPulses(8, -1, 9))
	require.Equal(t, before, pulseInverseCosts)
	require.Equal(t, beforeIndices, pulseCacheIndex)
	require.Equal(t, beforeCosts, pulseCacheBits)
	t.Logf("%d distinct rows, %d inverse cells, %d exact ties; static table bytes=%d", len(rows), cells, ties,
		reflect.TypeFor[pulseInverseTable]().Size())
}

func pulseInverseCapture(converter func(int, int, int) int, band, lm, budget int) (value int, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	value = converter(band, lm, budget)

	return value, false
}

func TestPulseInverseFlattenedAliasesAndPanicPresence(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	minimum := -maximum - 1
	var aliases, invalid, panics int
	for index := range len(pulseCacheIndex) {
		for _, lm := range []int{minimum, -1024, -2, -1, 0, 3, 4, 1024, maximum - 1, maximum} {
			// Runtime signed arithmetic intentionally wraps like the original
			// flattened index, including lm++ at MaxInt. Odd band/lm pairs
			// which land within the table were accepted before this change.
			band := index - (lm+1)*maxBands
			for _, budget := range []int{-1, 0, 1, 127, 128, 255, 256, 257, 1024, maximum - 1, maximum} {
				want, wantPanic := pulseInverseCapture(pulseInverseReference, band, lm, budget)
				got, gotPanic := pulseInverseCapture(bitsToPulses, band, lm, budget)
				require.False(t, wantPanic)
				require.Equal(t, wantPanic, gotPanic, "band=%d lm=%d budget=%d", band, lm, budget)
				require.Equal(t, want, got)
				aliases++
			}
		}
	}
	for _, index := range []int{minimum, -2, -1, len(pulseCacheIndex), len(pulseCacheIndex) + 1, maximum} {
		for _, lm := range []int{-1, minimum, maximum} {
			band := index - (lm+1)*maxBands
			for _, budget := range []int{minimum, -1, 0, 1, 256, 257, maximum} {
				want, wantPanic := pulseInverseCapture(pulseInverseReference, band, lm, budget)
				got, gotPanic := pulseInverseCapture(bitsToPulses, band, lm, budget)
				require.Equal(t, wantPanic, gotPanic, "band=%d lm=%d budget=%d", band, lm, budget)
				require.Equal(t, budget > 0, gotPanic, "early return must precede invalid indexing")
				if !wantPanic {
					require.Equal(t, want, got)
				} else {
					panics++
				}
				invalid++
			}
		}
	}
	t.Logf("%d valid flattened aliases, %d invalid-input cases, %d matching panic-presence cases", aliases, invalid, panics)
}

func TestPulseInverseWarmAllocations(t *testing.T) {
	var total int
	for _, budget := range []int{-1, 0, 1, 128, 255, 256, 257, 1024} {
		allocations := testing.AllocsPerRun(100, func() {
			for index := range len(pulseCacheIndex) {
				total += bitsToPulses(index, -1, budget)
			}
		})
		require.Zero(t, allocations, "budget=%d", budget)
	}
	require.Positive(t, total)
}
