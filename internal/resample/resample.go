// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package resample provides tools to resample audio
package resample

// Up upsamples the requested amount
func Up(in, out []float32, upsampleCount int) {
	currIndex := 0
	for i := range in {
		for j := 0; j < upsampleCount; j++ {
			out[currIndex] = in[i]
			currIndex++
		}
	}
}
