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
	// maxBandSampleCount is the widest band — bands 20 and 21 span 22 edge
	// units — at the longest frame, which bounds any per-band scratch.
	maxBandSampleCount = 22 << maxLM
)

// encoderScratch holds the analysis path's working buffers. EncodeFrame runs 50
// times a second per stream, so allocating these per frame is pure garbage —
// they are sized for the worst case once and reused, the same way
// decoderScratch works.
type encoderScratch struct {
	autocorr [pitchLPCOrder + 1]float32
	lpc      [pitchLPCOrder]float32
	// pitchSearch decimates by a further 2, so its buffers are a quarter of the
	// window it is handed.
	pitchX   [maxFrameSampleCount >> 2]float32
	pitchY   [(maxFrameSampleCount + combFilterMaxPeriod) >> 2]float32
	pitchXC  [combFilterMaxPeriod >> 1]float32
	yyLookup [(combFilterMaxPeriod >> 1) + 1]float32
	// tfAnalysis works one band at a time, so the widest band at the longest
	// frame bounds both of its copies.
	tfTmp    [maxBandSampleCount]float32
	tfTmpOne [maxBandSampleCount]float32
}
