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

// Float32ToSigned16 quantizes a float32 PCM sample to signed 16-bit PCM.
func Float32ToSigned16(sample float32) int16 {
	sample64 := math.Round(float64(sample * 32768))
	sample64 = math.Max(sample64, -32768)
	sample64 = math.Min(sample64, 32767)

	return int16(sample64)
}

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
				res := Float32ToSigned16(in[i+k])

				out[currIndex] = byte(res & 0b11111111)
				currIndex++

				out[currIndex] = byte(uint16(res) >> 8) // #nosec G115
				currIndex++
			}
		}
	}

	return nil
}
