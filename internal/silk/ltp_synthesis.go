// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// rewhitenLTPHistory applies the LPC analysis filter. source contains dLPC
// preceding samples followed by len(dst) samples from one contiguous history
// span and does not overlap dst. Taps run newest-to-oldest; the prediction
// error is clamped before gain scaling.
func rewhitenLTPHistory(dst, source []float32, dLPC int, weights [16]float32, scale float32) {
	switch dLPC {
	case 10:
		// NB/MB: ten preceding samples plus the sample being rewhitened.
		for i := range dst {
			history := source[i : i+11]
			value := history[10]
			value -= history[9] * weights[0]
			value -= history[8] * weights[1]
			value -= history[7] * weights[2]
			value -= history[6] * weights[3]
			value -= history[5] * weights[4]
			value -= history[4] * weights[5]
			value -= history[3] * weights[6]
			value -= history[2] * weights[7]
			value -= history[1] * weights[8]
			value -= history[0] * weights[9]
			dst[i] = clampNegativeOneToOne(value) * scale
		}

		return
	case 16:
		// WB: sixteen preceding samples plus the sample being rewhitened.
		for i := range dst {
			history := source[i : i+17]
			value := history[16]
			value -= history[15] * weights[0]
			value -= history[14] * weights[1]
			value -= history[13] * weights[2]
			value -= history[12] * weights[3]
			value -= history[11] * weights[4]
			value -= history[10] * weights[5]
			value -= history[9] * weights[6]
			value -= history[8] * weights[7]
			value -= history[7] * weights[8]
			value -= history[6] * weights[9]
			value -= history[5] * weights[10]
			value -= history[4] * weights[11]
			value -= history[3] * weights[12]
			value -= history[2] * weights[13]
			value -= history[1] * weights[14]
			value -= history[0] * weights[15]
			dst[i] = clampNegativeOneToOne(value) * scale
		}

		return
	}
	// Other predictor orders use a variable-length analysis filter.
	for i := range dst {
		history := source[i : i+dLPC+1]
		value := history[dLPC]
		for k := range dLPC {
			value -= history[dLPC-k-1] * weights[k]
		}
		dst[i] = clampNegativeOneToOne(value) * scale
	}
}

// synthesizeLTPTaps adds five-tap pitch prediction to the excitation in dst.
// source stores each five-sample window oldest-to-newest; taps accumulate
// newest-to-oldest. The slices may overlap, so each result must be stored
// before reading the next sample's taps.
func synthesizeLTPTaps(dst, source []float32, weights [5]float32) {
	for i := range dst {
		history := source[i : i+5]
		value := dst[i]
		value += history[4] * weights[0]
		value += history[3] * weights[1]
		value += history[2] * weights[2]
		value += history[1] * weights[3]
		value += history[0] * weights[4]
		dst[i] = value
	}
}
