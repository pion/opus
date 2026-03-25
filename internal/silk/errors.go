// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "errors"

var (
	errUnsupportedSilkFrameDuration        = errors.New("unsupported silk frame duration")
	errUnsupportedSilkStereo               = errors.New("silk decoder does not support stereo")
	errUnsupportedSilkLowBitrateRedundancy = errors.New("silk decoder does not support low bit-rate redundancy")
	errOutBufferTooSmall                   = errors.New("out isn't large enough")
)
