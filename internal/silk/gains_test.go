// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeSubframeGainsRoundTrip checks that encoded gains decode back to the
// same values through the decoder, with the range coder and gain state in sync.
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

// TestLin2LogInverseOfLog2Lin checks that lin2log is a near-inverse of the
// silk_log2lin() spelled out inline in the gain code, matching the reference
// tolerance (they are only approximate inverses).
func TestLin2LogRoundTripAgainstGainDequant(t *testing.T) {
	// For every gain index 0..63, the log domain value fed to log2lin is
	// exactly recoverable, so lin2log(log2lin(x)) must return x for the gain
	// grid points used by the codec.
	for logGain := int32(0); logGain < gainNLevels; logGain++ {
		inLogQ7 := (gainInvScaleQ16 * logGain >> 16) + gainOffsetQ7
		if inLogQ7 > gainMaxLogQ7 {
			inLogQ7 = gainMaxLogQ7
		}
		i := inLogQ7 >> 7
		f := inLogQ7 & 127
		gainQ16 := (1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7)

		got := lin2log(int32(gainQ16))
		// lin2log/log2lin are approximate inverses; the reference keeps the
		// round-trip within a couple of Q7 units.
		diff := got - inLogQ7
		if diff < 0 {
			diff = -diff
		}
		assert.LessOrEqualf(t, diff, int32(3), "lin2log(log2lin(%d))=%d want ~%d", logGain, got, inLogQ7)
	}
}
