// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSilkLog2(t *testing.T) {
	assert.InDelta(t, 3, silkLog2(8), 1e-4)
	assert.InDelta(t, 0, silkLog2(1), 1e-4)
}

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
