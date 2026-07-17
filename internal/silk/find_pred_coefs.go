// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

const (
	maxPredictionPowerGain           = 1e4
	maxPredictionPowerGainAfterReset = 1e2
)

// ltpAnalysisFilterFLP forms the LTP residual (silk_LTP_analysis_filter_FLP).
// x is the speech buffer and xBase the index of the first sample to process,
// which must have at least predictLPCOrder + max(pitchL) samples of history
// before it. The output holds nbSubfr blocks of preLength+subfrLength samples,
// each scaled by the subframe inverse gain.
func ltpAnalysisFilterFLP(ltpRes, x []float32, xBase int, b []float32, pitchL []int, invGains []float32, subfrLength, nbSubfr, preLength int) {
	xPtr := xBase
	resIdx := 0
	for k := range nbSubfr {
		lagPtr := xPtr - pitchL[k]
		invGain := invGains[k]
		for i := range subfrLength + preLength {
			v := x[xPtr+i]
			for j := range ltpOrder {
				v -= b[k*ltpOrder+j] * x[lagPtr+ltpOrder/2-j+i]
			}
			ltpRes[resIdx+i] = v * invGain
		}
		resIdx += subfrLength + preLength
		xPtr += subfrLength
	}
}

// residualEnergyFLP measures the per-subframe LPC residual energy of the
// gain-normalized signal, rescaled by the squared gains
// (silk_residual_energy_FLP). lpcInPre holds nbSubfr blocks of order+subfrLength
// samples; a0 and a1 are the quantized LPC for the first and second frame
// halves (equal when NLSF interpolation is inactive).
func residualEnergyFLP(nrgs, lpcInPre, a0, a1, gains []float32, subfrLength, nbSubfr, order int) {
	shift := order + subfrLength
	lpcRes := make([]float32, 2*shift)
	halves := [][]float32{a0, a1}
	for half := 0; half < nbSubfr; half += 2 {
		lpcAnalysisFilterFLP(lpcRes, halves[half/2], lpcInPre[half*shift:], 2*shift, order)
		nrgs[half] = gains[half] * gains[half] * float32(energyFLP(lpcRes[order:], subfrLength))
		nrgs[half+1] = gains[half+1] * gains[half+1] * float32(energyFLP(lpcRes[order+shift:], subfrLength))
	}
}

// interpolateNLSF linearly interpolates two NLSF vectors (silk_interpolate).
// ifactQ2 (0..4) is the weight on x1.
func interpolateNLSF(xi, x0, x1 []int16, ifactQ2, d int) {
	for i := range d {
		xi[i] = int16(int32(x0[i]) + (smulbb(int32(x1[i])-int32(x0[i]), int32(ifactQ2)) >> 2))
	}
}

// findLPCNLSF runs Burg over LPC_in_pre and, for 20 ms frames, searches the
// NLSF interpolation factor with the lowest first-half residual energy
// (silk_find_LPC_FLP). It returns the interpolation index (4 = no
// interpolation) and the NLSFs to quantize.
func (e *Encoder) findLPCNLSF(lpcInPre []float32, minInvGain float32, bandwidth Bandwidth, order, nbSubfr, subfrLength int) (int, []int16) {
	blockLen := subfrLength + order
	interpQ2 := 4
	aFull := make([]float32, order)
	resNrg := burgModifiedFLP(aFull, lpcInPre, minInvGain, blockLen, nbSubfr, order)

	nlsf := make([]int16, order)
	if !e.firstFrameAfterReset && nbSubfr == maxSubframeCount {
		aTmp := make([]float32, order)
		resNrg -= burgModifiedFLP(aTmp, lpcInPre[(maxSubframeCount/2)*blockLen:], minInvGain, blockLen, maxSubframeCount/2, order)
		a2nlsfFLP(nlsf, aTmp, order) // NLSFs of the last 10 ms

		resNrg2nd := float32(math.MaxFloat32)
		nlsf0 := make([]int16, order)
		lpcRes := make([]float32, 2*blockLen)
		aInterp := make([]float32, order)
		for k := 3; k >= 0; k-- {
			interpolateNLSF(nlsf0, e.prevNLSFq, nlsf, k, order)
			aQ12 := nlsfToLPCQ12(nlsf0, bandwidth)
			for i := range order {
				aInterp[i] = float32(aQ12[i]) * (1.0 / 4096.0)
			}
			lpcAnalysisFilterFLP(lpcRes, aInterp, lpcInPre, 2*blockLen, order)
			resInterp := float32(energyFLP(lpcRes[order:], subfrLength)) +
				float32(energyFLP(lpcRes[order+blockLen:], subfrLength))
			if resInterp < resNrg {
				resNrg = resInterp
				interpQ2 = k
			} else if resInterp > resNrg2nd {
				break
			}
			resNrg2nd = resInterp
		}
	}
	if interpQ2 == 4 {
		a2nlsfFLP(nlsf, aFull, order)
	}

	return interpQ2, nlsf
}

// predCoefsMinInvGain returns the minimum inverse prediction gain used to cap
// the short-term predictor (silk_find_pred_coefs_FLP).
func predCoefsMinInvGain(firstFrameAfterReset bool, ltpredCodGain, codingQuality float32) float32 {
	if firstFrameAfterReset {
		return 1.0 / maxPredictionPowerGainAfterReset
	}
	minInvGain := float32(math.Pow(2, float64(ltpredCodGain)/3)) / maxPredictionPowerGain

	return minInvGain / (0.25 + 0.75*codingQuality)
}
