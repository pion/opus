// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package celt implements the MDCT layer of the Opus decoder.
package celt

const (
	// RFC 6716 Section 4.3 defines the normal Opus CELT layer around a
	// 48 kHz mode with 21 energy bands and 2.5 ms band-edge units.
	sampleRate            = 48000
	shortBlockSampleCount = 120
	// maxCELTFrameBytes is the largest Opus frame payload (RFC 6716 Section 3.4).
	maxCELTFrameBytes   = 1275
	maxLM               = 3
	maxFrameSampleCount = shortBlockSampleCount << maxLM
	maxBands            = 21
	hybridStartBand     = 17
)
