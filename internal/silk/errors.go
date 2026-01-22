// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "errors"

var (
	errUnsupportedSilkFrameDuration        = errors.New("only silk frames with a duration of 20ms supported")
	errUnsupportedSilkStereo               = errors.New("silk decoder does not support stereo")
	errUnsupportedSilkLowBitrateRedundancy = errors.New("silk decoder does not support low bit-rate redundancy")
	errOutBufferTooSmall                   = errors.New("out isn't large enough")
	errNonAbsoluteLagsUnsupported          = errors.New("silk decoder does not support non-absolute lags")
)
