// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPLCMinimumLTPGainTruncation(t *testing.T) {
	decoder := NewDecoder()
	out := make([]float32, 320)
	require.NoError(t, decoder.Decode(testSilkFrame(), out, false, nanoseconds20Ms, BandwidthWideband))
	// silk_LTP_gain_vq_0 includes [0,0,2,0,0]. In silk_PLC_update
	// this produces gain 256 and scale 45876. SMULBB first truncates the
	// scale to signed int16, yielding -4915 rather than +11469.
	taps := [][]int8{{0, 0, 2, 0, 0}, {0, 0, 2, 0, 0}, {0, 0, 2, 0, 0}, {0, 0, 2, 0, 0}}
	decoder.updateFixedPLC(frameSignalTypeVoiced, 4, BandwidthWideband, []int{100, 100, 100, 100}, taps, 16384)
	require.Equal(t, [5]int16{0, 0, -4915, 0, 0}, decoder.fixedPLC.ltpCoefficients)
}

func TestCNGBandwidthResetRetainsHistory(t *testing.T) {
	decoder := NewDecoder()
	decoder.previousBandwidth = BandwidthWideband
	decoder.fixedCNG.fsKHz = 16
	decoder.fixedCNG.excitationQ14[37] = 123456
	decoder.fixedCNG.synthesisQ14[3] = -654321
	decoder.plcLossCount = 2
	decoder.fixedPrevGainQ16 = 123456
	decoder.fixedPLC.lastFrameLost = true
	decoder.fixedPLC.concealedEnergy = 1234
	decoder.fixedPLC.randomSeed = 5678
	decoder.resetPredictionForBandwidthChange(BandwidthNarrowband)
	decoder.resetFixedPLC(160, 8)
	decoder.resetFixedCNG(10, 8)
	require.Equal(t, int32(123456), decoder.fixedCNG.excitationQ14[37])
	require.Equal(t, int32(-654321), decoder.fixedCNG.synthesisQ14[3])
	require.Zero(t, decoder.fixedCNG.smoothedGainQ16)
	require.Equal(t, fixedCNGRandomSeed, decoder.fixedCNG.randomSeed)
	require.Equal(t, 2, decoder.plcLossCount)
	require.Equal(t, int32(123456), decoder.fixedPrevGainQ16)
	require.True(t, decoder.fixedPLC.lastFrameLost)
	require.Equal(t, int32(1234), decoder.fixedPLC.concealedEnergy)
	require.Equal(t, int32(5678), decoder.fixedPLC.randomSeed)
}
