// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

const findLPCCondFac = 1e-5

// findLPC estimates LPC coefficients with Burg and converts them to NLSFs
// (silk_find_LPC_FLP). The optional NLSF-interpolation search is deferred; the
// interpolation index stays at 4 (no interpolation). x holds nb_subfr
// subframes of subfrLength samples each (including order preceding samples).
func findLPC(nlsfQ15 []int16, x []float32, minInvGain float32, subfrLength, nbSubfr, order int) {
	a := make([]float32, order)
	burgModifiedFLP(a, x, minInvGain, subfrLength, nbSubfr, order)
	a2nlsfFLP(nlsfQ15, a, order)
}

// burgModifiedFLP estimates LPC coefficients with Burg's method, stacked over
// nb_subfr subframes, limiting the prediction gain to 1/minInvGain. It writes
// the order coefficients into a and returns the residual energy
// (silk_burg_modified_FLP).
func burgModifiedFLP(a, x []float32, minInvGain float32, subfrLength, nbSubfr, order int) float32 {
	var cFirstRow, cLastRow [maxLPCOrder]float64
	var cAf, cAb [maxLPCOrder + 1]float64
	var af [maxLPCOrder]float64

	c0 := energyFLP(x, nbSubfr*subfrLength)
	for s := range nbSubfr {
		xPtr := x[s*subfrLength:]
		for n := 1; n < order+1; n++ {
			cFirstRow[n-1] += innerProductFLP(xPtr, xPtr[n:], subfrLength-n)
		}
	}
	copy(cLastRow[:], cFirstRow[:])

	cAb[0] = c0 + findLPCCondFac*c0 + 1e-9
	cAf[0] = cAb[0]
	invGain := 1.0
	reachedMaxGain := false

	var n int
	for n = 0; n < order; n++ {
		for s := range nbSubfr {
			xPtr := x[s*subfrLength:]
			tmp1 := float64(xPtr[n])
			tmp2 := float64(xPtr[subfrLength-n-1])
			for k := range n {
				cFirstRow[k] -= float64(xPtr[n]) * float64(xPtr[n-k-1])
				cLastRow[k] -= float64(xPtr[subfrLength-n-1]) * float64(xPtr[subfrLength-n+k])
				atmp := af[k]
				tmp1 += float64(xPtr[n-k-1]) * atmp
				tmp2 += float64(xPtr[subfrLength-n+k]) * atmp
			}
			for k := 0; k <= n; k++ {
				cAf[k] -= tmp1 * float64(xPtr[n-k])
				cAb[k] -= tmp2 * float64(xPtr[subfrLength-n+k-1])
			}
		}

		tmp1 := cFirstRow[n]
		tmp2 := cLastRow[n]
		for k := range n {
			atmp := af[k]
			tmp1 += cLastRow[n-k-1] * atmp
			tmp2 += cFirstRow[n-k-1] * atmp
		}
		cAf[n+1] = tmp1
		cAb[n+1] = tmp2

		num := cAb[n+1]
		nrgB := cAb[0]
		nrgF := cAf[0]
		for k := range n {
			atmp := af[k]
			num += cAb[n-k] * atmp
			nrgB += cAb[k+1] * atmp
			nrgF += cAf[k+1] * atmp
		}

		rc := -2.0 * num / (nrgF + nrgB)

		gain := invGain * (1.0 - rc*rc)
		if gain <= float64(minInvGain) {
			rc = math.Sqrt(1.0 - float64(minInvGain)/invGain)
			if num > 0 {
				rc = -rc
			}
			invGain = float64(minInvGain)
			reachedMaxGain = true
		} else {
			invGain = gain
		}

		for k := range (n + 1) >> 1 {
			t1 := af[k]
			t2 := af[n-k-1]
			af[k] = t1 + rc*t2
			af[n-k-1] = t2 + rc*t1
		}
		af[n] = rc

		if reachedMaxGain {
			for k := n + 1; k < order; k++ {
				af[k] = 0.0
			}

			break
		}

		for k := 0; k <= n+1; k++ {
			t1 := cAf[k]
			cAf[k] += rc * cAb[n-k+1]
			cAb[n-k+1] += rc * t1
		}
	}

	var residualEnergy float64
	if reachedMaxGain {
		for k := range order {
			a[k] = float32(-af[k])
		}
		for s := range nbSubfr {
			c0 -= energyFLP(x[s*subfrLength:], order)
		}
		residualEnergy = c0 * invGain
	} else {
		residualEnergy = cAf[0]
		sumSq := 1.0
		for k := range order {
			atmp := af[k]
			residualEnergy += cAf[k+1] * atmp
			sumSq += atmp * atmp
			a[k] = float32(-atmp)
		}
		residualEnergy -= findLPCCondFac * c0 * sumSq
	}

	return float32(residualEnergy)
}
