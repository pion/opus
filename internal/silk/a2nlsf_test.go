// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestA2NLSFRoundTrip converts a valid NLSF vector to LPC via the decoder and
// back with A2NLSF, expecting to recover the input within the fixed-point
// tolerance.
func TestA2NLSFRoundTrip(t *testing.T) {
	cases := []struct {
		bandwidth Bandwidth
		order     int
	}{
		{BandwidthNarrowband, 10},
		{BandwidthWideband, 16},
	}

	for _, tc := range cases {
		for seed := range 8 {
			t.Run(fmt.Sprintf("bw%d_seed%d", tc.bandwidth, seed), func(t *testing.T) {
				nlsf := genNLSF(tc.order, uint32(seed*131+tc.order))
				stabilizeNLSF(nlsf, tc.order, tc.bandwidth)

				dec := NewDecoder()
				a32Q17 := dec.convertNormalizedLSFsToLPCCoefficients(nlsf, tc.bandwidth)
				aFloat := make([]float32, tc.order)
				for i := range aFloat {
					aFloat[i] = float32(a32Q17[i]) / (1 << 17)
				}

				recovered := make([]int16, tc.order)
				a2nlsfFLP(recovered, aFloat, tc.order)

				// Round-trip through two different fixed-point approximations
				// (decoder NLSF->LPC and A2NLSF LPC->NLSF); band-edge roots are
				// the least precise. ~1% of full scale is expected.
				for k := range nlsf {
					assert.InDeltaf(t, nlsf[k], recovered[k], 300, "coefficient %d", k)
				}
				// Result must be a valid increasing NLSF vector.
				for k := 1; k < tc.order; k++ {
					require.Greaterf(t, recovered[k], recovered[k-1], "not increasing at %d", k)
				}
			})
		}
	}
}
