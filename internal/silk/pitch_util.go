// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// Helpers used by the pitch analysis core: 2:1 downsampler, float/short
// conversions, energy, cross-correlation, and index-tracking insertion sorts.

const (
	resamplerDown2Coef0 = 9872
	resamplerDown2Coef1 = 39809 - 65536 // -25727
)

// resamplerDown2 halves the sample rate with a second-order all-pass filter
// (silk_resampler_down2). out must have len(in)/2 samples; state persists
// across calls.
func resamplerDown2(state *[2]int32, out, in []int16) {
	for k := range len(in) >> 1 { //nolint:varnamelen // k indexes the sample pair.
		in32 := int32(in[2*k]) << 10
		y := in32 - state[0]
		x := smlawb(y, y, resamplerDown2Coef1)
		out32 := state[0] + x
		state[0] = in32 + x

		in32 = int32(in[2*k+1]) << 10
		y = in32 - state[1]
		x = smulwb(y, resamplerDown2Coef0)
		out32 = out32 + state[1] + x
		state[1] = in32 + x

		out[k] = int16(sat16(rshiftRound32(out32, 11))) //nolint:gosec // G115
	}
}

// float2ShortArray rounds and saturates float samples to int16.
func float2ShortArray(out []int16, in []float32) {
	for i := range out {
		out[i] = int16(sat16(int32(math.RoundToEven(float64(in[i]))))) //nolint:gosec // G115
	}
}

// short2FloatArray widens int16 samples to float.
func short2FloatArray(out []float32, in []int16) {
	for i := range out {
		out[i] = float32(in[i])
	}
}

// energyFLP returns the signal energy in double precision.
func energyFLP(data []float32, n int) float64 {
	var result float64
	for i := range n {
		result += float64(data[i]) * float64(data[i])
	}

	return result
}

// pitchXcorr computes xcorr[j] = <x, y[j:]> for j in [0, maxPitch).
func pitchXcorr(x, y, xcorr []float32, length, maxPitch int) {
	for j := range maxPitch {
		xcorr[j] = float32(innerProductFLP(x, y[j:], length))
	}
}

// insertionSortDecreasingFLP sorts the first K of L values into decreasing
// order, tracking their original indices (silk_insertion_sort_decreasing_FLP).
//
//nolint:dupl,varnamelen // twin of the increasing-order sort below; l/k are lengths, as in the C reference.
func insertionSortDecreasingFLP(a []float32, idx []int, l, k int) {
	for i := range k {
		idx[i] = i
	}
	for i := 1; i < k; i++ {
		value := a[i]
		j := i - 1
		for ; j >= 0 && value > a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}
	for i := k; i < l; i++ {
		value := a[i]
		if value > a[k-1] { //nolint:gosec // G602: k-1 >= 0 for k >= 1.
			j := k - 2
			for ; j >= 0 && value > a[j]; j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}

// insertionSortIncreasing sorts the first K of L values into increasing order,
// tracking their original indices (silk_insertion_sort_increasing).
//
//nolint:dupl,varnamelen // twin of the decreasing-order sort above; l/k are lengths, as in the C reference.
func insertionSortIncreasing(a []int32, idx []int, l, k int) {
	for i := range k {
		idx[i] = i
	}
	for i := 1; i < k; i++ {
		value := a[i]
		j := i - 1
		for ; j >= 0 && value < a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}
	for i := k; i < l; i++ {
		value := a[i]
		if value < a[k-1] { //nolint:gosec // G602: k-1 >= 0 for k >= 1.
			j := k - 2
			for ; j >= 0 && value < a[j]; j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}
