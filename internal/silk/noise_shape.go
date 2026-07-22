// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// Noise-shaping analysis primitives. The main silk_noise_shape_analysis_FLP
// orchestration (which populates the encoder control struct) is wired up with
// the rest of the frame encoder.

const maxShapeLPCOrder = 24

// sigmoid returns 1/(1+exp(-x)) (silk_sigmoid).
func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(float64(-x))))
}

// warpedAutocorrelationFLP correlates the input after a chain of first-order
// all-pass warping sections (silk_warped_autocorrelation_FLP). order must be
// even; corr receives order+1 taps. warping 0 gives the regular autocorrelation.
func warpedAutocorrelationFLP(corr, input []float32, warping float32, length, order int) {
	var state, c [maxShapeLPCOrder + 1]float64 //nolint:varnamelen // c is the correlation accumulator.
	w := float64(warping)
	for n := range length {
		tmp1 := float64(input[n])
		for i := 0; i < order; i += 2 {
			tmp2 := state[i] + w*state[i+1] - w*tmp1 //nolint:gosec // G602: order is even and < maxShapeLPCOrder.
			state[i] = tmp1
			c[i] += state[0] * tmp1
			tmp1 = state[i+1] + w*state[i+2] - w*tmp2 //nolint:gosec // G602: order is even and < maxShapeLPCOrder.
			state[i+1] = tmp2                         //nolint:gosec // G602: order is even and < maxShapeLPCOrder.
			c[i+1] += state[0] * tmp2                 //nolint:gosec // G602: order is even and < maxShapeLPCOrder.
		}
		state[order] = tmp1
		c[order] += state[0] * tmp1
	}
	for i := 0; i < order+1; i++ {
		corr[i] = float32(c[i]) //nolint:gosec // G602: i <= order.
	}
}
