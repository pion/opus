// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"fmt"
	"math/bits"
	"math/rand/v2"
	"slices"
	"testing"
	"unsafe"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCWRSTableCountsMatchRecurrence(t *testing.T) {
	if got := unsafe.Sizeof(cwrsTableRows); got != 13376 {
		assert.FailNowf(t, "CWRS mismatch", "table size = %d", got)
	}
	for dimension := 1; dimension <= cwrsTableMaxIndex; dimension++ {
		row := cwrsUrow(dimension, cwrsMaxPulseCount)
		for pulses := 0; pulses <= cwrsMaxPulseCount+1; pulses++ {
			if min(dimension, pulses) >= cwrsTableRowCount {
				continue
			}
			if got := cwrsTableCount(dimension, pulses); got != row[pulses] {
				assert.FailNowf(t, "CWRS mismatch", "U(%d,%d) = %d, want %d", dimension,
					pulses, got, row[pulses])
			}
		}
	}
}

func TestCWRSTableEncodeMatchesGeneric(t *testing.T) {
	random := rand.New(rand.NewPCG(0x43575253, 0x726f756e6432)) //nolint:gosec // Deterministic test corpus, not cryptography.
	for dimension := 1; dimension <= cwrsTableMaxIndex; dimension++ {
		for pulses := 1; pulses <= cwrsMaxPulseCount; pulses++ {
			if !cwrsTableSupports(dimension, pulses) {
				continue
			}
			row := cwrsUrow(dimension, pulses)
			work := make([]uint32, len(row))
			vector := make([]int, dimension)
			for trial := range 8 {
				clear(vector)
				for range pulses {
					coefficient := random.IntN(dimension)
					// Never cancel pulses already placed at a coefficient.
					if vector[coefficient] < 0 || vector[coefficient] == 0 && random.IntN(2) == 0 {
						vector[coefficient]--
					} else {
						vector[coefficient]++
					}
				}
				copy(work, row)
				want := cwrsEncodeRow(vector, dimension, pulses, work)
				if got := cwrsEncodeTable(vector, dimension, pulses); got != want {
					assert.FailNowf(t, "CWRS mismatch", "n=%d k=%d trial=%d: index = %d, want %d", dimension, pulses, trial, got, want)
				}
			}
		}
	}
}

func TestCWRSTableEncodePreservesInvalidVectorChecks(t *testing.T) {
	for _, vector := range [][]int{
		{3, 0, 0},
		{1, 2, 0},
		{0, 0, -3},
		{0, 1, 0},
		{-1, 0, 0},
		{0, 0, 0},
		// Consuming all pulses must still reject a nonzero coefficient in the tail.
		{2, 1, 0},
		{-2, 0, -1},
	} {
		const pulses = 2
		want := cwrsEncodeRow(vector, len(vector), pulses, cwrsUrow(len(vector), pulses))
		if got := cwrsEncodeTable(vector, len(vector), pulses); got != want {
			assert.FailNowf(t, "CWRS mismatch", "vector=%v: index = %d, want %d", vector, got, want)
		}
	}
}

func TestCWRSTableDispatchAndFallback(t *testing.T) {
	for _, params := range [][2]int{{0, 1}, {1, 0}, {209, 1}, {16, 15}, {2, 129}, {208, 128}} {
		if cwrsTableSupports(params[0], params[1]) {
			assert.FailNowf(t, "CWRS mismatch", "unexpected table domain n=%d k=%d", params[0], params[1])
		}
	}
	cache := cwrsEncodeScratch{work: make([]uint32, cwrsMaxPulseCount+2)}
	for _, params := range [][2]int{{1, 1}, {2, 128}, {65, 1}, {74, 4}, {208, 1}, {209, 1}, {2, 129}} {
		dimension, pulses := params[0], params[1]
		vector := make([]int, dimension)
		vector[dimension-1] = -pulses
		row := cwrsUrow(dimension, pulses)
		index := cwrsEncodeRow(vector, dimension, pulses, append([]uint32(nil), row...))
		var got, want rangecoding.Encoder
		got.Init()
		want.Init()
		encodePulses(vector, dimension, pulses, &got, &cache)
		want.EncodeUniform(row[pulses]+row[pulses+1], index)
		if string(got.Done()) != string(want.Done()) {
			assert.FailNowf(t, "CWRS mismatch", "packet changed: n=%d k=%d", dimension, pulses)
		}
	}
}

func BenchmarkCWRSTableEncode(b *testing.B) {
	for _, params := range [][2]int{{8, 4}, {16, 8}, {24, 4}, {48, 2}, {74, 1}, {4, 64}} {
		dimension, pulses := params[0], params[1]
		vector := make([]int, dimension)
		for pulse := range pulses {
			vector[(pulse*7+3)%dimension]++
		}
		row := cwrsUrow(dimension, pulses)
		work := make([]uint32, len(row))
		b.Run(fmt.Sprintf("n%d_k%d/generic", dimension, pulses), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				copy(work, row)
				cwrsEncodeRow(vector, dimension, pulses, work)
			}
		})
		b.Run(fmt.Sprintf("n%d_k%d/table", dimension, pulses), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				cwrsEncodeTable(vector, dimension, pulses)
			}
		})
	}
}

// The bound is checked against the generic row: U(N,K)+U(N,K+1) fits in 32
// bits for K <= maxK and wraps (or is saturated) beyond.
func TestCWRSTableDecodeMaxK(t *testing.T) {
	for n := 2; n <= cwrsTableMaxIndex; n++ {
		maxK := int(cwrsTableDecodeMaxK[n])
		require.Positive(t, maxK, "n=%d", n)
		row := cwrsUrow(n, cwrsMaxPulseCount+1)
		for k := 1; k <= maxK; k++ {
			total := uint64(row[k]) + uint64(row[k+1])
			require.Less(t, total, uint64(1)<<32, "n=%d k=%d", n, k)
			require.Less(t, row[k], row[k+1], "n=%d k=%d rows must grow without wrapping", n, k)
		}
	}
}

func cwrsDecodeTableIndices(total uint32, row []uint32, k int, seed *uint32) []uint32 {
	if total <= 1<<12 {
		indices := make([]uint32, total)
		for i := range indices {
			indices[i] = uint32(i) //nolint:gosec // Bounded by total.
		}

		return indices
	}
	indices := []uint32{0, 1, total - 2, total - 1}
	for j := 0; j <= k+1; j++ {
		for _, delta := range []uint32{0, 1, ^uint32(0)} {
			if value := row[j] + delta; value < total {
				indices = append(indices, value)
			}
		}
	}
	for range 256 {
		*seed = *seed*1664525 + 1013904223
		indices = append(indices, uint32(uint64(*seed)*uint64(total)>>32)) //nolint:gosec // Below total.
	}

	return indices
}

// cwrsDecodeTable must return exactly the generic decoder's vector for every
// codebook the table path accepts.
func TestCWRSDecodeTableMatchesGeneric(t *testing.T) {
	seed := uint32(0xc0de58)
	want := make([]int, cwrsTableMaxIndex)
	got := make([]int, cwrsTableMaxIndex)
	for n := 3; n <= cwrsTableMaxIndex; n++ {
		for k := 1; k <= int(cwrsTableDecodeMaxK[n]); k++ {
			if !cwrsTableSupports(n, k) {
				continue
			}
			row := cwrsUrow(n, k)
			total := row[k] + row[k+1]
			require.Equal(t, total, cwrsTableCount(n, k)+cwrsTableCount(n, k+1), "n=%d k=%d total", n, k)
			for _, index := range cwrsDecodeTableIndices(total, row, k, &seed) {
				u := append([]uint32(nil), row...)
				cwrsDecode(want, n, k, index, u)
				cwrsDecodeTable(got, n, k, index)
				if !slicesEqualInt(want[:n], got[:n]) {
					require.Equal(t, want[:n], got[:n], "n=%d k=%d index=%d", n, k, index)
				}
			}
		}
	}
}

func slicesEqualInt(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// cwrsEncodeTableReference is the plain table sum, with branches instead of masks.
//
//nolint:dupl,varnamelen // Independent reference keeps the plain loop and notation.
func cwrsEncodeTableReference(y []int, n, k int) uint32 {
	var index uint32
	for j := range n {
		magnitude := y[j]
		sign := magnitude >> (bits.UintSize - 1)
		magnitude = (magnitude ^ sign) - sign
		if magnitude > k {
			return 0
		}
		remaining := k - magnitude
		index += cwrsTableRows[min(n-j, remaining)][max(n-j, remaining)]
		if y[j] < 0 {
			index += cwrsTableRows[min(n-j, k+1)][max(n-j, k+1)]
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

func TestCWRSTableFullRows(t *testing.T) {
	for dimension := range cwrsTableRowCount {
		row := cwrsUrow(dimension, cwrsTableMaxIndex-1)
		assert.Equal(t, row, cwrsTableRows[dimension][:], "dimension=%d", dimension)
	}
}

func TestCWRSTableReferenceDomain(t *testing.T) {
	random := rand.New(rand.NewPCG(0x43575253523139, 0x526f7741646472)) //nolint:gosec // Deterministic fixtures.
	var domains, vectors int
	for dimension := 1; dimension <= cwrsTableMaxIndex; dimension++ {
		for pulses := 1; pulses <= cwrsMaxPulseCount; pulses++ {
			if !cwrsTableSupports(dimension, pulses) {
				continue
			}
			domains++
			vector := make([]int, dimension+2)
			for trial := range 20 {
				clear(vector)
				cwrsTableFixture(vector[:dimension], pulses, trial, random)
				// Neither the ordinary loop nor the exhausted-pulse tail may
				// consume coefficients beyond the supplied dimension.
				vector[dimension] = pulses + 1
				vector[dimension+1] = -pulses - 1
				original := slices.Clone(vector)
				want := cwrsEncodeTableReference(vector, dimension, pulses)
				got := cwrsEncodeTable(vector, dimension, pulses)
				if got != want || !slices.Equal(vector, original) {
					assert.FailNowf(t, "CWRS reference mismatch", "n=%d k=%d trial=%d got=%d want=%d", dimension, pulses, trial, got, want)
				}
				vectors++
			}
		}
	}
	t.Logf("checked %d supported domains, %d valid/invalid vectors", domains, vectors)
}

//nolint:cyclop // Explicit fixture classes cover validity, sign, and row-orientation boundaries.
func cwrsTableFixture(vector []int, pulses, trial int, random *rand.Rand) {
	last := len(vector) - 1
	switch trial {
	case 0:
		vector[0] = pulses
	case 1:
		vector[0] = -pulses
	case 2:
		vector[last] = pulses
	case 3:
		vector[last] = -pulses
	case 4:
		vector[0] = pulses + 1
	case 5:
		vector[last] = -pulses - 1
	case 6:
		vector[0] = pulses
		vector[last]++
	case 7:
		vector[0] = -pulses
		vector[last]--
	case 8:
		vector[last] = pulses - 1
	case 9:
		vector[last] = 1 - pulses
	case 10:
		// A zero vector keeps the old partial sum.
	case 11:
		for coefficient := range vector {
			vector[coefficient] = random.IntN(2*pulses+3) - pulses - 1
		}
	default:
		for range pulses {
			coefficient := random.IntN(len(vector))
			if vector[coefficient] < 0 || vector[coefficient] == 0 && random.IntN(2) == 0 {
				vector[coefficient]--
			} else {
				vector[coefficient]++
			}
		}
	}
}

func TestCWRSTableExtremeCoefficients(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	for _, params := range [][2]int{{1, 1}, {2, 128}, {15, 128}, {16, 14}, {208, 14}} {
		dimension, pulses := params[0], params[1]
		for _, coefficient := range []int{maximum, -maximum, -maximum - 1} {
			for _, position := range []int{0, dimension / 2, dimension - 1} {
				vector := make([]int, dimension)
				vector[position] = coefficient
				want, wantPanic := cwrsTableCapture(cwrsEncodeTableReference, vector, dimension, pulses)
				got, gotPanic := cwrsTableCapture(cwrsEncodeTable, vector, dimension, pulses)
				assert.Equal(t, wantPanic, gotPanic, "n=%d k=%d value=%d pos=%d", dimension, pulses, coefficient, position)
				assert.Equal(t, want, got, "n=%d k=%d value=%d pos=%d", dimension, pulses, coefficient, position)
			}
		}
	}
}

func TestCWRSTableShortVectors(t *testing.T) {
	for _, params := range [][2]int{{1, 1}, {15, 128}, {16, 14}, {208, 14}} {
		dimension, pulses := params[0], params[1]
		for _, length := range []int{0, dimension / 2, dimension - 1} {
			for _, first := range []int{0, pulses, -pulses, pulses + 1} {
				vector := make([]int, length)
				if length != 0 {
					vector[0] = first
				}
				want, wantPanic := cwrsTableCapture(cwrsEncodeTableReference, vector, dimension, pulses)
				got, gotPanic := cwrsTableCapture(cwrsEncodeTable, vector, dimension, pulses)
				assert.Equal(t, wantPanic, gotPanic, "n=%d k=%d len=%d first=%d", dimension, pulses, length, first)
				assert.Equal(t, want, got, "n=%d k=%d len=%d first=%d", dimension, pulses, length, first)
			}
		}
	}
}

// abs(MinInt) can't be represented and panics; only check that it panics.
func cwrsTableCapture(encode func([]int, int, int) uint32, vector []int, dimension, pulses int) (index uint32, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	index = encode(vector, dimension, pulses)

	return index, false
}

func TestCWRSTableNoAlloc(t *testing.T) {
	for _, params := range [][2]int{{1, 1}, {4, 64}, {8, 4}, {15, 128}, {16, 14}, {24, 4}, {74, 1}, {208, 14}} {
		dimension, pulses := params[0], params[1]
		vector := make([]int, dimension)
		vector[dimension-1] = -pulses
		allocations := testing.AllocsPerRun(100, func() { cwrsEncodeTable(vector, dimension, pulses) })
		assert.Zero(t, allocations, "n=%d k=%d", dimension, pulses)
	}
}

func BenchmarkCWRSTableEncodeSizes(b *testing.B) {
	for _, params := range [][2]int{{8, 4}, {16, 8}, {24, 4}, {48, 2}, {74, 1}, {4, 64}, {15, 128}, {208, 14}} {
		dimension, pulses := params[0], params[1]
		vector := make([]int, dimension)
		for pulse := range pulses {
			vector[(pulse*7+3)%dimension]++
		}
		b.Run(fmt.Sprintf("n%d_k%d/before", dimension, pulses), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				cwrsEncodeTableReference(vector, dimension, pulses)
			}
		})
		b.Run(fmt.Sprintf("n%d_k%d/candidate", dimension, pulses), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				cwrsEncodeTable(vector, dimension, pulses)
			}
		})
	}
}
