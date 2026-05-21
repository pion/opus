// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"encoding/binary"
	"fmt"

	"github.com/pion/opus/internal/celt"
)

const (
	defaultBitrate = 24000
	minBitrate     = 6000
	maxBitrate     = 510000
	frame20msNS    = 20000000
)

// celtOnlyFullband20msConfig is the TOC config number (bits 3..7) for
// CELT-only, fullband, 20 ms frames per RFC 6716 Table 2. The mono/stereo bit
// is separate (bit 2 of the TOC) and not part of this constant.
const celtOnlyFullband20msConfig = 31

// Encoder encodes PCM into Opus packets.
type Encoder struct {
	celtEncoder celt.Encoder
	sampleRate  int
	channels    int
	bitrate     int
	complexity  int
}

// NewEncoder creates a new Opus encoder.
//
// This initial wrapper supports only 48 kHz, mono, 20 ms CELT-only packets.
// It wraps the internal CELT encoder and emits a complete Opus packet with
// TOC byte and one frame payload.
func NewEncoder(sampleRate, channels int) (Encoder, error) {
	encoder := Encoder{
		celtEncoder: celt.NewEncoder(),
		bitrate:     defaultBitrate,
		complexity:  0,
	}

	if err := encoder.Init(sampleRate, channels); err != nil {
		return Encoder{}, err
	}

	return encoder, nil
}

// Init initializes a pre-allocated Opus encoder.
//
// The current API surface only supports 48 kHz mono 20 ms CELT-only packets.
func (e *Encoder) Init(sampleRate, channels int) error {
	if sampleRate != celtSampleRate {
		return errInvalidSampleRate
	}

	if channels != 1 {
		return errInvalidChannelCount
	}

	e.sampleRate = sampleRate
	e.channels = channels
	e.bitrate = defaultBitrate
	e.complexity = 0
	e.celtEncoder = celt.NewEncoder()

	return nil
}

// SetBitrate sets the target bitrate in bits per second.
func (e *Encoder) SetBitrate(bps int) error {
	if bps < minBitrate || bps > maxBitrate {
		return fmt.Errorf("%w: %d", errBitrateOutOfRange, bps)
	}

	e.bitrate = bps

	return nil
}

// SetComplexity stores the requested encoder complexity.
//
// The current CELT encoder does not vary behavior by complexity, but the
// public API accepts the standard Opus 0..10 range for future expansion.
func (e *Encoder) SetComplexity(complexity int) error {
	if complexity < 0 || complexity > 10 {
		return fmt.Errorf("%w: %d", errInvalidComplexity, complexity)
	}

	e.complexity = complexity

	return nil
}

// Encode encodes S16LE PCM into a single Opus packet.
//
// The input must contain exactly one 20 ms mono 48 kHz frame.
func (e *Encoder) Encode(in []byte, out []byte) (int, error) {
	if len(in)%2 != 0 {
		return 0, fmt.Errorf("%w: s16le length %d not a multiple of 2", errInvalidInputLength, len(in))
	}

	expectedSamples := e.frameSampleCount() * e.channels
	if len(in)/2 != expectedSamples {
		return 0, fmt.Errorf("%w: got %d samples, want %d", errInvalidFrameSize, len(in)/2, expectedSamples)
	}

	pcm := make([]float32, len(in)/2)
	for i := range pcm {
		sample := int16(binary.LittleEndian.Uint16(in[i*2:])) //nolint:gosec // G115: little-endian s16 round-trip.
		pcm[i] = float32(sample) / 32768
	}

	return e.EncodeFloat32(pcm, out)
}

// EncodeFloat32 encodes float PCM into a single Opus packet.
//
// The input must contain exactly one 20 ms mono 48 kHz frame.
func (e *Encoder) EncodeFloat32(in []float32, out []byte) (int, error) {
	if e.sampleRate != celtSampleRate {
		return 0, errInvalidSampleRate
	}

	if e.channels != 1 {
		return 0, errInvalidChannelCount
	}
	frameSamples := e.frameSampleCount()
	if len(in) != frameSamples*e.channels {
		return 0, fmt.Errorf("%w: got %d samples, want %d", errInvalidFrameSize, len(in), frameSamples*e.channels)
	}

	frameBytes := e.frameBytes()
	if frameBytes <= 0 || frameBytes > maxOpusFrameSize {
		return 0, fmt.Errorf("%w: %d", errInvalidFrameByteBudget, frameBytes)
	}
	if len(out) < frameBytes+1 {
		return 0, errOutBufferTooSmall
	}

	payload, err := e.celtEncoder.EncodeFrame(in, frameBytes, 0, e.celtEncoder.Mode().BandCount())
	if err != nil {
		return 0, err
	}
	if len(payload) > maxOpusFrameSize {
		return 0, fmt.Errorf("%w: frame size %d exceeds %d", errMalformedPacket, len(payload), maxOpusFrameSize)
	}
	if len(out) < len(payload)+1 {
		return 0, errOutBufferTooSmall
	}

	out[0] = byte(e.tocHeader())
	copy(out[1:], payload)

	return 1 + len(payload), nil
}

func (e *Encoder) tocHeader() tableOfContentsHeader {
	header := byte(celtOnlyFullband20msConfig << 3)
	header |= byte(frameCodeOneFrame)

	return tableOfContentsHeader(header)
}

func (e *Encoder) frameBytes() int {
	return int(int64(e.bitrate) * frame20msNS / 1000000000 / 8)
}

func (e *Encoder) frameSampleCount() int {
	return int(int64(celtSampleRate) * frame20msNS / 1000000000)
}
