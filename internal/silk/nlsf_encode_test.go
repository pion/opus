// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genNLSF builds a deterministic, strictly increasing NLSF vector in Q15.
func genNLSF(order int, seed uint32) []int16 {
	nlsf := make([]int16, order)
	state := seed
	cur := int32(400)
	for k := range nlsf {
		state = 1664525*state + 1013904223
		cur += int32(state>>20)%2000 + 700
		if cur > 32200 {
			cur = 32200
		}
		nlsf[k] = int16(cur)
	}

	return nlsf
}

// TestEncodeNLSFRoundTrip encodes an NLSF vector and decodes it back through
// the decoder, asserting the reconstructed vector and range coder match and
// the result is a valid (stabilized, increasing) NLSF vector.
func TestEncodeNLSFRoundTrip(t *testing.T) {
	cases := []struct {
		bandwidth Bandwidth
		order     int
	}{
		{BandwidthNarrowband, 10},
		{BandwidthMediumband, 10},
		{BandwidthWideband, 16},
	}

	for _, tc := range cases {
		for _, voiced := range []bool{false, true} {
			for seed := range 12 {
				name := fmt.Sprintf("bw%d_voiced%t_seed%d", tc.bandwidth, voiced, seed)
				t.Run(name, func(t *testing.T) {
					input := genNLSF(tc.order, uint32(seed*97+tc.order+boolSeed(voiced))) //nolint:gosec // G115

					enc := NewEncoder()
					enc.rangeEncoder.Init()
					quant := enc.encodeNLSF(append([]int16(nil), input...), tc.bandwidth, voiced)
					encRange := enc.rangeEncoder.FinalRange()
					data := enc.rangeEncoder.Done()

					dec := NewDecoder()
					dec.rangeDecoder.Init(data)
					index1 := dec.normalizeLineSpectralFrequencyStageOne(voiced, tc.bandwidth)
					dLPC, resQ10 := dec.normalizeLineSpectralFrequencyStageTwo(tc.bandwidth, index1)
					nlsf := dec.normalizeLineSpectralFrequencyCoefficients(dLPC, tc.bandwidth, resQ10, index1)
					dec.normalizeLSFStabilization(nlsf, dLPC, tc.bandwidth)

					require.Len(t, nlsf, len(quant))
					for k := range quant {
						require.Equalf(t, quant[k], nlsf[k], "coefficient %d", k)
					}
					assert.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")

					for k := 1; k < len(quant); k++ {
						require.Greaterf(t, quant[k], quant[k-1], "not increasing at %d", k)
					}
				})
			}
		}
	}
}

func boolSeed(b bool) int {
	if b {
		return 1
	}

	return 0
}

// genClusteredNLSF builds a tightly clustered NLSF vector: all coefficients sit
// close together around a seed-dependent center, so stabilization pushes them to
// minimum spacing and the stage-2 residuals grow large. These are the vectors
// that expose an out-of-range stage-2 index if the quantizer fails to clamp.
func genClusteredNLSF(order int, seed uint32) []int16 {
	nlsf := make([]int16, order)
	state := seed
	state = 1664525*state + 1013904223
	center := int32(2000) + int32(state>>18)%28000
	cur := center
	for k := range nlsf {
		state = 1664525*state + 1013904223
		cur += int32(state>>28) % 3 // 0..2 Q15: far below the minimum spacing.
		if cur > 32200 {
			cur = 32200
		}
		nlsf[k] = int16(cur)
	}

	return nlsf
}

// TestEncodeNLSFClusteredStaysInRange is a regression test for an inverted
// clamp in quantizeNLSF that let stage-2 indices exceed the extension range.
// The oversized index encodes an extension symbol the decoder cannot reproduce,
// so the range coder silently desynchronizes rather than failing loudly. Tightly
// clustered NLSF vectors trigger it most reliably. See pion/opus#147.
func TestEncodeNLSFClusteredStaysInRange(t *testing.T) {
	cases := []struct {
		bandwidth Bandwidth
		order     int
	}{
		{BandwidthNarrowband, 10},
		{BandwidthMediumband, 10},
		{BandwidthWideband, 16},
	}

	for _, tc := range cases {
		for _, voiced := range []bool{false, true} {
			for seed := range 64 {
				name := fmt.Sprintf("bw%d_voiced%t_seed%d", tc.bandwidth, voiced, seed)
				t.Run(name, func(t *testing.T) {
					input := genClusteredNLSF(tc.order, uint32(seed*131+tc.order+boolSeed(voiced))) //nolint:gosec // G115

					// Stage-2 indices must stay within the extension range the
					// decoder can reconstruct: [-Ext, Ext-1].
					stabilized := append([]int16(nil), input...)
					stabilizeNLSF(stabilized, len(stabilized), tc.bandwidth)
					_, indices2, _ := quantizeNLSF(stabilized, tc.bandwidth)
					// The decoder reconstructs indices in [-Ext, Ext] inclusive
					// (RFC 6716 4.2.7.5.2). quantizeNLSF clamps ind to [-Ext, Ext-1]
					// and may then pick ind+1, so the emitted index reaches +Ext.
					for k, v := range indices2 {
						require.GreaterOrEqualf(t, int(v), -nlsfQuantMaxAmplitudeExt, "stage-2 index %d underflow", k)
						require.LessOrEqualf(t, int(v), nlsfQuantMaxAmplitudeExt, "stage-2 index %d overflow", k)
					}

					// And the full encode->decode round-trip must stay in sync.
					enc := NewEncoder()
					enc.rangeEncoder.Init()
					quant := enc.encodeNLSF(append([]int16(nil), input...), tc.bandwidth, voiced)
					encRange := enc.rangeEncoder.FinalRange()
					data := enc.rangeEncoder.Done()

					dec := NewDecoder()
					dec.rangeDecoder.Init(data)
					index1 := dec.normalizeLineSpectralFrequencyStageOne(voiced, tc.bandwidth)
					dLPC, resQ10 := dec.normalizeLineSpectralFrequencyStageTwo(tc.bandwidth, index1)
					nlsf := dec.normalizeLineSpectralFrequencyCoefficients(dLPC, tc.bandwidth, resQ10, index1)
					dec.normalizeLSFStabilization(nlsf, dLPC, tc.bandwidth)

					require.Len(t, nlsf, len(quant))
					for k := range quant {
						require.Equalf(t, quant[k], nlsf[k], "coefficient %d", k)
					}
					assert.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
				})
			}
		}
	}
}
