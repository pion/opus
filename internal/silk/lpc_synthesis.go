// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// lpcSynthesisSteadyState uses constant tap indices for SILK's orders 10 and 16.
// historyAndOutput contains dLPC prior samples followed by the current subframe
// and aliases subframeLPC. Each unclipped result feeds the next prediction;
// only out is clamped. Accumulation proceeds from the gain-scaled residual through
// oldest-to-newest history samples.
func lpcSynthesisSteadyState(
	out []float32,
	dLPC int,
	reversedAQ12 [16]float32,
	subframeRes, subframeLPC, historyAndOutput []float32,
	gain float32,
) {
	switch dLPC {
	case 10:
		// Narrowband and mediumband use ten predictor taps.
		for sampleIndex := range out {
			lpcVal := gain * subframeRes[sampleIndex]
			history := historyAndOutput[sampleIndex : sampleIndex+10]
			lpcVal += history[0] * reversedAQ12[0]
			lpcVal += history[1] * reversedAQ12[1]
			lpcVal += history[2] * reversedAQ12[2]
			lpcVal += history[3] * reversedAQ12[3]
			lpcVal += history[4] * reversedAQ12[4]
			lpcVal += history[5] * reversedAQ12[5]
			lpcVal += history[6] * reversedAQ12[6]
			lpcVal += history[7] * reversedAQ12[7]
			lpcVal += history[8] * reversedAQ12[8]
			lpcVal += history[9] * reversedAQ12[9]
			subframeLPC[sampleIndex] = lpcVal
			out[sampleIndex] = clampNegativeOneToOne(lpcVal)
		}

		return
	case 16:
		// Wideband uses all sixteen predictor taps.
		for sampleIndex := range out {
			lpcVal := gain * subframeRes[sampleIndex]
			history := historyAndOutput[sampleIndex : sampleIndex+16]
			lpcVal += history[0] * reversedAQ12[0]
			lpcVal += history[1] * reversedAQ12[1]
			lpcVal += history[2] * reversedAQ12[2]
			lpcVal += history[3] * reversedAQ12[3]
			lpcVal += history[4] * reversedAQ12[4]
			lpcVal += history[5] * reversedAQ12[5]
			lpcVal += history[6] * reversedAQ12[6]
			lpcVal += history[7] * reversedAQ12[7]
			lpcVal += history[8] * reversedAQ12[8]
			lpcVal += history[9] * reversedAQ12[9]
			lpcVal += history[10] * reversedAQ12[10]
			lpcVal += history[11] * reversedAQ12[11]
			lpcVal += history[12] * reversedAQ12[12]
			lpcVal += history[13] * reversedAQ12[13]
			lpcVal += history[14] * reversedAQ12[14]
			lpcVal += history[15] * reversedAQ12[15]
			subframeLPC[sampleIndex] = lpcVal
			out[sampleIndex] = clampNegativeOneToOne(lpcVal)
		}

		return
	}

	// Other predictor orders use a variable-length recurrence.
	for sampleIndex := range out {
		lpcVal := gain * subframeRes[sampleIndex]
		history := historyAndOutput[sampleIndex : sampleIndex+dLPC]
		for coefficientIndex := range dLPC {
			lpcVal += history[coefficientIndex] * reversedAQ12[coefficientIndex]
		}
		subframeLPC[sampleIndex] = lpcVal
		out[sampleIndex] = clampNegativeOneToOne(lpcVal)
	}
}

// lpcSynthesisFirstSubframeTail requires the first dLPC samples to be synthesized
// and dLPC to be 10 or 16. Every predictor tap then lies in this subframe.
// Accumulation runs newest-to-oldest, with unclipped results feeding back.
func lpcSynthesisFirstSubframeTail(
	out []float32,
	dLPC int,
	normalizedAQ12 [16]float32,
	subframeRes, subframeLPC []float32,
	gain float32,
) {
	switch dLPC {
	case 10:
		// NB/MB uses ten warm-up samples.
		for sampleIndex := 10; sampleIndex < len(out); sampleIndex++ {
			lpcVal := gain * subframeRes[sampleIndex]
			history := subframeLPC[sampleIndex-10 : sampleIndex]
			lpcVal += history[9] * normalizedAQ12[0]
			lpcVal += history[8] * normalizedAQ12[1]
			lpcVal += history[7] * normalizedAQ12[2]
			lpcVal += history[6] * normalizedAQ12[3]
			lpcVal += history[5] * normalizedAQ12[4]
			lpcVal += history[4] * normalizedAQ12[5]
			lpcVal += history[3] * normalizedAQ12[6]
			lpcVal += history[2] * normalizedAQ12[7]
			lpcVal += history[1] * normalizedAQ12[8]
			lpcVal += history[0] * normalizedAQ12[9]
			subframeLPC[sampleIndex] = lpcVal
			out[sampleIndex] = clampNegativeOneToOne(lpcVal)
		}

		return
	case 16:
		// WB needs sixteen warm-up samples before all taps are local.
		for sampleIndex := 16; sampleIndex < len(out); sampleIndex++ {
			lpcVal := gain * subframeRes[sampleIndex]
			history := subframeLPC[sampleIndex-16 : sampleIndex]
			lpcVal += history[15] * normalizedAQ12[0]
			lpcVal += history[14] * normalizedAQ12[1]
			lpcVal += history[13] * normalizedAQ12[2]
			lpcVal += history[12] * normalizedAQ12[3]
			lpcVal += history[11] * normalizedAQ12[4]
			lpcVal += history[10] * normalizedAQ12[5]
			lpcVal += history[9] * normalizedAQ12[6]
			lpcVal += history[8] * normalizedAQ12[7]
			lpcVal += history[7] * normalizedAQ12[8]
			lpcVal += history[6] * normalizedAQ12[9]
			lpcVal += history[5] * normalizedAQ12[10]
			lpcVal += history[4] * normalizedAQ12[11]
			lpcVal += history[3] * normalizedAQ12[12]
			lpcVal += history[2] * normalizedAQ12[13]
			lpcVal += history[1] * normalizedAQ12[14]
			lpcVal += history[0] * normalizedAQ12[15]
			subframeLPC[sampleIndex] = lpcVal
			out[sampleIndex] = clampNegativeOneToOne(lpcVal)
		}

		return
	}
}
