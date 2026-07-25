// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reconstructGainsQ16 replays the decoder's index-to-gain reconstruction
// (decodeSubframeQuantizations's per-subframe math) directly against
// already-known indices, instead of reading them from a range-coded
// bitstream — same approach as reconstructNLSF in nlsf_encode_test.go.
func reconstructGainsQ16(indices []int8, previousLogGain int32, conditional bool) []float32 {
	gainQ16 := make([]float32, len(indices))
	prevLogGain := previousLogGain

	for subframeIndex, idx := range indices {
		var logGain int32
		if subframeIndex == 0 && !conditional {
			gainIndex := int32(idx)
			logGain = maxInt32(gainIndex, prevLogGain-16)
		} else {
			deltaGainIndex := int32(idx)
			logGain = clamp(0, maxInt32(2*deltaGainIndex-16, prevLogGain+deltaGainIndex-4), 63)
		}
		prevLogGain = logGain

		inLogQ7 := min((gainInvScaleQ16*logGain>>16)+gainOffsetQ7, gainMaxLogQ7)
		i := inLogQ7 >> 7
		f := inLogQ7 & 127
		gainQ16[subframeIndex] = float32((1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7))
	}

	return gainQ16
}

// TestQuantizeGainsRoundTrip checks that quantizeGains's own returned gains
// match what the decoder would reconstruct from the same indices, across the
// independent/delta and double-step-threshold branches.
func TestQuantizeGainsRoundTrip(t *testing.T) {
	cases := []struct {
		name          string
		subframeCount int
		conditional   bool
		prevLogGain   int32
		targetsQ16    []int32
	}{
		{
			name:          "independent_first_low_state",
			subframeCount: 4,
			conditional:   false,
			prevLogGain:   10,
			targetsQ16:    []int32{200000, 500000, 1500000, 800000},
		},
		{
			name:          "independent_first_small_gains",
			subframeCount: 2,
			conditional:   false,
			prevLogGain:   10,
			targetsQ16:    []int32{81920, 120000},
		},
		{
			name:          "conditional_delta",
			subframeCount: 4,
			conditional:   true,
			prevLogGain:   30,
			targetsQ16:    []int32{600000, 650000, 500000, 700000},
		},
		{
			name:          "large_upward_jump_double_step",
			subframeCount: 4,
			conditional:   false,
			prevLogGain:   10,
			targetsQ16:    []int32{81920, 1000000000, 1500000000, 90000},
		},
		{
			name:          "monotone_decrease",
			subframeCount: 4,
			conditional:   true,
			prevLogGain:   45,
			targetsQ16:    []int32{900000, 400000, 150000, 82000},
		},
		{
			// Raw delta is so far past doubleStepThreshold that it gets
			// pre-shrunk before the gainMinDelta/gainMaxDelta clamp — a
			// different branch than the double-step accumulation in
			// large_upward_jump_double_step above.
			name:          "delta_pre_shrink_before_clamp",
			subframeCount: 4,
			conditional:   true,
			prevLogGain:   0,
			targetsQ16:    []int32{100000, 2000000000, 2000000000, 2000000000},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previousLogGain := tc.prevLogGain
			indices, gainQ16, _ := quantizeGains(tc.targetsQ16, &previousLogGain, tc.subframeCount, tc.conditional)

			reconstructed := reconstructGainsQ16(indices, tc.prevLogGain, tc.conditional)

			require.Equal(t, reconstructed, gainQ16, "reconstructed gains differ")

			// The independent-subframe transmit index is the raw 6-bit gain
			// index; delta indices are shifted non-negative (0..40).
			if !tc.conditional {
				assert.GreaterOrEqualf(t, indices[0], int8(0), "independent index")
				assert.LessOrEqualf(t, indices[0], int8(gainNLevels-1), "independent index")
			}
			for k := 1; k < tc.subframeCount; k++ {
				if k == 0 {
					continue
				}
				assert.GreaterOrEqualf(t, indices[k], int8(0), "delta index %d", k)
				assert.LessOrEqualf(t, indices[k], int8(gainMaxDelta-gainMinDelta), "delta index %d", k)
			}
		})
	}
}

// TestEncodeSubframeGainsRoundTrip checks that encoded gains decode back to
// the same values through the decoder, with the range coder and gain state
// in sync — the same check nlsf_encode_test.go's TestQuantizeNLSFRoundTrip
// does for NLSFs, but this one goes through the real range coder since
// *Encoder exists now.
func TestEncodeSubframeGainsRoundTrip(t *testing.T) {
	cases := []struct {
		name          string
		signalType    frameSignalType
		subframeCount int
		isFirst       bool
		haveState     bool
		prevLogGain   int32
		targetsQ16    []int32
	}{
		{
			name:          "independent_first_voiced",
			signalType:    frameSignalTypeVoiced,
			subframeCount: 4,
			isFirst:       true,
			haveState:     false,
			prevLogGain:   10,
			targetsQ16:    []int32{200000, 500000, 1500000, 800000},
		},
		{
			name:          "independent_first_unvoiced",
			signalType:    frameSignalTypeUnvoiced,
			subframeCount: 4,
			isFirst:       true,
			haveState:     false,
			prevLogGain:   10,
			targetsQ16:    []int32{81920, 120000, 90000, 300000},
		},
		{
			name:          "independent_first_inactive",
			signalType:    frameSignalTypeInactive,
			subframeCount: 2,
			isFirst:       true,
			haveState:     false,
			prevLogGain:   10,
			targetsQ16:    []int32{400000, 250000},
		},
		{
			name:          "conditional_first_delta",
			signalType:    frameSignalTypeVoiced,
			subframeCount: 4,
			isFirst:       false,
			haveState:     true,
			prevLogGain:   30,
			targetsQ16:    []int32{600000, 650000, 500000, 700000},
		},
		{
			name:          "large_upward_jump_double_step",
			signalType:    frameSignalTypeVoiced,
			subframeCount: 4,
			isFirst:       true,
			haveState:     false,
			prevLogGain:   10,
			targetsQ16:    []int32{81920, 1000000000, 1500000000, 90000},
		},
		{
			name:          "monotone_decrease",
			signalType:    frameSignalTypeUnvoiced,
			subframeCount: 4,
			isFirst:       false,
			haveState:     true,
			prevLogGain:   45,
			targetsQ16:    []int32{900000, 400000, 150000, 82000},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc := NewEncoder()
			enc.haveEncoded = tc.haveState
			enc.previousLogGain = tc.prevLogGain
			enc.rangeEncoder.Init()

			gains := enc.encodeSubframeGains(tc.targetsQ16, tc.signalType, tc.subframeCount, tc.isFirst)
			encRange := enc.rangeEncoder.FinalRange()
			data := enc.rangeEncoder.Done()

			dec := NewDecoder()
			dec.haveDecoded = tc.haveState
			dec.previousLogGain = tc.prevLogGain
			dec.rangeDecoder.Init(data)

			decGains := dec.decodeSubframeQuantizations(tc.signalType, tc.subframeCount, tc.isFirst)

			require.Equal(t, gains, decGains, "reconstructed gains differ")
			assert.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
			assert.Equal(t, enc.previousLogGain, dec.previousLogGain, "previousLogGain desync")
		})
	}
}

// TestLin2LogRoundTripAgainstGainDequant checks that lin2log is a near-inverse
// of the silk_log2lin() spelled out inline in the gain code, matching the
// reference tolerance (they are only approximate inverses).
func TestLin2LogRoundTripAgainstGainDequant(t *testing.T) {
	// For every gain index 0..63, the log domain value fed to log2lin is
	// exactly recoverable, so lin2log(log2lin(x)) must return x for the gain
	// grid points used by the codec.
	for logGain := range int32(gainNLevels) {
		inLogQ7 := (gainInvScaleQ16 * logGain >> 16) + gainOffsetQ7
		inLogQ7 = min(inLogQ7, gainMaxLogQ7)
		i := inLogQ7 >> 7
		f := inLogQ7 & 127
		gainQ16 := (1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7)

		got := lin2log(gainQ16)
		// lin2log/log2lin are approximate inverses; the reference keeps the
		// round-trip within a couple of Q7 units.
		diff := got - inLogQ7
		if diff < 0 {
			diff = -diff
		}
		assert.LessOrEqualf(t, diff, int32(3), "lin2log(log2lin(%d))=%d want ~%d", logGain, got, inLogQ7)
	}
}
