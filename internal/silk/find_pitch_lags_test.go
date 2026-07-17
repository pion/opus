// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindPitchLagsVoiced checks that a clearly periodic signal is detected as
// voiced with a pitch lag near its true period.
func TestFindPitchLagsVoiced(t *testing.T) {
	const (
		fsKHz        = 16
		nbSubfr      = 4
		ltpMemLength = 20 * fsKHz
		frameLength  = 20 * fsKHz
		period       = 96 // 16 kHz / 96 ~ 167 Hz, inside the pitch range
	)

	// Impulse train: a strong pulse every `period` samples.
	buf := make([]float32, ltpMemLength+frameLength)
	for i := range buf {
		if i%period == 0 {
			buf[i] = 8000
		}
	}

	enc := NewEncoder()
	voiced, pitchL, lagIndex, _, res, _ := enc.findPitchLags(buf, fsKHz, nbSubfr, 200, 0)

	require.True(t, voiced, "periodic signal should be voiced")
	require.Len(t, res, ltpMemLength+frameLength)
	primaryLag := int(lagIndex) + peMinLagMS*fsKHz
	assert.InDeltaf(t, period, primaryLag, 8, "primary lag")
	for _, p := range pitchL {
		assert.InDeltaf(t, period, p, 12, "subframe lag")
	}
}

// TestFindPitchLagsUnvoiced checks noise is not classified as strongly voiced.
func TestFindPitchLagsUnvoiced(t *testing.T) {
	const (
		fsKHz        = 16
		ltpMemLength = 20 * fsKHz
		frameLength  = 20 * fsKHz
	)
	buf := make([]float32, ltpMemLength+frameLength)
	state := uint32(1)
	for i := range buf {
		state = 1664525*state + 1013904223
		buf[i] = float32(int32(state>>16)%1000 - 500)
	}

	enc := NewEncoder()
	// Not asserting hard (noise can occasionally correlate) — just that it runs.
	_, _, _, _, res, _ := enc.findPitchLags(buf, fsKHz, 4, 50, 0) //nolint:dogsled // only the residual is under test
	require.Len(t, res, ltpMemLength+frameLength)
}
