// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import "errors"

var (
	errInvalidFrameSize    = errors.New("invalid CELT frame size")
	errInvalidLM           = errors.New("invalid CELT size shift")
	errInvalidBand         = errors.New("invalid CELT band")
	errInvalidSampleRate   = errors.New("invalid CELT sample rate")
	errInvalidChannelCount = errors.New("invalid CELT channel count")
	errRangeCoderSymbol    = errors.New("invalid CELT range coder symbol")
)
