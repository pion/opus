// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "github.com/pion/opus/internal/rangecoding"

// Encoder quantizes and range-encodes a single SILK channel. It is the
// counterpart to Decoder and is built up one stage at a time.
//
// State fields match the corresponding Decoder fields and use the same reset
// values so the encoder and decoder stay in sync across an uncoded frame.
type Encoder struct {
	rangeEncoder rangecoding.Encoder

	// haveEncoded reports whether a frame has been encoded yet; it selects
	// independent gain coding for the first subframe of the first frame.
	haveEncoded bool

	// previousLogGain is the running quantized log-gain index carried across
	// subframes and frames.
	previousLogGain int32

	// previousLag and isPreviousFrameVoiced carry pitch state across frames,
	// selecting relative vs absolute primary-lag coding.
	previousLag           int
	isPreviousFrameVoiced bool
}

// NewEncoder creates a SILK Encoder with its prediction state reset.
func NewEncoder() Encoder {
	e := Encoder{}
	e.resetPredictionState()

	return e
}

// resetPredictionState resets the encoder prediction state. The values must
// match Decoder.resetPredictionState.
func (e *Encoder) resetPredictionState() {
	e.haveEncoded = false
	e.previousLogGain = 10
	e.previousLag = 100
	e.isPreviousFrameVoiced = false
}
