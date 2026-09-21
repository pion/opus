// SPDX-FileCopyrightText: 2006-2011 Skype Limited
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

//nolint:gocyclo,gosec,lll,modernize,nestif // Preserve pinned fixed-point operation and branch order.
package silk

// silkFrameReconstructionFixed reproduces the generic fixed-point SILK
// inverse-NSQ path from the pinned libopus silk/decode_core.c. SILK uses this
// arithmetic even when the surrounding libopus build is floating point.
//
// The current Go PLC still owns a floating-point continuation state. Once a
// loss has invalidated this fixed history, callers fall back to the legacy
// reconstruction until the fixed-point PLC state is ported as well.
//
//nolint:cyclop,gocognit,maintidx
func (d *Decoder) silkFrameReconstructionFixed(
	signalType frameSignalType,
	bandwidth Bandwidth,
	subframeCount int,
	dLPC int,
	bQ7 [][]int8,
	pitchLags []int,
	eQ23 []int32,
	ltpScaleQ14 float32,
	wQ2 int16,
	out []float32,
) bool {
	if d.plcLossCount > 0 {
		// The legacy floating-point PLC path cannot advance fixed-point history.
		d.fixedStateValid = false
	}
	if !d.fixedStateValid {
		return false
	}

	subframeLength := d.samplesInSubframe(bandwidth)
	frameLength := subframeLength * subframeCount
	ltpMemoryLength := 4 * subframeLength
	if frameLength > len(d.fixedPCM) || ltpMemoryLength > len(d.fixedSLTP) || len(out) < frameLength {
		return false
	}

	pcm := d.fixedPCM[:frameLength]
	sLTP := d.fixedSLTP[:ltpMemoryLength]
	sLTPQ15 := d.fixedSLTPQ15[:ltpMemoryLength+frameLength]
	resQ14 := d.fixedResQ14[:subframeLength]
	sLPCQ14 := d.fixedSLPCScratch[:maxPredictLPCOrder+subframeLength]
	clear(sLTP)
	clear(sLTPQ15)
	copy(sLPCQ14, d.fixedSLPCQ14[:])

	nlsfInterpolation := wQ2 < 4
	sLTPBufferIndex := ltpMemoryLength
	excitationIndex := 0
	pcmIndex := 0
	for subframe := range subframeCount {
		aQ12Slot := 0
		if subframe > 1 && len(d.aQ12Sets) > 1 {
			aQ12Slot = 1
		}
		aQ12 := d.aQ12Int[aQ12Slot]
		gainQ16 := d.gainQ16Int[subframe]
		gainQ10 := gainQ16 >> 6
		gainAdjustmentQ16 := int32(1 << 16)
		if gainQ16 != d.fixedPrevGainQ16 {
			gainAdjustmentQ16 = div32VarQ(d.fixedPrevGainQ16, gainQ16, 16)
			for i := range maxPredictLPCOrder {
				sLPCQ14[i] = smulww(gainAdjustmentQ16, sLPCQ14[i])
			}
		}
		d.fixedPrevGainQ16 = gainQ16

		effectiveVoiced := signalType == frameSignalTypeVoiced
		var lag int
		if effectiveVoiced {
			lag = pitchLags[subframe]
		}
		if effectiveVoiced {
			if subframe == 0 || (subframe == 2 && nlsfInterpolation) {
				start := ltpMemoryLength - lag - dLPC - ltpOrder/2
				if subframe == 2 {
					copy(d.fixedOutBuf[ltpMemoryLength:], pcm[:2*subframeLength])
				}
				lpcAnalysisFilterFixed(
					sLTP[start:],
					d.fixedOutBuf[start+subframe*subframeLength:],
					aQ12,
					ltpMemoryLength-start,
					dLPC,
				)

				inverseGainQ31 := inverse32VarQ(gainQ16, 47)
				if subframe == 0 {
					inverseGainQ31 = smulwb(inverseGainQ31, int32(ltpScaleQ14)) << 2
				}
				for i := 0; i < lag+ltpOrder/2; i++ {
					sLTPQ15[sLTPBufferIndex-i-1] = smulwb(inverseGainQ31, int32(sLTP[ltpMemoryLength-i-1]))
				}
			} else if gainAdjustmentQ16 != 1<<16 {
				for i := 0; i < lag+ltpOrder/2; i++ {
					index := sLTPBufferIndex - i - 1
					sLTPQ15[index] = smulww(gainAdjustmentQ16, sLTPQ15[index])
				}
			}
		}

		var residual []int32
		if effectiveVoiced {
			predictionIndex := sLTPBufferIndex - lag + ltpOrder/2
			for i := range subframeLength {
				predictionQ13 := int32(2)
				for tap := range ltpOrder {
					coefficientQ14 := int32(bQ7[subframe][tap]) * 128
					predictionQ13 = smlawb(
						predictionQ13,
						sLTPQ15[predictionIndex-tap],
						coefficientQ14,
					)
				}
				predictionIndex++
				resQ14[i] = addLShift32(eQ23[excitationIndex+i]<<6, predictionQ13, 1)
				sLTPQ15[sLTPBufferIndex] = resQ14[i] << 1
				sLTPBufferIndex++
			}
			residual = resQ14
		} else {
			residual = d.fixedResQ14[:subframeLength]
			for i := range subframeLength {
				residual[i] = eQ23[excitationIndex+i] << 6
			}
		}

		for i := range subframeLength {
			predictionQ10 := int32(dLPC >> 1)
			for coefficient := range dLPC {
				predictionQ10 = smlawb(
					predictionQ10,
					sLPCQ14[maxPredictLPCOrder+i-coefficient-1],
					int32(aQ12[coefficient]),
				)
			}
			currentQ14 := addSat32(residual[i], lshiftSat32(predictionQ10, 4))
			sLPCQ14[maxPredictLPCOrder+i] = currentQ14
			pcm[pcmIndex+i] = int16(sat16(rshiftRound32(smulww(currentQ14, gainQ10), 8))) //nolint:gosec // Q14 to PCM.
		}

		copy(sLPCQ14[:maxPredictLPCOrder], sLPCQ14[subframeLength:subframeLength+maxPredictLPCOrder])
		excitationIndex += subframeLength
		pcmIndex += subframeLength
	}

	copy(d.fixedSLPCQ14[:], sLPCQ14[:maxPredictLPCOrder])
	retained := ltpMemoryLength - frameLength
	copy(d.fixedOutBuf[:retained], d.fixedOutBuf[frameLength:frameLength+retained])
	copy(d.fixedOutBuf[retained:retained+frameLength], pcm)
	for i, sample := range pcm {
		out[i] = float32(sample) / 32768
	}

	return true
}

// stereoUnmixFixed reproduces silk_stereo_MS_to_LR() for decoder output that
// is still backed by valid fixed-point channel state.
func (d *Decoder) stereoUnmixFixed(
	mid, side, out []float32,
	prediction0Q13, prediction1Q13 int32,
	bandwidth Bandwidth,
) bool {
	frameLength := len(mid)
	if len(side) != frameLength || len(out) < 2*frameLength || frameLength > maxFrameLength {
		return false
	}

	var midPCM, sidePCM [maxFrameLength + 2]int16
	copy(midPCM[:2], d.fixedStereoMid[:])
	copy(sidePCM[:2], d.fixedStereoSide[:])
	for i := range frameLength {
		midPCM[i+2] = int16(mid[i] * 32768)
		sidePCM[i+2] = int16(side[i] * 32768)
	}
	copy(d.fixedStereoMid[:], midPCM[frameLength:frameLength+2])
	copy(d.fixedStereoSide[:], sidePCM[frameLength:frameLength+2])

	interpolationLength := d.stereoPhaseOneSampleCount(bandwidth)
	denominatorQ16 := int32((1 << 16) / interpolationLength)
	pred0Q13 := d.fixedStereoPredQ13[0]
	pred1Q13 := d.fixedStereoPredQ13[1]
	delta0Q13 := rshiftRound32(smulbb(prediction0Q13-pred0Q13, denominatorQ16), 16)
	delta1Q13 := rshiftRound32(smulbb(prediction1Q13-pred1Q13, denominatorQ16), 16)
	for sample := 0; sample < interpolationLength; sample++ {
		pred0Q13 += delta0Q13
		pred1Q13 += delta1Q13
		stereoPredictFixed(midPCM[:], sidePCM[:], sample, pred0Q13, pred1Q13)
	}
	for sample := interpolationLength; sample < frameLength; sample++ {
		stereoPredictFixed(midPCM[:], sidePCM[:], sample, prediction0Q13, prediction1Q13)
	}
	d.fixedStereoPredQ13[0] = prediction0Q13
	d.fixedStereoPredQ13[1] = prediction1Q13

	for sample := range frameLength {
		midSample := int32(midPCM[sample+1])
		sideSample := int32(sidePCM[sample+1])
		out[2*sample] = float32(sat16(midSample+sideSample)) / 32768
		out[2*sample+1] = float32(sat16(midSample-sideSample)) / 32768
	}

	d.previousStereoWeights = d.fixedStereoPredQ13
	d.previousMidValues[0] = float32(d.fixedStereoMid[0]) / 32768
	d.previousMidValues[1] = float32(d.fixedStereoMid[1]) / 32768
	d.previousSideValue = float32(d.fixedStereoSide[1]) / 32768
	d.wasStereo = true

	return true
}

func stereoPredictFixed(mid, side []int16, sample int, prediction0Q13, prediction1Q13 int32) {
	sum := addLShift32(int32(mid[sample])+int32(mid[sample+2]), int32(mid[sample+1]), 1)
	sum = lshiftOvflw(sum, 9)
	sum = smlawb(int32(side[sample+1])<<8, sum, prediction0Q13)
	sum = smlawb(sum, int32(mid[sample+1])<<11, prediction1Q13)
	side[sample+1] = int16(sat16(rshiftRound32(sum, 8))) //nolint:gosec // Q8 to PCM.
}
