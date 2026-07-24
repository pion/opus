// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// Gain quantization constants from silk/define.h and the derived macros at the
// top of silk/gain_quant.c.
const (
	gainOffsetQ7    = 2090    // OFFSET        = (MIN_QGAIN_DB*128)/6 + 16*128
	gainScaleQ16    = 2251    // SCALE_Q16     = (65536*(N_LEVELS_QGAIN-1)) / (((MAX-MIN)*128)/6)
	gainInvScaleQ16 = 1907825 // INV_SCALE_Q16 = (65536*(((MAX-MIN)*128)/6)) / (N_LEVELS_QGAIN-1) == 0x1D1C71
	gainNLevels     = 64      // N_LEVELS_QGAIN
	gainMaxDelta    = 36      // MAX_DELTA_GAIN_QUANT
	gainMinDelta    = -4      // MIN_DELTA_GAIN_QUANT
	gainMaxLogQ7    = 3967    // 31 in Q7, the upper clamp used by silk_log2lin()
)

// encodeSubframeGains quantizes the per-subframe target gains (Q16), emits the
// indices, and returns the dequantized gains. It is the counterpart of
// Decoder.decodeSubframeQuantizations and produces bit-identical gains.
func (e *Encoder) encodeSubframeGains(
	gainsTargetQ16 []int32,
	signalType frameSignalType,
	subframeCount int,
	isFirstSilkFrameInOpusFrame bool,
) (gainQ16 []float32) {
	conditional := !isFirstSilkFrameInOpusFrame && e.haveEncoded
	indices, gainQ16, _ := quantizeGains(gainsTargetQ16, &e.previousLogGain, subframeCount, conditional)
	e.emitGainIndices(indices, signalType, conditional)
	e.haveEncoded = true

	return gainQ16
}

// quantizeGains is silk_gains_quant: it turns target gains into transmit
// indices and the dequantized gains, updating the running previousLogGain
// (an in/out parameter, same pattern as pitchAnalysisCore's ltpCorr — this
// used to be an *Encoder method keyed on e.previousLogGain, but nothing else
// here needs the rest of *Encoder, so the state is threaded explicitly
// instead of deferring the whole function). For the independent first
// subframe the index is the full 6-bit gain index; for delta subframes it is
// the non-negative transmit index (0..40).
func quantizeGains(
	gainsTargetQ16 []int32,
	previousLogGain *int32,
	subframeCount int,
	conditional bool,
) (indices []int8, gainQ16 []float32, gainQ16Int []int32) {
	indices = make([]int8, subframeCount)
	gainQ16 = make([]float32, subframeCount)
	gainQ16Int = make([]int32, subframeCount)

	for subframeIndex := range subframeCount {
		ind := smulwb(gainScaleQ16, lin2log(gainsTargetQ16[subframeIndex])-gainOffsetQ7)
		if ind < *previousLogGain {
			ind++
		}
		ind = clamp(0, ind, gainNLevels-1)

		if subframeIndex == 0 && !conditional { //nolint:nestif // faithful port of silk_gains_quant.
			ind = clamp(*previousLogGain+gainMinDelta, ind, gainNLevels-1)
			*previousLogGain = ind
			indices[subframeIndex] = int8(ind) //nolint:gosec // G115: ind is in [0,63].
		} else {
			delta := ind - *previousLogGain
			doubleStepThreshold := 2*gainMaxDelta - gainNLevels + *previousLogGain
			if delta > doubleStepThreshold {
				delta = doubleStepThreshold + ((delta - doubleStepThreshold + 1) >> 1)
			}
			delta = clamp(gainMinDelta, delta, gainMaxDelta)
			if delta > doubleStepThreshold {
				*previousLogGain += (delta << 1) - doubleStepThreshold
				if *previousLogGain > gainNLevels-1 {
					*previousLogGain = gainNLevels - 1
				}
			} else {
				*previousLogGain += delta
			}
			indices[subframeIndex] = int8(delta - gainMinDelta) //nolint:gosec // G115: delta is in [gainMinDelta,gainMaxDelta].
		}

		inLogQ7 := (gainInvScaleQ16 * (*previousLogGain) >> 16) + gainOffsetQ7
		inLogQ7 = min(inLogQ7, gainMaxLogQ7)
		i := inLogQ7 >> 7
		f := inLogQ7 & 127
		gain := (1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7)
		gainQ16Int[subframeIndex] = gain
		gainQ16[subframeIndex] = float32(gain)
	}

	return indices, gainQ16, gainQ16Int
}

// emitGainIndices range-encodes the gain indices produced by quantizeGains.
func (e *Encoder) emitGainIndices(indices []int8, signalType frameSignalType, conditional bool) {
	for subframeIndex, index := range indices {
		if subframeIndex == 0 && !conditional {
			msb := uint32(index >> 3)  //nolint:gosec // G115: index is in [0,63].
			lsb := uint32(index & 0x7) //nolint:gosec // G115
			switch signalType {
			case frameSignalTypeInactive:
				e.rangeEncoder.EncodeSymbolWithICDF(icdfIndependentQuantizationGainMSBInactive, msb)
			case frameSignalTypeVoiced:
				e.rangeEncoder.EncodeSymbolWithICDF(icdfIndependentQuantizationGainMSBVoiced, msb)
			case frameSignalTypeUnvoiced:
				e.rangeEncoder.EncodeSymbolWithICDF(icdfIndependentQuantizationGainMSBUnvoiced, msb)
			}
			e.rangeEncoder.EncodeSymbolWithICDF(icdfIndependentQuantizationGainLSB, lsb)
		} else {
			e.rangeEncoder.EncodeSymbolWithICDF(icdfDeltaQuantizationGain, uint32(index)) //nolint:gosec // G115
		}
	}
}
