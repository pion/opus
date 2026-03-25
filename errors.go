// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import "errors"

var (
	errTooShortForTableOfContentsHeader = errors.New("packet is too short to contain table of contents header")

	errUnsupportedFrameCode = errors.New("unsupported frame code")

	errUnsupportedConfigurationMode = errors.New("unsupported configuration mode")

	errUnsupportedSilkRedundancy = errors.New("unsupported silk redundancy")

	errInvalidSampleRate = errors.New("sample rate must be 8000, 12000, 16000, 24000, or 48000")

	errInvalidChannelCount = errors.New("channel count must be mono or stereo")

	errOutBufferTooSmall = errors.New("out isn't large enough")

	errNoLastPacketDuration = errors.New("no last packet duration")

	errInvalidPacketDuration = errors.New("invalid packet duration")

	errUnsupportedSignalType = errors.New("previous signal type is unsupported")

	errMalformedPacket = errors.New("malformed packet")
)
