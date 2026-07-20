// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sigmQ15 and sqrtApprox (both used below) are tested directly in
// math_fix_test.go, where they're defined — no need to re-verify their
// fixed-point math here, VAD only needs them to behave, not to re-prove it.

// tone builds a full-scale-ish sine at freq Hz for the given length.
func tone(length, freqHz, fsHz int, amp float64) []int16 {
	pcm := make([]int16, length)
	for i := range pcm {
		pcm[i] = int16(amp * math.Sin(2*math.Pi*float64(freqHz)*float64(i)/float64(fsHz)))
	}

	return pcm
}

func TestVADSpeechActivityBounds(t *testing.T) {
	// SILK's three internal rates (NB/MB/WB) — getSpeechActivityQ8 branches
	// on fsKHz internally (frame-length checks), so exercise all three
	// instead of only the WB rate this encoder starts with.
	for _, fsKHz := range []int{8, 12, 16} {
		t.Run(fmt.Sprintf("%dkHz", fsKHz), func(t *testing.T) {
			frameLength := 20 * fsKHz

			silence := make([]int16, frameLength)
			loud := tone(frameLength, 300, fsKHz*1000, 8000)

			silentVAD := newVADState()
			loudVAD := newVADState()
			var silentSA, loudSA int
			for range 10 {
				var quality [vadNBands]int32
				silentSA, _, quality = silentVAD.getSpeechActivityQ8(silence, frameLength, fsKHz)
				require.GreaterOrEqual(t, silentSA, 0)
				require.LessOrEqual(t, silentSA, 255)
				for _, q := range quality {
					require.GreaterOrEqual(t, q, int32(0))
					require.LessOrEqual(t, q, int32(32767))
				}

				loudSA, _, _ = loudVAD.getSpeechActivityQ8(loud, frameLength, fsKHz)
				require.GreaterOrEqual(t, loudSA, 0)
				require.LessOrEqual(t, loudSA, 255)
			}

			assert.Greater(t, loudSA, silentSA, "a loud tone should read as more active than silence")
			assert.Less(t, silentSA, 64, "silence should read as low activity")
		})
	}
}

// TestVADHighEnergyRatio drives band 0's energy well past the 1<<23
// threshold that switches getSpeechActivityQ8 to the coarser division path
// for NrgToNoiseRatio_Q8 (the "else" branch when Xnrg&0xFF800000 != 0) — a
// near-full-scale tone reaches it within a handful of frames.
func TestVADHighEnergyRatio(t *testing.T) {
	const fsKHz = 16
	frameLength := 20 * fsKHz
	loud := tone(frameLength, 300, fsKHz*1000, 32000)

	v := newVADState()
	var sa int
	for range 5 {
		sa, _, _ = v.getSpeechActivityQ8(loud, frameLength, fsKHz)
	}

	masked := uint32(v.xnrgSubfr[0]) & 0xFF800000 //nolint:gosec // G115: xnrgSubfr is non-negative here.
	require.Greater(t, masked, uint32(0), "test setup should actually reach the high-energy branch")
	assert.GreaterOrEqual(t, sa, 0)
	assert.LessOrEqual(t, sa, 255)
}

// TestVADMidPowerScaling exercises the 0 < powerNrg < 16384 branch of the
// power-level scaling step — moderate, sustained energy above the noise
// floor but well short of the high-energy path above. powerNrg itself is a
// local inside getSpeechActivityQ8 (xnrgSubfr alone isn't the same value —
// it's only the last subframe's raw sum, not the full accumulated Xnrg the
// switch actually branches on), so this amplitude was picked by temporarily
// instrumenting the function directly and observing powerNrg land in
// [86, 286] across every one of these calls; there's no way to assert that
// from the test's side without re-deriving the calculation, so the coverage
// itself is the check here.
func TestVADMidPowerScaling(t *testing.T) {
	const fsKHz = 16
	frameLength := 20 * fsKHz
	pcm := tone(frameLength, 300, fsKHz*1000, 500)

	v := newVADState()
	var sa int
	for range 4 {
		sa, _, _ = v.getSpeechActivityQ8(pcm, frameLength, fsKHz)
	}

	assert.GreaterOrEqual(t, sa, 0)
	assert.LessOrEqual(t, sa, 255)
}

func TestVADDeterministic(t *testing.T) {
	const fsKHz = 16
	frameLength := 10 * fsKHz
	pcm := tone(frameLength, 500, fsKHz*1000, 5000)

	a := newVADState()
	b := newVADState()
	for range 5 {
		saA, tiltA, qualA := a.getSpeechActivityQ8(pcm, frameLength, fsKHz)
		saB, tiltB, qualB := b.getSpeechActivityQ8(pcm, frameLength, fsKHz)
		require.Equal(t, saA, saB)
		require.Equal(t, tiltA, tiltB)
		require.Equal(t, qualA, qualB)
	}
}

func TestHPVariableCutoff(t *testing.T) {
	lo := lin2log(variableHPMinCutoffHz) << 8
	hi := lin2log(variableHPMaxCutoffHz) << 8
	initial := (lo + hi) / 2

	// Unvoiced previous frame leaves the smoother untouched.
	got := hpVariableCutoff(initial, frameSignalTypeUnvoiced, 120, 16, 20000, 200)
	assert.Equal(t, initial, got)

	// Voiced previous frame keeps the smoother within the cutoff range.
	got = hpVariableCutoff(initial, frameSignalTypeVoiced, 120, 16, 20000, 200)
	assert.GreaterOrEqual(t, got, lo)
	assert.LessOrEqual(t, got, hi)
}

// TestHPVariableCutoffDecreasingPitch exercises the deltaFreqQ7 < 0 branch
// (silk_HP_variable_cutoff's "less smoothing for decreasing pitch frequency"
// case): starting the smoother at the top of its range makes the computed
// pitch-frequency delta come out negative, tripling its magnitude before the
// clamp.
func TestHPVariableCutoffDecreasingPitch(t *testing.T) {
	lo := lin2log(variableHPMinCutoffHz) << 8
	hi := lin2log(variableHPMaxCutoffHz) << 8

	got := hpVariableCutoff(hi, frameSignalTypeVoiced, 120, 16, 20000, 200)
	assert.Less(t, got, hi, "a lower computed pitch frequency should pull the smoother down")
	assert.GreaterOrEqual(t, got, lo)
}
