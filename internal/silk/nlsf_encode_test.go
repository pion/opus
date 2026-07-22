// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQuantizeNLSFRoundTrip quantizes an NLSF vector and reconstructs it
// through the same stage-2 residual formula the decoder uses
// (normalizeLineSpectralFrequencyStageTwo/Coefficients), without going
// through the range coder — encodeNLSF/emitNLSFIndices need the *Encoder
// type, which doesn't exist in this package yet (deferred to the
// orchestration piece, alongside findPitchLags).
func TestQuantizeNLSFRoundTrip(t *testing.T) {
	cases := []struct {
		bandwidth Bandwidth
		order     int
	}{
		{BandwidthNarrowband, 10},
		{BandwidthMediumband, 10},
		{BandwidthWideband, 16},
	}

	for _, tc := range cases {
		for seed := range 12 {
			name := fmt.Sprintf("bw%d_seed%d", tc.bandwidth, seed)
			t.Run(name, func(t *testing.T) {
				input := genNLSF(tc.order, uint32(seed*97+tc.order)) //nolint:gosec // G115

				stabilized := append([]int16(nil), input...)
				stabilizeNLSF(stabilized, len(stabilized), tc.bandwidth)
				index1, indices2, quant := quantizeNLSF(stabilized, tc.bandwidth)

				reconstructed := reconstructNLSF(index1, indices2, tc.bandwidth)
				stabilizeNLSF(reconstructed, len(reconstructed), tc.bandwidth)

				require.Len(t, reconstructed, len(quant))
				for k := range quant {
					require.Equalf(t, quant[k], reconstructed[k], "coefficient %d", k)
				}
				for k := 1; k < len(quant); k++ {
					require.Greaterf(t, quant[k], quant[k-1], "not increasing at %d", k)
				}
			})
		}
	}
}

// reconstructNLSF replays the decoder's stage-2 residual and coefficient
// reconstruction (normalizeLineSpectralFrequencyStageTwo/Coefficients)
// directly from already-decoded indices, instead of reading them from a
// range-coded bitstream.
func reconstructNLSF(index1 int, indices2 []int8, bandwidth Bandwidth) []int16 {
	order := len(indices2)
	qstepQ16, _ := nlsfStepSizes(bandwidth)

	predSelect := predictionWeightSelectionForNarrowbandAndMediumbandNormalizedLSF
	predTable := predictionWeightForNarrowbandAndMediumbandNormalizedLSF
	cb1Set := codebookNormalizedLSFStageOneNarrowbandOrMediumband
	if bandwidth == BandwidthWideband {
		predSelect = predictionWeightSelectionForWidebandNormalizedLSF
		predTable = predictionWeightForWidebandNormalizedLSF
		cb1Set = codebookNormalizedLSFStageOneWideband
	}
	cb1 := cb1Set[index1]

	resQ10 := make([]int16, order)
	for k := order - 1; k >= 0; k-- {
		firstOperand := int32(0)
		if k+1 < order {
			predQ8 := int32(predTable[predSelect[index1][k]][k]) //nolint:gosec // G115: table values are small positive weights.
			firstOperand = (int32(resQ10[k+1]) * predQ8) >> 8
		}
		resQ10[k] = int16(firstOperand + nlsfSecondOperand(int32(indices2[k]), qstepQ16)) //nolint:gosec // G115
	}

	weightsQ9 := make([]int32, order)
	for k := range order {
		weightsQ9[k] = nlsfWeightQ9(cb1, order, k)
	}

	nlsf := make([]int16, order)
	for k := range order {
		nlsf[k] = int16(clamp(0, //nolint:gosec // G115
			int32((int(cb1[k])<<7)+(int(resQ10[k])<<14)/int(weightsQ9[k])), 32767))
	}

	return nlsf
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

// TestQuantizeNLSFClusteredStaysInRange is a regression test for an inverted
// clamp in quantizeNLSF that let stage-2 indices exceed the extension range.
// The oversized index encodes an extension symbol the decoder cannot
// reproduce, so the range coder silently desynchronizes rather than failing
// loudly. Tightly clustered NLSF vectors trigger it most reliably. See
// pion/opus#147.
func TestQuantizeNLSFClusteredStaysInRange(t *testing.T) {
	cases := []struct {
		bandwidth Bandwidth
		order     int
	}{
		{BandwidthNarrowband, 10},
		{BandwidthMediumband, 10},
		{BandwidthWideband, 16},
	}

	for _, tc := range cases {
		for seed := range 64 {
			name := fmt.Sprintf("bw%d_seed%d", tc.bandwidth, seed)
			t.Run(name, func(t *testing.T) {
				input := genClusteredNLSF(tc.order, uint32(seed*131+tc.order)) //nolint:gosec // G115

				// Stage-2 indices must stay within the extension range the
				// decoder can reconstruct: [-Ext, Ext-1].
				stabilized := append([]int16(nil), input...)
				stabilizeNLSF(stabilized, len(stabilized), tc.bandwidth)
				index1, indices2, quant := quantizeNLSF(stabilized, tc.bandwidth)
				// The decoder reconstructs indices in [-Ext, Ext] inclusive
				// (RFC 6716 4.2.7.5.2). quantizeNLSF clamps ind to [-Ext, Ext-1]
				// and may then pick ind+1, so the emitted index reaches +Ext.
				for k, v := range indices2 {
					require.GreaterOrEqualf(t, int(v), -nlsfQuantMaxAmplitudeExt, "stage-2 index %d underflow", k)
					require.LessOrEqualf(t, int(v), nlsfQuantMaxAmplitudeExt, "stage-2 index %d overflow", k)
				}

				// And the reconstruction (what the decoder would compute from
				// these exact indices) must still match what quantizeNLSF
				// itself scored and returned.
				reconstructed := reconstructNLSF(index1, indices2, tc.bandwidth)
				stabilizeNLSF(reconstructed, len(reconstructed), tc.bandwidth)
				assert.Equal(t, quant, reconstructed)
			})
		}
	}
}
