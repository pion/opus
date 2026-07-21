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
		cur += int32(state>>20)%2000 + 700 //nolint:gosec // G115: bounded arithmetic.
		if cur > 32200 {
			cur = 32200
		}
		nlsf[k] = int16(cur)
	}

	return nlsf
}

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
				nlsf := genNLSF(tc.order, uint32(seed*131+tc.order)) //nolint:gosec // G115: non-negative test seed.

				dec := NewDecoder()
				dec.normalizeLSFStabilization(nlsf, tc.order, tc.bandwidth)
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

// TestA2NLSFInitialRootBelowZero exercises the branch where the even
// polynomial's value at cos(0) is already negative on the very first
// evaluation, forcing nlsf[0] to 0 and switching to the odd polynomial to
// search for the first root. The branch is only observable indirectly (via
// nlsf[0]==0, since a2nlsf keeps no other externally visible state) — these
// aQ16 vectors were found by brute-force search over random coefficients,
// not derived analytically.
//
// Unlike TestA2NLSFRoundTrip, these aQ16 values are NOT derived from a valid
// whitening filter (no real encoder would produce them), so the classic
// interlacing-root guarantee that makes NLSFs monotonic doesn't apply here —
// only boundedness does.
func TestA2NLSFInitialRootBelowZero(t *testing.T) {
	cases := []struct {
		name  string
		order int
		aQ16  []int32
	}{
		{"order10", 10, []int32{479952, -135678, 36600, -1536, 84504, -11100, 118244, 21727, -7612, -30372}},
		{"order16", 16, []int32{82040, -77114, 55740, -3567, 27247, -762, 3861, 1517, -119, 1306, 213, 3, -42, 80, 24, 66}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aQ16 := append([]int32(nil), tc.aQ16...)
			nlsf := make([]int16, tc.order)
			a2nlsf(nlsf, aQ16, tc.order)

			assert.Equal(t, int16(0), nlsf[0], "expected the initial-root-negative branch to fire")
			for k := 1; k < tc.order; k++ {
				require.GreaterOrEqualf(t, nlsf[k], int16(0), "coefficient %d negative", k)
			}
		})
	}
}

// TestA2NLSFDegenerate exercises the bandwidth-expansion retry path with
// pathological LPC coefficients. Zeros and huge values force the algorithm
// through multiple expansion passes; the output must still be a valid
// increasing NLSF vector.
func TestA2NLSFDegenerate(t *testing.T) {
	cases := []struct {
		name  string
		order int
		fill  func(aQ16 []int32)
	}{
		{"zeros_order10", 10, func(aQ16 []int32) {}},
		{"zeros_order16", 16, func(aQ16 []int32) {}},
		{"huge_order10", 10, func(aQ16 []int32) {
			for i := range aQ16 {
				aQ16[i] = -(1 << 22)
			}
		}},
		{"huge_order16", 16, func(aQ16 []int32) {
			for i := range aQ16 {
				aQ16[i] = -(1 << 22)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nlsf := make([]int16, tc.order)
			aQ16 := make([]int32, tc.order)
			tc.fill(aQ16)

			a2nlsf(nlsf, aQ16, tc.order)

			// Whatever path was taken, the result must be a valid increasing
			// NLSF vector — never NaN-equivalent garbage or negative values.
			for k := range nlsf {
				require.GreaterOrEqual(t, nlsf[k], int16(0), "coefficient %d negative", k)
			}
			for k := 1; k < tc.order; k++ {
				require.Greaterf(t, nlsf[k], nlsf[k-1], "not increasing at %d", k)
			}
		})
	}
}
