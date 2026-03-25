// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "errors"

var (
	errUnsupportedSilkFrameDuration = errors.New("unsupported silk frame duration")
	errUnsupportedSilkStereo        = errors.New("silk decoder does not support stereo")
	errOutBufferTooSmall            = errors.New("out isn't large enough")

	// ErrNoLowBitrateRedundancy indicates that a packet does not contain in-band FEC data.
	ErrNoLowBitrateRedundancy = errors.New("packet does not contain low bit-rate redundancy")
)
