// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"slices"
)

const (
	nlsfQuantMaxAmplitude    = 4
	nlsfQuantMaxAmplitudeExt = 10
	nlsfLevelAdjQ10          = 102 // round(NLSF_QUANT_LEVEL_ADJ * 1024)
)

// nlsfStepSizes returns the Q16 quantization step and its Q6 inverse.
func nlsfStepSizes(bandwidth Bandwidth) (qstepQ16, invQstepQ6 int32) {
	if bandwidth == BandwidthWideband {
		return 9830, 427
	}

	return 11796, 356
}

// nlsfWeightQ9 computes the Q9 spectral weight for coefficient k from the
// stage-1 codebook vector, using the same formula as the decoder.
func nlsfWeightQ9(cb1 []uint, order, k int) int32 {
	previous, next := uint(0), uint(256)
	if k != 0 {
		previous = cb1[k-1]
	}
	if k+1 != order {
		next = cb1[k+1]
	}
	w2Q18 := (1024/(cb1[k]-previous) + 1024/(next-cb1[k])) << 16

	i := ilog(int(w2Q18))              //nolint:gosec // G115
	f := int((w2Q18 >> (i - 8)) & 127) //nolint:gosec // G115
	y := 46214
	if i&1 != 0 {
		y = 32768
	}
	y >>= (32 - i) >> 1

	return int32(int16(y + ((213 * f * y) >> 16))) //nolint:gosec // G115
}

// nlsfSecondOperand mirrors the decoder's residual reconstruction term for a
// stage-2 index.
func nlsfSecondOperand(ind, qstepQ16 int32) int32 {
	return (((ind << 10) - int32(sign(int(ind)))*nlsfLevelAdjQ10) * qstepQ16) >> 16
}

// encodeNLSF quantizes and range-encodes the input NLSF vector, returning the
// quantized NLSFs the decoder will reconstruct. It searches every stage-1
// codebook vector and greedily quantizes the stage-2 residual for each,
// keeping the lowest weighted distortion.
func (e *Encoder) encodeNLSF(nlsfQ15 []int16, bandwidth Bandwidth, voiced bool) []int16 {
	stabilizeNLSF(nlsfQ15, len(nlsfQ15), bandwidth)
	index1, indices2, quantized := quantizeNLSF(nlsfQ15, bandwidth)
	e.emitNLSFIndices(index1, indices2, bandwidth, voiced)

	return quantized
}

// quantizeNLSF searches the two-stage NLSF codebooks (silk_NLSF_encode) and
// returns the stage-1 index, stage-2 indices, and reconstructed NLSF vector.
// The input must already be stabilized.
func quantizeNLSF(nlsfQ15 []int16, bandwidth Bandwidth) (int, []int8, []int16) {
	order := len(nlsfQ15)

	cb1Set := codebookNormalizedLSFStageOneNarrowbandOrMediumband
	predSelect := predictionWeightSelectionForNarrowbandAndMediumbandNormalizedLSF
	predTable := predictionWeightForNarrowbandAndMediumbandNormalizedLSF
	if bandwidth == BandwidthWideband {
		cb1Set = codebookNormalizedLSFStageOneWideband
		predSelect = predictionWeightSelectionForWidebandNormalizedLSF
		predTable = predictionWeightForWidebandNormalizedLSF
	}
	qstepQ16, invQstepQ6 := nlsfStepSizes(bandwidth)

	bestIndex1 := 0
	bestIndices2 := make([]int8, order)
	bestNLSF := make([]int16, order)
	bestDistortion := int64(math.MaxInt64)

	indices2 := make([]int8, order)
	weightsQ9 := make([]int32, order)
	resReconQ10 := make([]int16, order)
	candidate := make([]int16, order)

	for index1 := range cb1Set {
		cb1 := cb1Set[index1]
		for k := range order {
			weightsQ9[k] = nlsfWeightQ9(cb1, order, k)
		}

		// Greedily quantize the stage-2 residual backwards.
		prevOut := int32(0)
		for k := order - 1; k >= 0; k-- {
			target := (int32(nlsfQ15[k]) - int32(cb1[k])<<7) * weightsQ9[k] >> 14
			predQ10 := int32(0)
			if k+1 < order {
				predQ10 = (int32(predTable[predSelect[index1][k]][k]) * prevOut) >> 8
			}
			ind := clamp((invQstepQ6*(target-predQ10))>>16, -nlsfQuantMaxAmplitudeExt, nlsfQuantMaxAmplitudeExt-1)
			out0 := nlsfSecondOperand(ind, qstepQ16) + predQ10
			out1 := nlsfSecondOperand(ind+1, qstepQ16) + predQ10
			chosen, chosenOut := ind, out0
			if absInt32(target-out1) < absInt32(target-out0) {
				chosen, chosenOut = ind+1, out1
			}
			indices2[k] = int8(chosen)
			resReconQ10[k] = int16(chosenOut)
			prevOut = int32(resReconQ10[k])
		}

		// Reconstruct and stabilize as the decoder will, then score.
		for k := range order {
			candidate[k] = int16(clamp(0,
				int32((int(cb1[k])<<7)+(int(resReconQ10[k])<<14)/int(weightsQ9[k])), 32767))
		}
		stabilizeNLSF(candidate, order, bandwidth)

		var distortion int64
		for k := range order {
			diff := int64(nlsfQ15[k]) - int64(candidate[k])
			distortion += int64(weightsQ9[k]) * diff * diff
		}
		if distortion < bestDistortion {
			bestDistortion = distortion
			bestIndex1 = index1
			copy(bestIndices2, indices2)
			copy(bestNLSF, candidate)
		}
	}

	return bestIndex1, bestIndices2, bestNLSF
}

// emitNLSFIndices range-encodes the NLSF codebook indices.
func (e *Encoder) emitNLSFIndices(index1 int, indices2 []int8, bandwidth Bandwidth, voiced bool) {
	cb2Select := codebookNormalizedLSFStageTwoIndexNarrowbandOrMediumband
	stageOnePDF := icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandUnvoiced
	if bandwidth == BandwidthWideband {
		cb2Select = codebookNormalizedLSFStageTwoIndexWideband
		stageOnePDF = icdfNormalizedLSFStageOneIndexWidebandUnvoiced
		if voiced {
			stageOnePDF = icdfNormalizedLSFStageOneIndexWidebandVoiced
		}
	} else if voiced {
		stageOnePDF = icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandVoiced
	}

	e.rangeEncoder.EncodeSymbolWithICDF(stageOnePDF, uint32(index1)) //nolint:gosec // G115
	cb2 := cb2Select[index1]
	for k := range indices2 {
		v := int(indices2[k])
		switch {
		case v <= -nlsfQuantMaxAmplitude:
			e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndex[cb2[k]], 0)
			e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndexExtension, uint32(-nlsfQuantMaxAmplitude-v)) //nolint:gosec // G115
		case v >= nlsfQuantMaxAmplitude:
			e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndex[cb2[k]], 2*nlsfQuantMaxAmplitude)
			e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndexExtension, uint32(v-nlsfQuantMaxAmplitude)) //nolint:gosec // G115
		default:
			e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndex[cb2[k]], uint32(v+nlsfQuantMaxAmplitude)) //nolint:gosec // G115
		}
	}
}

// stabilizeNLSF enforces the minimum spacing between consecutive NLSF
// coefficients (RFC 6716 Section 4.2.7.5.4).
func stabilizeNLSF(nlsfQ15 []int16, dLPC int, bandwidth Bandwidth) {
	NDeltaMinQ15 := codebookMinimumSpacingForNormalizedLSCoefficientsNarrowbandAndMediumband
	if bandwidth == BandwidthWideband {
		NDeltaMinQ15 = codebookMinimumSpacingForNormalizedLSCoefficientsWideband
	}

	for adjustment := 0; adjustment <= 19; adjustment++ {
		i := 0
		iValue := int(math.MaxInt)
		for nlsfIndex := 0; nlsfIndex <= len(nlsfQ15); nlsfIndex++ {
			previousNLSF := 0
			currentNLSF := 32768
			if nlsfIndex != 0 {
				previousNLSF = int(nlsfQ15[nlsfIndex-1])
			}
			if nlsfIndex != len(nlsfQ15) {
				currentNLSF = int(nlsfQ15[nlsfIndex])
			}
			spacingValue := currentNLSF - previousNLSF - NDeltaMinQ15[nlsfIndex]
			if spacingValue < iValue {
				i = nlsfIndex
				iValue = spacingValue
			}
		}

		switch {
		case iValue >= 0:
			return
		case i == 0:
			nlsfQ15[0] = int16(NDeltaMinQ15[0]) //nolint:gosec // G115

			continue
		case i == dLPC:
			nlsfQ15[dLPC-1] = int16(32768 - NDeltaMinQ15[dLPC]) //nolint:gosec // G115

			continue
		}

		minCenterQ15 := NDeltaMinQ15[i] >> 1
		for k := 0; k <= i-1; k++ {
			minCenterQ15 += NDeltaMinQ15[k]
		}
		maxCenterQ15 := 32768 - (NDeltaMinQ15[i] >> 1)
		for k := i + 1; k <= dLPC; k++ {
			maxCenterQ15 -= NDeltaMinQ15[k]
		}
		centerFreqQ15 := int(clamp(
			int32(minCenterQ15), //nolint:gosec // G115
			int32((int(nlsfQ15[i-1])+int(nlsfQ15[i])+1)>>1), //nolint:gosec // G115
			int32(maxCenterQ15)), //nolint:gosec // G115
		)
		nlsfQ15[i-1] = int16(centerFreqQ15 - NDeltaMinQ15[i]>>1) //nolint:gosec // G115
		nlsfQ15[i] = nlsfQ15[i-1] + int16(NDeltaMinQ15[i])       //nolint:gosec // G115
	}

	slices.Sort(nlsfQ15)
	for k := 0; k <= dLPC-1; k++ {
		prevNLSF := int16(0)
		if k != 0 {
			prevNLSF = nlsfQ15[k-1]
		}
		nlsfQ15[k] = maxInt16(nlsfQ15[k], saturatingAddInt16(prevNLSF, int16(NDeltaMinQ15[k]))) //nolint:gosec // G115
	}
	for k := dLPC - 1; k >= 0; k-- {
		nextNLSF := 32768
		if k != dLPC-1 {
			nextNLSF = int(nlsfQ15[k+1])
		}
		nlsfQ15[k] = minInt16(nlsfQ15[k], int16(nextNLSF-NDeltaMinQ15[k+1])) //nolint:gosec // G115
	}
}
