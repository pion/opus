// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodePLC(t *testing.T) {
	decoder := NewDecoder()
	initialPLC := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(initialPLC, false, 1, nanoseconds20Ms, BandwidthWideband))
	assert.Zero(t, signalEnergy(initialPLC))

	decoded := make([]float32, 320)
	require.NoError(t, decoder.Decode(testSilkFrame(), decoded, false, nanoseconds20Ms, BandwidthWideband))

	firstPLC := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(firstPLC, false, 1, nanoseconds20Ms, BandwidthWideband))
	firstEnergy := signalEnergy(firstPLC)
	assert.Positive(t, firstEnergy)

	secondPLC := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(secondPLC, false, 1, nanoseconds20Ms, BandwidthWideband))
	assert.Less(t, signalEnergy(secondPLC), firstEnergy)

	recovered := make([]float32, 320)
	require.NoError(t, decoder.Decode(testSilkFrame(), recovered, false, nanoseconds20Ms, BandwidthWideband))
	for _, sample := range recovered {
		assert.False(t, math.IsNaN(float64(sample)))
		assert.False(t, math.IsInf(float64(sample), 0))
	}
	assert.Zero(t, decoder.plcLossCount)
}

func TestDecodePLCStereoToMono(t *testing.T) {
	decoder := NewDecoder()
	decoder.haveDecoded = true
	decoder.sideDecoder.haveDecoded = true
	for i := range decoder.finalOutValues {
		decoder.finalOutValues[i] = float32(i%17) / 17
		decoder.sideDecoder.finalOutValues[i] = float32(i%11) / 22
	}

	out := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(
		out,
		true,
		1,
		nanoseconds20Ms,
		BandwidthWideband,
	))
	assert.Positive(t, signalEnergy(out))
}

func TestDecodePLCVoiced(t *testing.T) {
	decoder := NewDecoder()
	decoder.haveDecoded = true
	decoder.isPreviousFrameVoiced = true
	decoder.previousLag = 100
	decoder.pitchLags = []int{100}
	decoder.n0Q15 = make([]int16, 16)
	for i := range decoder.finalOutValues {
		decoder.finalOutValues[i] = float32(i%20-10) / 10
	}

	firstPLC := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(firstPLC, false, 1, nanoseconds20Ms, BandwidthWideband))
	firstEnergy := signalEnergy(firstPLC)
	assert.Positive(t, firstEnergy)
	assert.Equal(t, 101, decoder.previousLag)
	assert.Len(t, decoder.previousFrameLPCValues, len(decoder.n0Q15))

	decoder.pitchLags = nil
	secondPLC := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(secondPLC, false, 1, nanoseconds20Ms, BandwidthWideband))
	assert.Less(t, signalEnergy(secondPLC), firstEnergy)
	assert.Equal(t, 2, decoder.plcLossCount)
	assert.Equal(t, 102, decoder.previousLag)
}

func TestDecodePLCValidation(t *testing.T) {
	decoder := NewDecoder()
	out := make([]float32, 320)

	assert.Zero(t, signalEnergy(nil))
	assert.ErrorIs(t, decoder.DecodePLC(out, false, 0, nanoseconds20Ms, BandwidthWideband), errOutBufferTooSmall)
	assert.ErrorIs(t, decoder.DecodePLC(out, false, 1, 0, BandwidthWideband), errUnsupportedSilkFrameDuration)
	assert.ErrorIs(t, decoder.DecodePLC(out[:319], false, 1, nanoseconds20Ms, BandwidthWideband), errOutBufferTooSmall)
}

func TestDecodePLCStereoMidOnly(t *testing.T) {
	decoder := NewDecoder()
	decoder.haveDecoded = true
	decoder.sideDecoder = nil
	decoder.previousDecodeOnlyMid = true
	for i := range decoder.finalOutValues {
		decoder.finalOutValues[i] = float32(i%17) / 17
	}

	out := make([]float32, 640)
	require.NoError(t, decoder.DecodePLC(out, true, 2, nanoseconds20Ms, BandwidthWideband))
	assert.NotNil(t, decoder.sideDecoder)
	assert.Positive(t, signalEnergy(out))
}
