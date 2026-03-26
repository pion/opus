// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package bitdepth provides utilities to convert between different audio bitdepths
package bitdepth

import (
	"errors"
	"math"
)

var (
	errInvalidChannelCount  = errors.New("channel count must be positive")
	errInvalidInputLength   = errors.New("input length must be divisible by channel count")
	errInvalidResampleCount = errors.New("resample count must be positive")
	errOutBufferTooSmall    = errors.New("out isn't large enough")
)

// ConvertFloat32LittleEndianToSigned16LittleEndian converts a f32le to s16le.
func ConvertFloat32LittleEndianToSigned16LittleEndian(
	in []float32,
	out []byte,
	channelCount int,
	resampleCount int,
) error {
	if channelCount <= 0 {
		return errInvalidChannelCount
	}
	if resampleCount <= 0 {
		return errInvalidResampleCount
	}
	if len(in)%channelCount != 0 {
		return errInvalidInputLength
	}
	if len(in)*resampleCount*2 > len(out) {
		return errOutBufferTooSmall
	}

	currIndex := 0
	for i := 0; i < len(in); i += channelCount {
		for j := resampleCount; j > 0; j-- {
			for k := range channelCount {
				res := int16(math.Floor(float64(in[i+k] * 32767)))

				out[currIndex] = byte(res & 0b11111111)
				currIndex++

				out[currIndex] = byte(uint16(res) >> 8) // #nosec G115
				currIndex++
			}
		}
	}

	return nil
}
