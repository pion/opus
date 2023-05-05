// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package opus provides a Opus Audio Codec RFC 6716 implementation
package opus

import (
	"fmt"

	"github.com/pion/opus/internal/bitdepth"
	"github.com/pion/opus/internal/resample"
	"github.com/pion/opus/internal/silk"
)

// Decoder decodes the Opus bitstream into PCM
type Decoder struct {
	silkDecoder silk.Decoder
	silkBuffer  []float32
}

// NewDecoder creates a new Opus Decoder
func NewDecoder() Decoder {
	return Decoder{
		silkDecoder: silk.NewDecoder(),
		silkBuffer:  make([]float32, 320),
	}
}

func (d *Decoder) decode(in []byte, out []float32) (bandwidth Bandwidth, isStereo bool, err error) {
	if len(in) < 1 {
		return 0, false, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	var encodedFrames [][]byte
	switch tocHeader.frameCode() {
	case frameCodeOneFrame:
		encodedFrames = append(encodedFrames, in[1:])
	default:
		return 0, false, fmt.Errorf("%w: %d", errUnsupportedFrameCode, tocHeader.frameCode())
	}

	if cfg.mode() != configurationModeSilkOnly {
		return 0, false, fmt.Errorf("%w: %d", errUnsupportedConfigurationMode, cfg.mode())
	}

	for _, encodedFrame := range encodedFrames {
		err := d.silkDecoder.Decode(encodedFrame, out, tocHeader.isStereo(), cfg.frameDuration().nanoseconds(), silk.Bandwidth(cfg.bandwidth()))
		if err != nil {
			return 0, false, err
		}
	}

	return cfg.bandwidth(), tocHeader.isStereo(), nil
}

// Decode decodes the Opus bitstream into S16LE PCM
func (d *Decoder) Decode(in, out []byte) (bandwidth Bandwidth, isStereo bool, err error) {
	bandwidth, isStereo, err = d.decode(in, d.silkBuffer)
	if err != nil {
		return
	}

	err = bitdepth.ConvertFloat32LittleEndianToSigned16LittleEndian(d.silkBuffer, out, 3)
	return
}

// DecodeFloat32 decodes the Opus bitstream into F32LE PCM
func (d *Decoder) DecodeFloat32(in []byte, out []float32) (bandwidth Bandwidth, isStereo bool, err error) {
	bandwidth, isStereo, err = d.decode(in, d.silkBuffer)
	if err != nil {
		return
	}

	resample.Up(d.silkBuffer, out, 3)
	return
}
