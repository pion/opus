// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// Floating-point LPC and signal-analysis primitives shared by the pitch and
// LPC analysis stages (silk/float/*_FLP.c).

// maxLPCOrder matches libopus's SILK_MAX_ORDER_LPC (SigProc_FIX.h), which
// covers hybrid/SWB orders this port doesn't implement yet — this encoder
// only ever requests order 10 (NB/MB) or 16 (WB).
const maxLPCOrder = 24

// innerProductFLP accumulates a dot product in double precision.
func innerProductFLP(a, b []float32, n int) float64 {
	var acc float64
	for i := range n {
		acc += float64(a[i]) * float64(b[i])
	}

	return acc
}

// autocorrelationFLP computes the first correlationCount autocorrelation taps.
func autocorrelationFLP(results, inputData []float32, inputDataSize, correlationCount int) {
	if correlationCount > inputDataSize {
		correlationCount = inputDataSize
	}
	for i := range correlationCount {
		results[i] = float32(innerProductFLP(inputData, inputData[i:], inputDataSize-i))
	}
}

// schurFLP computes reflection coefficients from an autocorrelation sequence
// and returns the residual energy (silk_schur_FLP).
func schurFLP(reflCoef, autoCorr []float32, order int) float32 {
	var c [maxLPCOrder + 1][2]float64 //nolint:varnamelen // c is the correlation work matrix, as in the C reference.
	for k := 0; k <= order; k++ {
		c[k][0] = float64(autoCorr[k])
		c[k][1] = float64(autoCorr[k])
	}
	for k := range order {
		rcTmp := -c[k+1][0] / math.Max(c[0][1], 1e-9)
		reflCoef[k] = float32(rcTmp)
		for n := range order - k {
			ctmp1 := c[n+k+1][0]
			ctmp2 := c[n][1]
			c[n+k+1][0] = ctmp1 + ctmp2*rcTmp
			c[n][1] = ctmp2 + ctmp1*rcTmp
		}
	}

	return float32(c[0][1])
}

// k2aFLP converts reflection coefficients to LPC prediction coefficients.
func k2aFLP(a, rc []float32, order int) {
	for k := range order {
		rck := rc[k] //nolint:gosec // G602: k < order <= maxLPCOrder.
		for n := range (k + 1) >> 1 {
			tmp1 := a[n] //nolint:gosec // G602: indices bounded by k < order.
			tmp2 := a[k-n-1]
			a[n] = tmp1 + tmp2*rck //nolint:gosec // G602: indices bounded by k < order.
			a[k-n-1] = tmp2 + tmp1*rck
		}
		a[k] = -rck //nolint:gosec // G602: k < order <= maxLPCOrder.
	}
}

// bwexpanderFLP applies bandwidth expansion (chirp) to an AR filter.
func bwexpanderFLP(ar []float32, d int, chirp float32) {
	cfac := chirp
	for i := range d - 1 {
		ar[i] *= cfac
		cfac *= chirp
	}
	ar[d-1] *= cfac
}

// applySineWindowFLP multiplies px by a sine window. winType 1 starts at 0
// (rising edge); winType 2 starts at 1 (falling edge). length must be a
// multiple of 4.
func applySineWindowFLP(pxWin, px []float32, winType, length int) {
	freq := float32(math.Pi) / float32(length+1)
	c := 2.0 - freq*freq //nolint:varnamelen // c is the recurrence coefficient, as in the C reference.

	var s0, s1 float32
	if winType < 2 {
		s0 = 0.0
		s1 = freq
	} else {
		s0 = 1.0
		s1 = 0.5 * c
	}

	for k := 0; k < length; k += 4 {
		pxWin[k+0] = px[k+0] * 0.5 * (s0 + s1)
		pxWin[k+1] = px[k+1] * s1 //nolint:gosec // G602: length is a multiple of 4.
		s0 = c*s1 - s0
		pxWin[k+2] = px[k+2] * 0.5 * (s1 + s0) //nolint:gosec // G602: length is a multiple of 4.
		pxWin[k+3] = px[k+3] * s0              //nolint:gosec // G602: length is a multiple of 4.
		s1 = c*s0 - s1
	}
}

// lpcAnalysisFilterFLP computes the LPC residual r = s - predicted(s). The
// first order samples of r are set to zero (silk_LPC_analysis_filter_FLP).
func lpcAnalysisFilterFLP(rLPC, predCoef, s []float32, length, order int) {
	for ix := order; ix < length; ix++ {
		var pred float32
		for j := range order {
			pred += s[ix-1-j] * predCoef[j]
		}
		rLPC[ix] = s[ix] - pred
	}
	for i := range order {
		rLPC[i] = 0
	}
}
