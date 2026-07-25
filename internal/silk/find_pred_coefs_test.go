// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLtpAnalysisFilterFLPZeroCoefsPassesThrough(t *testing.T) {
	const (
		nbSubfr     = 2
		subfrLength = 8
		preLength   = 2
		lag         = 10
		// x must have at least lag+ltpOrder/2 samples of history before
		// xBase, since the filter reads back to lagPtr-ltpOrder/2.
		xBase = lag + ltpOrder/2
	)
	signal := make([]float32, xBase+nbSubfr*subfrLength+preLength)
	for i := range signal {
		signal[i] = float32(i + 1)
	}
	b := make([]float32, ltpOrder*nbSubfr) // zero LTP taps: residual == input.
	pitchL := []int{lag, lag}
	invGains := []float32{1, 2}

	ltpRes := make([]float32, nbSubfr*(subfrLength+preLength))
	ltpAnalysisFilterFLP(ltpRes, signal, xBase, b, pitchL, invGains, subfrLength, nbSubfr, preLength)

	for k := range nbSubfr {
		for i := range subfrLength + preLength {
			want := signal[xBase+k*subfrLength+i] * invGains[k]
			assert.InDeltaf(t, want, ltpRes[k*(subfrLength+preLength)+i], 1e-4, "subfr %d sample %d", k, i)
		}
	}
}

func TestResidualEnergyFLPZeroCoefsMatchesRawEnergy(t *testing.T) {
	const (
		order       = 4
		subfrLength = 6
		nbSubfr     = 4
		shift       = order + subfrLength
	)
	lpcInPre := make([]float32, nbSubfr*shift)
	state := uint32(11)
	for i := range lpcInPre {
		state = 1664525*state + 1013904223
		lpcInPre[i] = float32(int32(state>>16)%2000-1000) / 100
	}
	a0 := make([]float32, order) // zero AR coefs: residual == input, unfiltered.
	a1 := make([]float32, order)
	gains := []float32{1, 1, 1, 1}

	nrgs := make([]float32, nbSubfr)
	residualEnergyFLP(nrgs, lpcInPre, a0, a1, gains, subfrLength, nbSubfr, order)

	for half := 0; half < nbSubfr; half += 2 {
		base := half * shift
		want0 := float32(energyFLP(lpcInPre[base+order:], subfrLength))
		want1 := float32(energyFLP(lpcInPre[base+order+shift:], subfrLength))
		assert.InDeltaf(t, want0, nrgs[half], 1e-2, "nrgs[%d]", half)
		//nolint:gosec // G602: half<nbSubfr-1 by loop step 2.
		assert.InDeltaf(t, want1, nrgs[half+1], 1e-2, "nrgs[%d]", half+1)
	}
}

func TestInterpolateNLSF(t *testing.T) {
	x0 := []int16{100, 200, 300}
	x1 := []int16{500, 1000, 1500}
	xi := make([]int16, 3)

	// ifactQ2==0: pure x0.
	interpolateNLSF(xi, x0, x1, 0, 3)
	assert.Equal(t, x0, xi)

	// ifactQ2==4 (max weight per silk_interpolate's own bound): pure x1.
	interpolateNLSF(xi, x0, x1, 4, 3)
	assert.Equal(t, x1, xi)

	// ifactQ2==2: halfway (silk_ADD_RSHIFT with round-toward-negative-infinity
	// via a plain arithmetic right shift, not round-to-nearest).
	interpolateNLSF(xi, x0, x1, 2, 3)
	for i := range xi {
		//nolint:gosec // G115: test fixture values are small.
		want := int16(int32(x0[i]) + ((int32(x1[i]) - int32(x0[i])) >> 1))
		assert.Equal(t, want, xi[i])
	}
}

func TestPredCoefsMinInvGain(t *testing.T) {
	assert.InDelta(t, 1.0/maxPredictionPowerGainAfterReset, predCoefsMinInvGain(true, 5, 0.5), 1e-9)

	got := predCoefsMinInvGain(false, 6.0, 0.5)
	want := float32(math.Pow(2, 6.0/3)) / maxPredictionPowerGain / (0.25 + 0.75*0.5)
	assert.InDelta(t, want, got, 1e-6)
}

// TestFindLPCNLSF exercises both the interpolation-search path (4 subframes,
// not the first frame after reset — matching find_pitch_lags_FLP's caller
// convention) and the no-interpolation path (2 subframes, always skips the
// search since it requires MAX_NB_SUBFR), including nlsfToLPCQ12 (only
// reachable from the interpolation search).
func TestFindLPCNLSF(t *testing.T) {
	const (
		order       = 16
		nbSubfr     = 4
		subfrLength = 80
		blockLen    = subfrLength + order
	)
	lpcInPre := make([]float32, nbSubfr*blockLen)
	for i := range lpcInPre {
		lpcInPre[i] = float32(3000 * math.Sin(2*math.Pi*float64(i)/37))
	}

	enc := NewEncoder()
	enc.firstFrameAfterReset = false
	enc.prevNLSFq = genNLSF(order, 42)

	interpQ2, nlsf := enc.findLPCNLSF(lpcInPre, 1e-4, BandwidthWideband, order, nbSubfr, subfrLength)

	require.Len(t, nlsf, order)
	require.GreaterOrEqual(t, interpQ2, 0)
	require.LessOrEqual(t, interpQ2, 4)
	for k := range nlsf {
		assert.GreaterOrEqualf(t, nlsf[k], int16(0), "coefficient %d", k)
	}
	for k := 1; k < order; k++ {
		assert.Greaterf(t, nlsf[k], nlsf[k-1], "not increasing at %d", k)
	}

	// nbSubfr==2 always takes the no-interpolation path (interpQ2 stays 4)
	// regardless of firstFrameAfterReset/prevNLSFq, since the search requires
	// exactly MAX_NB_SUBFR subframes.
	interpQ2NB, nlsfNB := enc.findLPCNLSF(lpcInPre[:2*blockLen], 1e-4, BandwidthNarrowband, order, 2, subfrLength)
	assert.Equal(t, 4, interpQ2NB)
	require.Len(t, nlsfNB, order)
}
