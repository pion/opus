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
