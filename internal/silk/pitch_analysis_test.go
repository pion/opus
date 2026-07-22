// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPitchAnalysisCoreDetectsPeriod feeds a periodic signal and checks the
// detected lag matches the period and the frame reads as voiced.
func TestPitchAnalysisCoreDetectsPeriod(t *testing.T) {
	const (
		fsKHz   = 16
		nbSubfr = 4
		period  = 80 // 16 kHz / 80 = 200 Hz
	)
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frame := make([]float32, frameLength)
	for i := range frame {
		frame[i] = float32(5000 * math.Sin(2*math.Pi*float64(i)/period))
	}

	pitchOut := make([]int, nbSubfr)
	ltpCorr := float32(0)
	lagIndex, contourIndex, voiced := pitchAnalysisCore(frame, pitchOut, &ltpCorr, 0, 0.4, 0.3, fsKHz, 2, nbSubfr)

	require.True(t, voiced, "a clean periodic tone should be voiced")
	for k, p := range pitchOut {
		assert.InDeltaf(t, period, p, 8, "subframe %d lag", k)
	}
	assert.GreaterOrEqual(t, lagIndex, int16(0))
	assert.GreaterOrEqual(t, contourIndex, int8(0))
	assert.Greater(t, ltpCorr, float32(0))
}

// TestPitchAnalysisCoreEscapesOnNoise checks the low-correlation escape path
// runs without error and yields zeroed lags when unvoiced.
func TestPitchAnalysisCoreEscapesOnNoise(t *testing.T) {
	const (
		fsKHz   = 16
		nbSubfr = 4
	)
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frame := make([]float32, frameLength)
	state := uint32(987654321)
	for i := range frame {
		state = 1664525*state + 1013904223
		frame[i] = float32(int32(state>>16)%400 - 200)
	}

	pitchOut := make([]int, nbSubfr)
	ltpCorr := float32(0)
	_, _, voiced := pitchAnalysisCore(frame, pitchOut, &ltpCorr, 0, 0.7, 0.6, fsKHz, 2, nbSubfr)

	if !voiced {
		for _, p := range pitchOut {
			assert.Equal(t, 0, p)
		}
		assert.Equal(t, float32(0), ltpCorr)
	}
}

func TestPitchAnalysisCore8kHz(t *testing.T) {
	const (
		fsKHz   = 8
		nbSubfr = 4
		period  = 48
	)
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frame := make([]float32, frameLength)
	for i := range frame {
		frame[i] = float32(5000 * math.Sin(2*math.Pi*float64(i)/period))
	}

	pitchOut := make([]int, nbSubfr)
	ltpCorr := float32(0)
	_, _, voiced := pitchAnalysisCore(frame, pitchOut, &ltpCorr, 0, 0.4, 0.3, fsKHz, 2, nbSubfr)

	require.True(t, voiced)
	for k, p := range pitchOut {
		assert.InDeltaf(t, period, p, 6, "subframe %d lag", k)
	}
}

// TestPitchAnalysisCoreSilenceEscapesEarly exercises the cmax<0.2 first-stage
// escape — distinct from TestPitchAnalysisCoreEscapesOnNoise, which happens to
// fall through to the later lag==-1 escape instead. A silent frame has zero
// cross-correlation at every lag, so it bails out right after the 4 kHz
// stage without ever reaching the codebook search.
func TestPitchAnalysisCoreSilenceEscapesEarly(t *testing.T) {
	const (
		fsKHz   = 16
		nbSubfr = 4
	)
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frame := make([]float32, frameLength)

	pitchOut := make([]int, nbSubfr)
	ltpCorr := float32(1) // non-zero, to confirm the escape resets it.
	lagIndex, contourIndex, voiced := pitchAnalysisCore(frame, pitchOut, &ltpCorr, 0, 0.4, 0.3, fsKHz, 2, nbSubfr)

	require.False(t, voiced)
	assert.Equal(t, int16(0), lagIndex)
	assert.Equal(t, int8(0), contourIndex)
	assert.Equal(t, float32(0), ltpCorr)
	for _, p := range pitchOut {
		assert.Equal(t, 0, p)
	}
}

// TestPitchAnalysisCorePrevLagBias exercises the previous-lag continuity
// bias (both the prevLag>0 Q-domain conversion and the delta-lag penalty in
// the stage-2 codebook search), which no other test in this file reaches
// since they all pass prevLag=0. A clean periodic tone with prevLag set to
// the true period should still lock onto that period — the bias favors it,
// it doesn't need to override it.
func TestPitchAnalysisCorePrevLagBias(t *testing.T) {
	const (
		fsKHz   = 16
		nbSubfr = 4
		period  = 80
	)
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frame := make([]float32, frameLength)
	for i := range frame {
		frame[i] = float32(5000 * math.Sin(2*math.Pi*float64(i)/period))
	}

	pitchOut := make([]int, nbSubfr)
	ltpCorr := float32(0.8)
	_, _, voiced := pitchAnalysisCore(frame, pitchOut, &ltpCorr, period, 0.4, 0.3, fsKHz, 2, nbSubfr)

	require.True(t, voiced)
	for k, p := range pitchOut {
		assert.InDeltaf(t, period, p, 8, "subframe %d lag", k)
	}
}

// TestPitchAnalysisCore10ms exercises the 10 ms (nbSubfr==2) codebook path —
// its own stage-2/stage-3 lag tables and search sizes, never reached by the
// other tests in this file, which all use the 20 ms (nbSubfr==4) frame size.
func TestPitchAnalysisCore10ms(t *testing.T) {
	const (
		fsKHz   = 16
		nbSubfr = peMaxNBSubfr >> 1
		period  = 80
	)
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frame := make([]float32, frameLength)
	for i := range frame {
		frame[i] = float32(5000 * math.Sin(2*math.Pi*float64(i)/period))
	}

	pitchOut := make([]int, nbSubfr)
	ltpCorr := float32(0)
	_, _, voiced := pitchAnalysisCore(frame, pitchOut, &ltpCorr, 0, 0.4, 0.3, fsKHz, 2, nbSubfr)

	require.True(t, voiced)
	for k, p := range pitchOut {
		assert.InDeltaf(t, period, p, 8, "subframe %d lag", k)
	}
}
