// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSigmQ15(t *testing.T) {
	assert.Equal(t, int32(16384), sigmQ15(0))
	assert.Equal(t, int32(32767), sigmQ15(1000))
	assert.Equal(t, int32(0), sigmQ15(-1000))
	assert.Greater(t, sigmQ15(32), sigmQ15(0))
	assert.Less(t, sigmQ15(-32), sigmQ15(0))
}

func TestSqrtApprox(t *testing.T) {
	assert.Equal(t, int32(0), sqrtApprox(0))
	assert.Equal(t, int32(0), sqrtApprox(-5))
	// The approximation is documented as within ~10% for small outputs and
	// ~2.5% for outputs above 120.
	for _, x := range []int32{1000, 50000, 1 << 20, 1 << 28} {
		got := float64(sqrtApprox(x))
		want := math.Sqrt(float64(x))
		assert.InEpsilonf(t, want, got, 0.11, "sqrtApprox(%d)", x)
	}
}

// tone builds a full-scale-ish sine at freq Hz for the given length.
func tone(length, freqHz, fsHz int, amp float64) []int16 {
	pcm := make([]int16, length)
	for i := range pcm {
		pcm[i] = int16(amp * math.Sin(2*math.Pi*float64(freqHz)*float64(i)/float64(fsHz)))
	}

	return pcm
}

func TestVADSpeechActivityBounds(t *testing.T) {
	const fsKHz = 16
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
