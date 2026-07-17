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
	// The first subframe gain is coded independently only at the start of a
	// frame; otherwise it is delta coded like the rest.
	conditional := !(isFirstSilkFrameInOpusFrame || !e.haveEncoded)

	gainQ16 = make([]float32, subframeCount)

	for subframeIndex := range subframeCount {
		// Convert to log scale, scale, floor() — silk_gains_quant() step 1.
		ind := smulwb(gainScaleQ16, lin2log(gainsTargetQ16[subframeIndex])-gainOffsetQ7)

		// Round towards previous quantized gain (hysteresis).
		if ind < e.previousLogGain {
			ind++
		}
		ind = clamp(0, ind, gainNLevels-1)

		if subframeIndex == 0 && !conditional {
			// Full (independent) index, limited so it cannot drop more than
			// MIN_DELTA_GAIN_QUANT below the previous index.
			ind = clamp(e.previousLogGain+gainMinDelta, ind, gainNLevels-1)
			e.previousLogGain = ind

			// The 3 MSBs use a signal-type-dependent PDF; the 3 LSBs are uniform.
			msb := uint32(ind >> 3)  //nolint:gosec // G115: ind is in [0,63].
			lsb := uint32(ind & 0x7) //nolint:gosec // G115
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
			// Delta index relative to the previous subframe's gain.
			delta := ind - e.previousLogGain

			// Double the quantization step size for large gain increases so
			// the maximum gain level can still be reached.
			doubleStepThreshold := 2*gainMaxDelta - gainNLevels + e.previousLogGain
			if delta > doubleStepThreshold {
				delta = doubleStepThreshold + ((delta - doubleStepThreshold + 1) >> 1)
			}
			delta = clamp(gainMinDelta, delta, gainMaxDelta)

			// Accumulate the delta into the running index.
			if delta > doubleStepThreshold {
				e.previousLogGain += (delta << 1) - doubleStepThreshold
				if e.previousLogGain > gainNLevels-1 {
					e.previousLogGain = gainNLevels - 1
				}
			} else {
				e.previousLogGain += delta
			}

			// Shift to make the transmitted index non-negative (0..40) and emit
			// it with the delta-gain PDF, as the decoder expects.
			transmitted := uint32(delta - gainMinDelta) //nolint:gosec // G115: delta is in [-4,36].
			e.rangeEncoder.EncodeSymbolWithICDF(icdfDeltaQuantizationGain, transmitted)
		}

		// Dequantize using the same expression as the decoder.
		inLogQ7 := (gainInvScaleQ16 * e.previousLogGain >> 16) + gainOffsetQ7
		if inLogQ7 > gainMaxLogQ7 {
			inLogQ7 = gainMaxLogQ7
		}
		i := inLogQ7 >> 7
		f := inLogQ7 & 127
		gainQ16[subframeIndex] = float32((1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7))
	}

	e.haveEncoded = true

	return gainQ16
}
