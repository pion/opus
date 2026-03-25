// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package bitdepth provides utilities to convert between different audio bitdepths
package bitdepth

import (
	"errors"
	"math"
)

var errOutBufferTooSmall = errors.New("out isn't large enough")

// ConvertFloat32LittleEndianToSigned16LittleEndian converts a f32le to s16le.
func ConvertFloat32LittleEndianToSigned16LittleEndian(in []float32, out []byte, resampleCount int) error {
	if len(in)*resampleCount*2 > len(out) {
		return errOutBufferTooSmall
	}

	currIndex := 0
	for i := range in {
		res := int16(math.Floor(float64(in[i] * 32767)))

		for j := resampleCount; j > 0; j-- {
			out[currIndex] = byte(res & 0b11111111)
			currIndex++

			out[currIndex] = byte(uint16(res) >> 8) // #nosec G115
			currIndex++
		}
	}

	return nil
}
