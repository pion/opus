// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package resample provides tools to resample audio
package resample

import "errors"

var (
	errInvalidChannelCount  = errors.New("channel count must be positive")
	errInvalidInputLength   = errors.New("input length must be divisible by channel count")
	errInvalidUpsampleCount = errors.New("upsample count must be positive")
	errOutBufferTooSmall    = errors.New("out buffer too small")
)

// Up upsamples the requested amount with sample repetition.
// This is a temporary, low-quality resampler; a better interpolation-based
// algorithm can replace it later.
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.9
func Up(in, out []float32, channelCount, upsampleCount int) error {
	if channelCount <= 0 {
		return errInvalidChannelCount
	}
	if upsampleCount <= 0 {
		return errInvalidUpsampleCount
	}
	if len(in)%channelCount != 0 {
		return errInvalidInputLength
	}
	if len(in)*upsampleCount > len(out) {
		return errOutBufferTooSmall
	}

	currIndex := 0
	for i := 0; i < len(in); i += channelCount {
		for range upsampleCount {
			for k := range channelCount {
				out[currIndex] = in[i+k]
				currIndex++
			}
		}
	}

	return nil
}
