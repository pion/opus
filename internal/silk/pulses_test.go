// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lcgPulses generates deterministic signed pulses in [-maxMag, maxMag].
func lcgPulses(seed uint32, n int, maxMag int32) []int8 {
	pulses := make([]int8, n)
	state := seed
	for i := range pulses {
		state = 1664525*state + 1013904223
		pulses[i] = int8(int32(state>>24)%(2*maxMag+1) - maxMag) //nolint:gosec // G115: bounded to [-maxMag,maxMag].
	}

	return pulses
}

// TestEncodePulsesRoundTrip encodes quantized excitation indices and decodes
// them back through the real decoder path, asserting the signed pulses and the
// range coder state match. Higher magnitudes force LSB downscaling, exercising
// the escape and LSB paths.
func TestEncodePulsesRoundTrip(t *testing.T) {
	signalTypes := []frameSignalType{frameSignalTypeInactive, frameSignalTypeUnvoiced, frameSignalTypeVoiced}
	offsets := []frameQuantizationOffsetType{frameQuantizationOffsetTypeLow, frameQuantizationOffsetTypeHigh}
	lengths := []int{80, 160, 320}
	mags := []int32{1, 3, 8}

	for _, signalType := range signalTypes {
		for _, offset := range offsets {
			for _, length := range lengths {
				for _, mag := range mags {
					name := fmt.Sprintf("t%d_o%d_len%d_mag%d", signalType, offset, length, mag)
					t.Run(name, func(t *testing.T) {
						seed := uint32(int(signalType)*131 + int(offset)*17 + length + int(mag))
						pulses := lcgPulses(seed, length, mag)

						enc := NewEncoder()
						enc.rangeEncoder.Init()
						enc.encodePulses(signalType, offset, pulses, length)
						encRange := enc.rangeEncoder.FinalRange()
						data := enc.rangeEncoder.Done()

						dec := NewDecoder()
						dec.rangeDecoder.Init(data)
						shellblocks := length / shellCodecFrameLength
						rateLevel := dec.decodeRatelevel(signalType == frameSignalTypeVoiced)
						pulsecounts, lsbcounts := dec.decodePulseAndLSBCounts(shellblocks, rateLevel)
						eRaw := dec.decodePulseLocation(pulsecounts)
						dec.decodeExcitationLSB(eRaw, lsbcounts)
						dec.decodeExcitationSign(eRaw, signalType, offset, pulsecounts)

						require.Len(t, eRaw, len(pulses))
						for i := range pulses {
							require.Equalf(t, int32(pulses[i]), eRaw[i], "pulse %d", i)
						}
						assert.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
					})
				}
			}
		}
	}
}
