// SPDX-FileCopyrightText: 2006-2011 Skype Limited
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

//nolint:gosec,lll // Fixed-point conversions, expressions, and bounded indices match pinned libopus.
package silk

const (
	fixedCNGBufferMaskMax                = 255
	fixedCNGGainSmoothQ16                = 4634
	fixedCNGGainSmoothThresholdQ16       = 46396
	fixedCNGNLSFSmoothQ16                = 16348
	fixedCNGRandomSeed             int32 = 3176576
)

type fixedCNGState struct {
	excitationQ14   [maxFrameLength]int32
	smoothedNLSFQ15 [maxPredictLPCOrder]int16
	synthesisQ14    [maxPredictLPCOrder]int32
	smoothedGainQ16 int32
	randomSeed      int32
	fsKHz           int
}

func (d *Decoder) resetFixedCNG(dLPC, fsKHz int) {
	cng := &d.fixedCNG
	stepQ15 := int32(32767 / (dLPC + 1))
	accumulatorQ15 := int32(0)
	for i := range dLPC {
		accumulatorQ15 += stepQ15
		cng.smoothedNLSFQ15[i] = int16(accumulatorQ15) //nolint:gosec // Bounded by int16 max.
	}
	cng.smoothedGainQ16 = 0
	cng.randomSeed = fixedCNGRandomSeed
	cng.fsKHz = fsKHz
}

// applyFixedCNG mirrors silk_CNG(). It is called after the decoder output
// history and PLC loss count have been updated, but before PLC glue, matching
// silk_decode_frame().
//
//nolint:cyclop,gocognit
func (d *Decoder) applyFixedCNG(
	frame []int16,
	signalType frameSignalType,
	bandwidth Bandwidth,
	subframeCount int,
	nlsfQ15 []int16,
) {
	dLPC := dLPCForBandwidth(bandwidth)
	subframeLength := d.samplesInSubframe(bandwidth)
	fsKHz := subframeLength / 5
	cng := &d.fixedCNG
	if cng.fsKHz != fsKHz {
		d.resetFixedCNG(dLPC, fsKHz)
	}

	if d.plcLossCount == 0 && signalType == frameSignalTypeInactive {
		for i := range dLPC {
			delta := int32(nlsfQ15[i]) - int32(cng.smoothedNLSFQ15[i])
			cng.smoothedNLSFQ15[i] += int16(smulwb(delta, fixedCNGNLSFSmoothQ16)) //nolint:gosec // Convex smoothing stays int16.
		}

		maxGainQ16 := int32(0)
		maxGainSubframe := 0
		for subframe := range subframeCount {
			if d.gainQ16Int[subframe] > maxGainQ16 {
				maxGainQ16 = d.gainQ16Int[subframe]
				maxGainSubframe = subframe
			}
		}
		historyLength := (subframeCount - 1) * subframeLength
		copy(cng.excitationQ14[subframeLength:subframeLength+historyLength], cng.excitationQ14[:historyLength])
		copy(cng.excitationQ14[:subframeLength], d.fixedExcQ14[maxGainSubframe*subframeLength:(maxGainSubframe+1)*subframeLength])

		for subframe := range subframeCount {
			gainQ16 := d.gainQ16Int[subframe]
			cng.smoothedGainQ16 += smulwb(gainQ16-cng.smoothedGainQ16, fixedCNGGainSmoothQ16)
			if smulww(cng.smoothedGainQ16, fixedCNGGainSmoothThresholdQ16) > gainQ16 {
				cng.smoothedGainQ16 = gainQ16
			}
		}
	}

	if d.plcLossCount == 0 {
		clear(cng.synthesisQ14[:dLPC])

		return
	}

	gainQ16 := smulww(int32(d.fixedPLC.randomScaleQ14), d.fixedPLC.previousGainQ16[1])
	if gainQ16 >= 1<<21 || cng.smoothedGainQ16 > 1<<23 {
		gainQ16 = smultt(gainQ16, gainQ16)
		gainQ16 = subLShift32(smultt(cng.smoothedGainQ16, cng.smoothedGainQ16), gainQ16, 5)
		gainQ16 = lshiftOvflw(sqrtApprox(gainQ16), 16)
	} else {
		gainQ16 = smulww(gainQ16, gainQ16)
		gainQ16 = subLShift32(smulww(cng.smoothedGainQ16, cng.smoothedGainQ16), gainQ16, 5)
		gainQ16 = lshiftOvflw(sqrtApprox(gainQ16), 8)
	}
	gainQ10 := gainQ16 >> 6

	var signalQ14 [maxFrameLength + maxPredictLPCOrder]int32
	copy(signalQ14[:maxPredictLPCOrder], cng.synthesisQ14[:])
	mask := fixedCNGBufferMaskMax
	for mask > len(frame) {
		mask >>= 1
	}
	seed := cng.randomSeed
	for i := range frame {
		seed = silkRand(seed)
		index := int((seed >> 24) & int32(mask))
		signalQ14[maxPredictLPCOrder+i] = cng.excitationQ14[index]
	}
	cng.randomSeed = seed

	a32Q17 := d.convertNormalizedLSFsToLPCCoefficients(cng.smoothedNLSFQ15[:dLPC], bandwidth)
	d.limitLPCCoefficientsRange(a32Q17)
	d.limitLPCFilterPredictionGainInto(a32Q17, 0)
	aQ12 := d.aQ12Int[0][:dLPC]
	for i := range frame {
		predictionQ10 := int32(dLPC >> 1)
		for coefficient := range dLPC {
			predictionQ10 = smlawb(
				predictionQ10,
				signalQ14[maxPredictLPCOrder+i-coefficient-1],
				int32(aQ12[coefficient]),
			)
		}
		index := maxPredictLPCOrder + i
		signalQ14[index] = addSat32(signalQ14[index], lshiftSat32(predictionQ10, 4))
		noise := sat16(rshiftRound32(smulww(signalQ14[index], gainQ10), 8))
		frame[i] = int16(sat16(int32(frame[i]) + noise)) //nolint:gosec // Saturated to int16.
	}
	copy(cng.synthesisQ14[:], signalQ14[len(frame):len(frame)+maxPredictLPCOrder])
}

func smultt(a, b int32) int32 {
	return (a >> 16) * (b >> 16)
}

func subLShift32(a, b int32, shift uint) int32 {
	return sub32Ovflw(a, lshiftOvflw(b, shift))
}
