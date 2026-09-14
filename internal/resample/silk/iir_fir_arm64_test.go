// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build arm64 && go1.27 && !purego

package silkresample

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

const neonTestBufLen = 512

type neonTestBuffer struct {
	name string
	buf  []int16
}

func neonSampleCount(maxIndexQ16, indexIncrementQ16 int32) int {
	if maxIndexQ16 <= 0 || indexIncrementQ16 <= 0 {
		return 0
	}

	return int((int64(maxIndexQ16)-1)/int64(indexIncrementQ16) + 1)
}

//nolint:gosec // Deterministic test vector generation does not need crypto/rand.
func neonTestBuffers() []neonTestBuffer {
	fill := func(f func(int) int16) []int16 {
		buf := make([]int16, neonTestBufLen)
		for i := range buf {
			buf[i] = f(i)
		}

		return buf
	}

	buffers := []neonTestBuffer{
		{"zero", fill(func(int) int16 { return 0 })},
		{"maxInt16", fill(func(int) int16 { return math.MaxInt16 })},
		{"minInt16", fill(func(int) int16 { return math.MinInt16 })},
		{"one", fill(func(int) int16 { return 1 })},
		{"negativeOne", fill(func(int) int16 { return -1 })},
		{"alternatingExtremes", fill(func(i int) int16 {
			if i%2 == 0 {
				return math.MaxInt16
			}

			return math.MinInt16
		})},
		{"ramp", fill(func(i int) int16 { return int16(i*257 - math.MaxInt16) })},
		// Matching the tap signs drives the accumulator to its largest magnitude.
		{"signAligned", fill(func(i int) int16 {
			if resamplerFracFIR12NEON[i%12][i%8] < 0 {
				return math.MinInt16
			}

			return math.MaxInt16
		})},
	}

	for seed := int64(1); seed <= 8; seed++ {
		rng := rand.New(rand.NewSource(seed))
		buffers = append(buffers, neonTestBuffer{"random", fill(func(int) int16 { return int16(rng.Intn(1 << 16)) })})
	}

	return buffers
}

func assertNEONMatchesGeneric(t *testing.T, buf []int16, maxIndexQ16, indexIncrementQ16 int32) {
	t.Helper()

	sampleCount := neonSampleCount(maxIndexQ16, indexIncrementQ16)
	if sampleCount == 0 {
		return
	}
	if lastIndexQ16 := int64(sampleCount-1) * int64(indexIncrementQ16); (lastIndexQ16>>16)+8 > int64(len(buf)) {
		return
	}

	expected := make([]int16, sampleCount)
	assert.Equal(t, sampleCount, resamplerPrivateIIRFIRInterpolateGeneric(expected, buf, maxIndexQ16, indexIncrementQ16))

	actual := make([]int16, sampleCount)
	resamplerPrivateIIRFIRInterpolateNEON(
		&actual[0], &buf[0], &resamplerFracFIR12NEON[0][0], sampleCount, uint32(indexIncrementQ16),
	)

	assert.Equal(t, expected, actual)
}

func TestIIRFIRInterpolateNEONMatchesGeneric(t *testing.T) {
	// 0x15555 advances about one phase per output; the rest cover sub-sample,
	// unit and multi-sample strides including the largest fractional step.
	increments := []int32{
		1, 2, 0x100, 0x1555, 0x2000, 0x8000, 0xffff,
		1 << 16, 0x15555, 0x18000, 1 << 17, 0x1ffff, 3 << 16,
	}
	outputCounts := []int32{1, 2, 3, 7, 8, 9, 16, 17, 31}

	for _, buffer := range neonTestBuffers() {
		for _, increment := range increments {
			for _, outputCount := range outputCounts {
				assertNEONMatchesGeneric(t, buffer.buf, outputCount*increment, increment)
			}
		}
	}
}

func TestIIRFIRInterpolateNEONCoversEveryPhase(t *testing.T) {
	phases := make(map[int32]bool, 12)
	for indexQ16 := int32(0); indexQ16 < 1<<16; indexQ16 += 7 {
		phases[silkSMULWB(indexQ16, 12)] = true
	}
	assert.Len(t, phases, 12)

	buffers := neonTestBuffers()
	buf := buffers[len(buffers)-1].buf
	for increment := int32(1); increment < 1<<16; increment += 251 {
		assertNEONMatchesGeneric(t, buf, increment*12, increment)
	}
}

// Reassociating the eight products is only safe because the accumulator cannot
// overflow: the largest per-phase tap magnitude sum is 50045, so a full-scale
// int16 window peaks near 1.64e9. Saturation is still reachable after rounding.
func TestIIRFIRInterpolateAccumulatorBound(t *testing.T) {
	var worst int64
	for phase := range resamplerFracFIR12NEON {
		var magnitude int64
		for _, coefficient := range resamplerFracFIR12NEON[phase] {
			magnitude += int64(max(int32(coefficient), -int32(coefficient)))
		}
		worst = max(worst, magnitude*math.MaxInt16)
	}

	assert.Less(t, worst, int64(math.MaxInt32))
	assert.Greater(t, worst>>15, int64(math.MaxInt16))
}

func TestIIRFIRInterpolateNEONSaturates(t *testing.T) {
	buf := make([]int16, 16)
	for i := range buf {
		if resamplerFracFIR12NEON[0][i%8] < 0 {
			buf[i] = math.MinInt16
		} else {
			buf[i] = math.MaxInt16
		}
	}

	expected := make([]int16, 1)
	assert.Equal(t, 1, resamplerPrivateIIRFIRInterpolateGeneric(expected, buf, 1, 1<<16))
	assert.Equal(t, int16(math.MaxInt16), expected[0])

	actual := make([]int16, 1)
	resamplerPrivateIIRFIRInterpolateNEON(&actual[0], &buf[0], &resamplerFracFIR12NEON[0][0], 1, 1<<16)
	assert.Equal(t, expected, actual)
}

func TestIIRFIRInterpolateNEONAliasedOutput(t *testing.T) {
	const increment = 0x18000

	buffers := neonTestBuffers()
	source := buffers[len(buffers)-1].buf
	maxIndexQ16 := int32(32) * increment
	sampleCount := neonSampleCount(maxIndexQ16, increment)

	expected := make([]int16, len(source))
	copy(expected, source)
	assert.Equal(t, sampleCount, resamplerPrivateIIRFIRInterpolateGeneric(expected, expected, maxIndexQ16, increment))

	actual := make([]int16, len(source))
	copy(actual, source)
	resamplerPrivateIIRFIRInterpolateNEON(
		&actual[0], &actual[0], &resamplerFracFIR12NEON[0][0], sampleCount, uint32(increment),
	)

	assert.Equal(t, expected, actual)
}

func TestIIRFIRInterpolateEmptyRange(t *testing.T) {
	buffers := neonTestBuffers()
	out := make([]int16, 16)

	for _, maxIndexQ16 := range []int32{0, -1, math.MinInt32} {
		assert.Zero(t, resamplerPrivateIIRFIRInterpolate(out, buffers[0].buf, maxIndexQ16, 1<<16))
	}
}

// Without the wrapper's span checks the kernel would write past out or read
// past buf; delegating to the bounds-checked generic path must panic instead.
func TestIIRFIRInterpolateGuardsInvalidSpans(t *testing.T) {
	buffers := neonTestBuffers()
	buf := buffers[len(buffers)-1].buf

	for _, test := range []struct {
		name              string
		outLen            int
		maxIndexQ16       int32
		indexIncrementQ16 int32
	}{
		{"outputTooShort", 2, 32 << 16, 1 << 16},
		{"windowPastBuffer", 1024, int32(len(buf)) << 16, 1 << 16},
		{"zeroIncrement", 16, 1 << 20, 0},
		{"negativeIncrement", 16, 1 << 20, -(1 << 16)},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := make([]int16, test.outLen)
			assert.Panics(t, func() {
				resamplerPrivateIIRFIRInterpolate(out, buf, test.maxIndexQ16, test.indexIncrementQ16)
			})
		})
	}
}
