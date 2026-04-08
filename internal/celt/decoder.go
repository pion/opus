// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import "github.com/pion/opus/internal/rangecoding"

// Decoder maintains state for the RFC 6716 Section 4.3 CELT layer.
type Decoder struct {
	mode         *Mode
	rangeDecoder rangecoding.Decoder
	previousLogE [2][maxBands]float32
	overlap      [2][]float32
}

// NewDecoder creates a CELT decoder with the static Opus 48 kHz mode.
func NewDecoder() Decoder {
	decoder := Decoder{mode: DefaultMode()}
	decoder.Reset()

	return decoder
}

// Reset clears frame-to-frame CELT decode state.
func (d *Decoder) Reset() {
	d.mode = DefaultMode()
	d.rangeDecoder = rangecoding.Decoder{}
	clear(d.previousLogE[0][:])
	clear(d.previousLogE[1][:])

	for channelIndex := range d.overlap {
		if cap(d.overlap[channelIndex]) < shortBlockSampleCount {
			d.overlap[channelIndex] = make([]float32, shortBlockSampleCount)
		}
		clear(d.overlap[channelIndex])
	}
}

// Mode returns the static CELT mode used by this decoder.
func (d *Decoder) Mode() *Mode {
	if d.mode == nil {
		d.mode = DefaultMode()
	}

	return d.mode
}
