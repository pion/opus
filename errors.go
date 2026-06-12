// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import "errors"

var (
	errTooShortForTableOfContentsHeader = errors.New("packet is too short to contain table of contents header")

	errUnsupportedFrameCode = errors.New("unsupported frame code")

	errUnsupportedConfigurationMode = errors.New("unsupported configuration mode")

	errInvalidSampleRate = errors.New("invalid sample rate")

	errInvalidChannelCount = errors.New("invalid channel count")

	errOutBufferTooSmall = errors.New("out isn't large enough")

	errMalformedPacket = errors.New("malformed packet")

	errBitrateOutOfRange = errors.New("bitrate out of range")

	errInvalidComplexity = errors.New("invalid complexity")

	errInvalidInputLength = errors.New("invalid input length")

	errInvalidFrameSize = errors.New("invalid frame size")

	errInvalidFrameByteBudget = errors.New("invalid frame byte budget")

	errInvalidPLCFrameSize = errors.New("PLC output must contain exactly 20 ms of interleaved samples")
)
