// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

// decoderCELTLPC preserves the generic float32 Levinson-Durbin operation
// boundaries. The shared encoder helper remains free to use contracted math.
func decoderCELTLPC(autocorrelation []float32, order int, coefficients []float32) []float32 {
	coefficients = coefficients[:order]
	clear(coefficients)
	if autocorrelation[0] <= 1e-10 {
		return coefficients
	}

	errorEnergy := autocorrelation[0]
	for i := range order {
		var reflectionNumerator float32
		for j := range i {
			reflectionNumerator += decoderRoundedProduct(coefficients[j], autocorrelation[i-j])
		}
		reflectionNumerator += autocorrelation[i+1]
		reflection := -reflectionNumerator / errorEnergy
		coefficients[i] = reflection
		for j := range (i + 1) >> 1 {
			left, right := coefficients[j], coefficients[i-1-j]
			coefficients[j] = left + decoderRoundedProduct(reflection, right)
			coefficients[i-1-j] = right + decoderRoundedProduct(reflection, left)
		}
		reflectionSquared := decoderRoundedProduct(reflection, reflection)
		errorEnergy -= decoderRoundedProduct(reflectionSquared, errorEnergy)
		if errorEnergy <= 0.001*autocorrelation[0] {
			break
		}
	}

	return coefficients
}
