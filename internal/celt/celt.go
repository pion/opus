// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package celt implements the MDCT layer of the Opus decoder.
package celt

const (
	// RFC 6716 Section 4.3 defines the normal Opus CELT layer around a
	// 48 kHz mode with 21 energy bands and 2.5 ms band-edge units.
	sampleRate            = 48000
	shortBlockSampleCount = 120
	maxLM                 = 3
	maxBands              = 21
	hybridStartBand       = 17
)
