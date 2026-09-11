// SPDX-FileCopyrightText: 2006-2011 Skype Limited
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

//nolint:cyclop,gosec,lll,nestif // Preserve pinned fixed-point PLC operation and branch order.
package silk

import (
	"math"
	"math/bits"
)

const (
	fixedPLCBandwidthExpansionQ16 = 64881
	fixedPLCPitchGainMinQ14       = 11469
	fixedPLCPitchGainMaxQ14       = 15565
	fixedPLCPitchDriftQ16         = 655
	fixedPLCRandomBufferSize      = 128
	fixedPLCMinimumLPCOrder       = 10
)

type fixedPLCState struct {
	pitchLQ8        int32
	ltpCoefficients [ltpOrder]int16
	previousLPCQ12  [maxPredictLPCOrder]int16
	lastFrameLost   bool
	randomSeed      int32
	randomScaleQ14  int16
	concealedEnergy int32
	concealedShift  int
	previousLTPQ14  int16
	previousGainQ16 [2]int32
	fsKHz           int
	subframeCount   int
	subframeLength  int
}

func (d *Decoder) resetFixedPLC(frameLength, subframeCount, subframeLength, fsKHz int) {
	d.fixedPLC = fixedPLCState{
		pitchLQ8:        int32(frameLength << 7), //nolint:gosec // Bounded SILK frame length.
		previousGainQ16: [2]int32{1 << 16, 1 << 16},
		fsKHz:           fsKHz,
		subframeCount:   subframeCount,
		subframeLength:  subframeLength,
	}
}

// updateFixedPLC mirrors silk_PLC_update() after an accepted frame.
func (d *Decoder) updateFixedPLC(
	signalType frameSignalType,
	subframeCount int,
	bandwidth Bandwidth,
	pitchLags []int,
	bQ7 [][]int8,
	ltpScaleQ14 int16,
) {
	subframeLength := d.samplesInSubframe(bandwidth)
	frameLength := subframeCount * subframeLength
	fsKHz := subframeLength / 5
	if d.fixedPLC.fsKHz != fsKHz {
		d.resetFixedPLC(frameLength, subframeCount, subframeLength, fsKHz)
	}

	ltpGainQ14 := int32(0)
	if signalType == frameSignalTypeVoiced {
		for offset := 0; offset*subframeLength < pitchLags[subframeCount-1]; offset++ {
			if offset == subframeCount {
				break
			}
			subframe := subframeCount - 1 - offset
			candidate := int32(0)
			for tap := range ltpOrder {
				candidate += int32(bQ7[subframe][tap]) * 128
			}
			if candidate > ltpGainQ14 {
				ltpGainQ14 = candidate
				d.fixedPLC.pitchLQ8 = int32(pitchLags[subframe] << 8) //nolint:gosec // Bounded pitch.
			}
		}
		clear(d.fixedPLC.ltpCoefficients[:])
		d.fixedPLC.ltpCoefficients[ltpOrder/2] = int16(ltpGainQ14) //nolint:gosec // Limited immediately below.
		if ltpGainQ14 < fixedPLCPitchGainMinQ14 {
			scaleQ10 := (fixedPLCPitchGainMinQ14 << 10) / max(ltpGainQ14, 1)
			for i, coefficient := range d.fixedPLC.ltpCoefficients {
				d.fixedPLC.ltpCoefficients[i] = int16((int32(coefficient) * scaleQ10) >> 10) //nolint:gosec
			}
		} else if ltpGainQ14 > fixedPLCPitchGainMaxQ14 {
			scaleQ14 := (fixedPLCPitchGainMaxQ14 << 14) / max(ltpGainQ14, 1)
			for i, coefficient := range d.fixedPLC.ltpCoefficients {
				d.fixedPLC.ltpCoefficients[i] = int16((int32(coefficient) * scaleQ14) >> 14) //nolint:gosec
			}
		}
	} else {
		d.fixedPLC.pitchLQ8 = int32(fsKHz * 18 << 8) //nolint:gosec // Bounded SILK rate.
		clear(d.fixedPLC.ltpCoefficients[:])
	}

	coefficientSlot := 0
	if len(d.aQ12Sets) > 1 {
		coefficientSlot = 1
	}
	copy(d.fixedPLC.previousLPCQ12[:], d.aQ12Int[coefficientSlot])
	if signalType == frameSignalTypeVoiced {
		d.fixedPLC.previousLTPQ14 = ltpScaleQ14
	} else {
		d.fixedPLC.previousLTPQ14 = 0
	}
	d.fixedPLC.previousGainQ16[0] = d.gainQ16Int[subframeCount-2]
	d.fixedPLC.previousGainQ16[1] = d.gainQ16Int[subframeCount-1]
	d.fixedPLC.subframeCount = subframeCount
	d.fixedPLC.subframeLength = subframeLength
}

// concealFrameFixed mirrors the non-neural silk_PLC_conceal() path.
//
//nolint:cyclop,gocognit,maintidx
func (d *Decoder) concealFrameFixed(out []float32, bandwidth Bandwidth) bool {
	if !d.fixedStateValid || !d.haveDecoded {
		return false
	}

	subframeLength := d.samplesInSubframe(bandwidth)
	frameLength := len(out)
	subframeCount := frameLength / subframeLength
	ltpMemoryLength := 4 * subframeLength
	fsKHz := subframeLength / 5
	if subframeLength == 0 || frameLength == 0 || frameLength > maxFrameLength ||
		frameLength%subframeLength != 0 || ltpMemoryLength > len(d.fixedSLTP) {
		return false
	}
	if d.fixedPLC.fsKHz != fsKHz {
		d.resetFixedPLC(frameLength, subframeCount, subframeLength, fsKHz)
	}

	plc := &d.fixedPLC
	if d.fixedFirstFrame {
		clear(plc.previousLPCQ12[:])
	}
	previousGainQ10 := [2]int32{plc.previousGainQ16[0] >> 6, plc.previousGainQ16[1] >> 6}
	energy1, shift1, energy2, shift2 := fixedPLCEnergy(
		d.fixedExcQ14[:], previousGainQ10, subframeLength, plc.subframeCount,
	)
	randomStart := max(0, plc.subframeCount*plc.subframeLength-fixedPLCRandomBufferSize)
	if energy1>>shift2 < energy2>>shift1 {
		randomStart = max(0, (plc.subframeCount-1)*plc.subframeLength-fixedPLCRandomBufferSize)
	}
	randomSource := d.fixedExcQ14[randomStart:]

	harmonicGainQ15 := int32(32440)
	if d.plcLossCount > 0 {
		harmonicGainQ15 = 31130
	}
	randomGainQ15 := int32(32440)
	if d.isPreviousFrameVoiced {
		randomGainQ15 = 31130
		if d.plcLossCount > 0 {
			randomGainQ15 = 26214
		}
	} else if d.plcLossCount > 0 {
		randomGainQ15 = 29491
	}

	bwexpandFixed16(plc.previousLPCQ12[:], fixedPLCBandwidthExpansionQ16)
	randomScaleQ14 := plc.randomScaleQ14
	if d.plcLossCount == 0 {
		randomScaleQ14 = 1 << 14
		if d.isPreviousFrameVoiced {
			for _, coefficient := range plc.ltpCoefficients {
				randomScaleQ14 -= coefficient
			}
			randomScaleQ14 = maxInt16(3277, randomScaleQ14)
			randomScaleQ14 = int16((int32(randomScaleQ14) * int32(plc.previousLTPQ14)) >> 14) //nolint:gosec
		} else {
			inverseGainQ30 := lpcInversePredictionGain(plc.previousLPCQ12[:dLPCForBandwidth(bandwidth)])
			downscaleQ30 := min(int32(1<<27), inverseGainQ30)
			downscaleQ30 = max(int32(1<<22), downscaleQ30) << 3
			randomGainQ15 = smulwb(downscaleQ30, randomGainQ15) >> 14
		}
	}

	randomSeed := plc.randomSeed
	lag := int(rshiftRound32(plc.pitchLQ8, 8))
	sLTPQ14 := d.fixedSLTPQ15[:ltpMemoryLength+frameLength]
	clear(sLTPQ14)
	rewhitened := d.fixedSLTP[:ltpMemoryLength]
	clear(rewhitened)
	dLPC := dLPCForBandwidth(bandwidth)
	start := ltpMemoryLength - lag - dLPC - ltpOrder/2
	if start <= 0 {
		return false
	}
	lpcAnalysisFilterFixed(rewhitened[start:], d.fixedOutBuf[start:], plc.previousLPCQ12[:dLPC], ltpMemoryLength-start, dLPC)
	inverseGainQ30 := inverse32VarQ(plc.previousGainQ16[1], 46)
	inverseGainQ30 = min(inverseGainQ30, math.MaxInt32>>1)
	for i := start + dLPC; i < ltpMemoryLength; i++ {
		sLTPQ14[i] = smulwb(inverseGainQ30, int32(rewhitened[i]))
	}

	sLTPIndex := ltpMemoryLength
	for range subframeCount {
		predictionIndex := sLTPIndex - lag + ltpOrder/2
		for range subframeLength {
			predictionQ12 := int32(2)
			for tap := range ltpOrder {
				predictionQ12 = smlawb(predictionQ12, sLTPQ14[predictionIndex-tap], int32(plc.ltpCoefficients[tap]))
			}
			predictionIndex++
			randomSeed = silkRand(randomSeed)
			randomIndex := int((randomSeed >> 25) & (fixedPLCRandomBufferSize - 1))
			sLTPQ14[sLTPIndex] = lshiftOvflw(smlawb(predictionQ12, randomSource[randomIndex], int32(randomScaleQ14)), 2)
			sLTPIndex++
		}
		for tap, coefficient := range plc.ltpCoefficients {
			plc.ltpCoefficients[tap] = int16((harmonicGainQ15 * int32(coefficient)) >> 15) //nolint:gosec
		}
		randomScaleQ14 = int16((int32(randomScaleQ14) * randomGainQ15) >> 15) //nolint:gosec
		plc.pitchLQ8 = smlawb(plc.pitchLQ8, plc.pitchLQ8, fixedPLCPitchDriftQ16)
		plc.pitchLQ8 = min(plc.pitchLQ8, int32(18*fsKHz<<8)) //nolint:gosec
		lag = int(rshiftRound32(plc.pitchLQ8, 8))
	}

	lpcBase := ltpMemoryLength - maxPredictLPCOrder
	copy(sLTPQ14[lpcBase:ltpMemoryLength], d.fixedSLPCQ14[:])
	pcm := d.fixedPCM[:frameLength]
	for i := range frameLength {
		predictionQ10 := int32(dLPC >> 1)
		for coefficient := range dLPC {
			predictionQ10 = smlawb(predictionQ10, sLTPQ14[ltpMemoryLength+i-coefficient-1], int32(plc.previousLPCQ12[coefficient]))
		}
		sLTPQ14[ltpMemoryLength+i] = addSat32(sLTPQ14[ltpMemoryLength+i], lshiftSat32(predictionQ10, 4))
		pcm[i] = int16(sat16(rshiftRound32(smulww(sLTPQ14[ltpMemoryLength+i], previousGainQ10[1]), 8))) //nolint:gosec
	}
	copy(d.fixedSLPCQ14[:], sLTPQ14[ltpMemoryLength+frameLength-maxPredictLPCOrder:ltpMemoryLength+frameLength])
	plc.randomSeed = randomSeed
	plc.randomScaleQ14 = randomScaleQ14
	d.previousLag = lag

	retained := ltpMemoryLength - frameLength
	copy(d.fixedOutBuf[:retained], d.fixedOutBuf[frameLength:frameLength+retained])
	copy(d.fixedOutBuf[retained:retained+frameLength], pcm)
	d.plcLossCount++
	d.applyFixedCNG(pcm, frameSignalTypeInactive, bandwidth, subframeCount, nil)
	d.glueFixedPLC(pcm)
	for i, sample := range pcm {
		out[i] = float32(sample) / 32768
	}
	d.saveFinalOutValues(out)

	return true
}

func fixedPLCEnergy(
	excitation []int32,
	previousGainQ10 [2]int32,
	subframeLength, subframeCount int,
) (energy1 int32, shift1 int, energy2 int32, shift2 int) {
	var scaled [2 * maxSubFrameLength]int16
	for block := range 2 {
		start := (block + subframeCount - 2) * subframeLength
		for i := range subframeLength {
			scaled[block*subframeLength+i] = int16(sat16(smulww(excitation[start+i], previousGainQ10[block]) >> 8)) //nolint:gosec
		}
	}
	energy1, shift1 = sumSqrShift(scaled[:subframeLength])
	energy2, shift2 = sumSqrShift(scaled[subframeLength : 2*subframeLength])

	return
}

func bwexpandFixed16(coefficients []int16, chirpQ16 int32) {
	chirpDeltaQ16 := chirpQ16 - 65536
	for i := 0; i < len(coefficients)-1; i++ {
		coefficients[i] = int16(rshiftRound32(chirpQ16*int32(coefficients[i]), 16)) //nolint:gosec
		chirpQ16 += rshiftRound32(chirpQ16*chirpDeltaQ16, 16)
	}
	coefficients[len(coefficients)-1] = int16(rshiftRound32(chirpQ16*int32(coefficients[len(coefficients)-1]), 16)) //nolint:gosec
}

func dLPCForBandwidth(bandwidth Bandwidth) int {
	if bandwidth == BandwidthWideband {
		return maxPredictLPCOrder
	}

	return fixedPLCMinimumLPCOrder
}

func sumSqrShift(samples []int16) (int32, int) {
	shift := 31 - bits.LeadingZeros32(uint32(len(samples)))
	energy := uint32(len(samples))
	for i := 0; i+1 < len(samples); i += 2 {
		pair := uint32(smulbb(int32(samples[i]), int32(samples[i])))
		pair += uint32(smulbb(int32(samples[i+1]), int32(samples[i+1])))
		energy += pair >> shift
	}
	if len(samples)&1 != 0 {
		last := samples[len(samples)-1]
		energy += uint32(smulbb(int32(last), int32(last))) >> shift
	}
	shift = max(0, shift+3-bits.LeadingZeros32(energy))
	energy = 0
	for i := 0; i+1 < len(samples); i += 2 {
		pair := uint32(smulbb(int32(samples[i]), int32(samples[i])))
		pair += uint32(smulbb(int32(samples[i+1]), int32(samples[i+1])))
		energy += pair >> shift
	}
	if len(samples)&1 != 0 {
		last := samples[len(samples)-1]
		energy += uint32(smulbb(int32(last), int32(last))) >> shift
	}

	return int32(energy), shift
}

func (d *Decoder) glueFixedPLC(frame []int16) {
	plc := &d.fixedPLC
	if d.plcLossCount > 0 {
		plc.concealedEnergy, plc.concealedShift = sumSqrShift(frame)
		plc.lastFrameLost = true

		return
	}
	if !plc.lastFrameLost {
		return
	}
	energy, energyShift := sumSqrShift(frame)
	if energyShift > plc.concealedShift {
		plc.concealedEnergy >>= energyShift - plc.concealedShift
	} else if energyShift < plc.concealedShift {
		energy >>= plc.concealedShift - energyShift
	}
	if energy > plc.concealedEnergy {
		leading := bits.LeadingZeros32(uint32(plc.concealedEnergy)) - 1
		plc.concealedEnergy <<= leading
		energy >>= max(24-leading, 0)
		fractionQ24 := plc.concealedEnergy / max(energy, 1)
		gainQ16 := sqrtApprox(fractionQ24) << 4
		slopeQ16 := (((int32(1) << 16) - gainQ16) / int32(len(frame))) << 2 //nolint:gosec // Bounded frame.
		for i, sample := range frame {
			frame[i] = int16(smulwb(gainQ16, int32(sample))) //nolint:gosec
			gainQ16 += slopeQ16
			if gainQ16 > 1<<16 {
				break
			}
		}
	}
	plc.lastFrameLost = false
}
