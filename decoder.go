// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package opus provides a Opus Audio Codec RFC 6716 implementation
package opus

import (
	"fmt"

	"github.com/pion/opus/internal/bitdepth"
	"github.com/pion/opus/internal/resample"
	"github.com/pion/opus/internal/silk"
)

const (
	maxOpusFrameSize                = 1275
	maxOpusPacketDurationNanosecond = 120000000
	maxSilkFrameSampleCount         = 320
)

// Decoder decodes the Opus bitstream into PCM.
type Decoder struct {
	silkDecoder silk.Decoder
	silkBuffer  []float32
}

// NewDecoder creates a new Opus Decoder.
func NewDecoder() Decoder {
	return Decoder{
		silkDecoder: silk.NewDecoder(),
		silkBuffer:  make([]float32, maxSilkFrameSampleCount),
	}
}

func parseFrameLength(in []byte) (frameLength int, bytesRead int, err error) {
	if len(in) < 1 {
		return 0, 0, fmt.Errorf("%w: missing frame length", errMalformedPacket)
	}

	if in[0] < 252 {
		return int(in[0]), 1, nil
	}

	if len(in) < 2 {
		return 0, 0, fmt.Errorf("%w: truncated two-byte frame length", errMalformedPacket)
	}

	return int(in[0]) + 4*int(in[1]), 2, nil
}

func parsePacketFramesCode0(in []byte) ([][]byte, error) {
	// [R2] Code 0 uses an implicit frame length for the whole payload, so it
	// must not exceed the 1275-byte maximum.
	if len(in[1:]) > maxOpusFrameSize {
		return nil, fmt.Errorf("%w: frame size %d exceeds %d", errMalformedPacket, len(in[1:]), maxOpusFrameSize)
	}

	return [][]byte{in[1:]}, nil
}

func parsePacketFramesCode1(in []byte) ([][]byte, error) {
	payload := in[1:]
	// [R3] Code 1 packets have an odd total length so (N-1)/2 is integral.
	if len(payload)%2 != 0 {
		return nil, fmt.Errorf("%w: code 1 packet payload must be even-sized", errMalformedPacket)
	}

	// [R2] Code 1 uses an implicit length for both equal-sized frames.
	frameSize := len(payload) / 2
	if frameSize > maxOpusFrameSize {
		return nil, fmt.Errorf("%w: frame size %d exceeds %d", errMalformedPacket, frameSize, maxOpusFrameSize)
	}

	return [][]byte{payload[:frameSize], payload[frameSize:]}, nil
}

func parsePacketFramesCode2(in []byte) ([][]byte, error) {
	// [R4] Code 2 must have enough bytes after the TOC to decode a valid
	// first-frame length.
	frameSize, bytesRead, err := parseFrameLength(in[1:])
	if err != nil {
		return nil, err
	}

	firstFrameStart := 1 + bytesRead
	firstFrameEnd := firstFrameStart + frameSize
	// [R4] The signaled first-frame length must fit in the remaining bytes.
	if firstFrameEnd > len(in) {
		return nil, fmt.Errorf("%w: first frame overruns packet", errMalformedPacket)
	}

	// [R2] The second Code 2 frame has an implicit length from the remainder.
	secondFrameSize := len(in) - firstFrameEnd
	if secondFrameSize > maxOpusFrameSize {
		return nil, fmt.Errorf("%w: frame size %d exceeds %d", errMalformedPacket, secondFrameSize, maxOpusFrameSize)
	}

	return [][]byte{in[firstFrameStart:firstFrameEnd], in[firstFrameEnd:]}, nil
}

func parsePacketPadding(in []byte, offset int) (padding int, newOffset int, err error) {
	for {
		// [R6][R7] Padding length bytes are part of the Code 3 header and
		// must be present before any frame data.
		if offset >= len(in) {
			return 0, 0, fmt.Errorf("%w: truncated padding length", errMalformedPacket)
		}

		paddingByte := int(in[offset])
		offset++
		if paddingByte == 255 {
			padding += 254

			continue
		}

		padding += paddingByte

		break
	}

	return padding, offset, nil
}

func parsePacketFramesCode3(in []byte, tocHeader tableOfContentsHeader) ([][]byte, error) {
	// [R6][R7] Code 3 packets need at least TOC + frame count bytes.
	if len(in) < 2 {
		return nil, fmt.Errorf("%w: code 3 packet missing frame count byte", errMalformedPacket)
	}

	isVBR, hasPadding, frameCount := parseFrameCountByte(in[1])
	// [R5] Code 3 packets must contain at least one frame.
	if frameCount == 0 {
		return nil, fmt.Errorf("%w: code 3 frame count must not be zero", errMalformedPacket)
	}

	// [R5] Total audio duration in a packet is capped at 120 ms.
	if int(frameCount)*tocHeader.configuration().frameDuration().nanoseconds() > maxOpusPacketDurationNanosecond {
		return nil, fmt.Errorf("%w: packet duration exceeds 120 ms", errMalformedPacket)
	}

	offset := 2
	padding := 0
	var err error
	if hasPadding {
		padding, offset, err = parsePacketPadding(in, offset)
		if err != nil {
			return nil, err
		}
	}

	payloadEnd := len(in) - padding
	// [R6] In CBR Code 3, the padding-length bytes plus trailing padding
	// must fit within the packet, leaving at least TOC + frame count.
	// [R7] In VBR Code 3, the same bound applies before frame data.
	if payloadEnd < offset {
		return nil, fmt.Errorf("%w: padding overruns packet", errMalformedPacket)
	}

	if !isVBR {
		return parsePacketFramesCode3CBR(in, offset, payloadEnd, frameCount)
	}

	return parsePacketFramesCode3VBR(in, offset, payloadEnd, frameCount)
}

func parsePacketFramesCode3CBR(in []byte, offset, payloadEnd int, frameCount byte) ([][]byte, error) {
	payloadSize := payloadEnd - offset
	// [R6] CBR payload size must be an integer multiple of M frames.
	if payloadSize%int(frameCount) != 0 {
		return nil, fmt.Errorf("%w: CBR payload not divisible by frame count", errMalformedPacket)
	}

	// [R2] CBR Code 3 uses an implicit equal frame length.
	frameSize := payloadSize / int(frameCount)
	if frameSize > maxOpusFrameSize {
		return nil, fmt.Errorf("%w: frame size %d exceeds %d", errMalformedPacket, frameSize, maxOpusFrameSize)
	}

	frames := make([][]byte, 0, frameCount)
	for i := 0; i < int(frameCount); i++ {
		frames = append(frames, in[offset:offset+frameSize])
		offset += frameSize
	}

	return frames, nil
}

func parsePacketFramesCode3VBR(in []byte, offset, payloadEnd int, frameCount byte) ([][]byte, error) {
	frameSizes := make([]int, 0, frameCount)
	for i := 0; i < int(frameCount)-1; i++ {
		// [R7] VBR Code 3 must have enough header bytes to decode each of the
		// first M-1 frame lengths.
		frameSize, bytesRead, err := parseFrameLength(in[offset:payloadEnd])
		if err != nil {
			return nil, err
		}

		offset += bytesRead
		frameSizes = append(frameSizes, frameSize)
	}

	frames := make([][]byte, 0, frameCount)
	for _, frameSize := range frameSizes {
		// [R7] The first M-1 VBR frames must fit before the final implicit
		// frame and any trailing padding.
		if offset+frameSize > payloadEnd {
			return nil, fmt.Errorf("%w: VBR frame overruns packet", errMalformedPacket)
		}

		frames = append(frames, in[offset:offset+frameSize])
		offset += frameSize
	}

	// [R2] The final VBR Code 3 frame has an implicit length from the
	// remaining payload.
	lastFrameSize := payloadEnd - offset
	if lastFrameSize < 0 {
		return nil, fmt.Errorf("%w: VBR payload underrun", errMalformedPacket)
	}
	if lastFrameSize > maxOpusFrameSize {
		return nil, fmt.Errorf("%w: frame size %d exceeds %d", errMalformedPacket, lastFrameSize, maxOpusFrameSize)
	}
	frames = append(frames, in[offset:payloadEnd])

	return frames, nil
}

func parsePacketFrames(in []byte, tocHeader tableOfContentsHeader) ([][]byte, error) {
	// [R1] A well-formed Opus packet contains at least one byte for the TOC.
	if len(in) < 1 {
		return nil, fmt.Errorf("%w: %w", errMalformedPacket, errTooShortForTableOfContentsHeader)
	}

	switch tocHeader.frameCode() {
	case frameCodeOneFrame:
		return parsePacketFramesCode0(in)
	case frameCodeTwoEqualFrames:
		return parsePacketFramesCode1(in)
	case frameCodeTwoDifferentFrames:
		return parsePacketFramesCode2(in)
	case frameCodeArbitraryFrames:
		return parsePacketFramesCode3(in, tocHeader)
	default:
		return nil, fmt.Errorf("%w: %d", errUnsupportedFrameCode, tocHeader.frameCode())
	}
}

func (d *Decoder) decode(in []byte, out []float32) (bandwidth Bandwidth, isStereo bool, err error) {
	if len(in) < 1 {
		return 0, false, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	encodedFrames, err := parsePacketFrames(in, tocHeader)
	if err != nil {
		return 0, false, err
	}

	if cfg.mode() != configurationModeSilkOnly {
		return 0, false, fmt.Errorf("%w: %d", errUnsupportedConfigurationMode, cfg.mode())
	}

	requiredSamples := maxSilkFrameSampleCount * len(encodedFrames)
	if cap(out) < requiredSamples {
		d.silkBuffer = make([]float32, requiredSamples)
		out = d.silkBuffer
	}
	out = out[:requiredSamples]
	for i := range out {
		out[i] = 0
	}

	for i, encodedFrame := range encodedFrames {
		frameOut := out[i*maxSilkFrameSampleCount : (i+1)*maxSilkFrameSampleCount]
		err := d.silkDecoder.Decode(
			encodedFrame,
			frameOut,
			tocHeader.isStereo(),
			cfg.frameDuration().nanoseconds(),
			silk.Bandwidth(cfg.bandwidth()),
		)
		if err != nil {
			return 0, false, err
		}
	}

	return cfg.bandwidth(), tocHeader.isStereo(), nil
}

// Decode decodes the Opus bitstream into S16LE PCM.
func (d *Decoder) Decode(in, out []byte) (bandwidth Bandwidth, isStereo bool, err error) {
	bandwidth, isStereo, err = d.decode(in, d.silkBuffer)
	if err != nil {
		return
	}

	err = bitdepth.ConvertFloat32LittleEndianToSigned16LittleEndian(d.silkBuffer, out, 3)

	return
}

// DecodeFloat32 decodes the Opus bitstream into F32LE PCM.
func (d *Decoder) DecodeFloat32(in []byte, out []float32) (bandwidth Bandwidth, isStereo bool, err error) {
	bandwidth, isStereo, err = d.decode(in, d.silkBuffer)
	if err != nil {
		return
	}

	resample.Up(d.silkBuffer, out, 3)

	return
}
