// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/pion/opus/pkg/oggreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tocByte(code frameCode) byte {
	return byte(9<<3) | byte(code)
}

func makePacket(toc byte, payloadLen int) []byte {
	packet := make([]byte, 1+payloadLen)
	packet[0] = toc

	return packet
}

func TestParsePacketFramesRFC6716MalformedRules(t *testing.T) {
	t.Parallel()

	tocCode0 := tocByte(frameCodeOneFrame)
	tocCode1 := tocByte(frameCodeTwoEqualFrames)
	tocCode2 := tocByte(frameCodeTwoDifferentFrames)
	tocCode3 := tocByte(frameCodeArbitraryFrames)

	oversizedCode0 := makePacket(tocCode0, maxOpusFrameSize+1)
	oversizedCode1 := makePacket(tocCode1, 2*(maxOpusFrameSize+1))

	tests := []struct {
		name   string
		packet []byte
	}{
		{
			// RFC 6716 §3.4 [R2]: no implicit frame length may exceed 1275 bytes.
			name:   "R2 code 0 implicit frame larger than 1275 bytes",
			packet: oversizedCode0,
		},
		{
			// RFC 6716 §3.4 [R2]: no implicit frame length may exceed 1275 bytes.
			name:   "R2 code 1 implicit frame larger than 1275 bytes",
			packet: oversizedCode1,
		},
		{
			// RFC 6716 §3.4 [R3]: code 1 packets must have odd total length N so
			// (N-1)/2 is an integer.
			name:   "R3 code 1 even total length",
			packet: []byte{tocCode1, 0x00},
		},
		{
			// RFC 6716 §3.4 [R4]: code 2 packets must have enough bytes after the
			// TOC for a valid first-frame length.
			name:   "R4 code 2 missing first frame length",
			packet: []byte{tocCode2},
		},
		{
			// RFC 6716 §3.4 [R4]: the first-frame length in a code 2 packet must
			// be no larger than the bytes remaining in the packet.
			name:   "R4 code 2 first frame length exceeds remaining payload",
			packet: []byte{tocCode2, 3, 0xAA, 0xBB},
		},
		{
			// RFC 6716 §3.4 [R5]: code 3 packets must contain at least one frame.
			name:   "R5 code 3 zero frame count",
			packet: []byte{tocCode3, 0x00},
		},
		{
			// RFC 6716 §3.4 [R5]: code 3 packets must contain no more than 120 ms
			// of audio total.
			name:   "R5 code 3 total duration exceeds 120 ms",
			packet: []byte{tocCode3, 7},
		},
		{
			// RFC 6716 §3.4 [R6]: for CBR code 3, padding metadata plus trailing
			// padding must not exceed N-2.
			name:   "R6 code 3 CBR padding exceeds packet payload",
			packet: []byte{tocCode3, 0b01000001, 5},
		},
		{
			// RFC 6716 §3.4 [R6]: for CBR code 3, (N-2-P) must be a non-negative
			// integer multiple of M.
			name:   "R6 code 3 CBR payload not divisible by frame count",
			packet: []byte{tocCode3, 2, 0xAA},
		},
		{
			// RFC 6716 §3.4 [R7]: VBR code 3 packets must be large enough for all
			// header bytes, the first M-1 frames, and any trailing padding.
			name:   "R7 code 3 VBR frame length exceeds remaining payload",
			packet: []byte{tocCode3, 0b10000010, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parsePacketFrames(tt.packet, tableOfContentsHeader(tt.packet[0]))
			require.Error(t, err)
			assert.ErrorIs(t, err, errMalformedPacket)
		})
	}
}

func TestDecodeRejectsEmptyPacket(t *testing.T) {
	t.Parallel()

	decoder := NewDecoder()

	bandwidth, isStereo, sampleCount, err := decoder.decode(nil, make([]float32, 0))

	require.Error(t, err)
	assert.Zero(t, bandwidth)
	assert.False(t, isStereo)
	assert.Zero(t, sampleCount)
	assert.ErrorIs(t, err, errTooShortForTableOfContentsHeader)
}

func TestParsePacketFramesValidEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("code 1 splits two equal-sized frames", func(t *testing.T) {
		t.Parallel()

		packet := []byte{tocByte(frameCodeTwoEqualFrames), 1, 2, 3, 4}
		frames, err := parsePacketFrames(packet, tableOfContentsHeader(packet[0]))

		require.NoError(t, err)
		assert.Equal(t, [][]byte{{1, 2}, {3, 4}}, frames)
	})

	t.Run("code 2 rejects truncated two-byte frame length", func(t *testing.T) {
		t.Parallel()

		packet := []byte{tocByte(frameCodeTwoDifferentFrames), 252}
		_, err := parsePacketFrames(packet, tableOfContentsHeader(packet[0]))

		require.Error(t, err)
		assert.ErrorIs(t, err, errMalformedPacket)
	})

	t.Run("code 2 allows two zero-length frames", func(t *testing.T) {
		t.Parallel()

		// RFC 6716 §3.2.4: a 2-byte code 2 packet with first-frame length 0 is
		// valid, yielding two zero-length frames.
		packet := []byte{tocByte(frameCodeTwoDifferentFrames), 0}
		frames, err := parsePacketFrames(packet, tableOfContentsHeader(packet[0]))

		require.NoError(t, err)
		require.Len(t, frames, 2)
		assert.Empty(t, frames[0])
		assert.Empty(t, frames[1])
	})

	t.Run("code 3 CBR allows one zero-length frame", func(t *testing.T) {
		t.Parallel()

		// RFC 6716 §3.2.5: a code 3 CBR packet with M=1 and no payload is valid,
		// yielding one zero-length frame.
		packet := []byte{tocByte(frameCodeArbitraryFrames), 1}
		frames, err := parsePacketFrames(packet, tableOfContentsHeader(packet[0]))

		require.NoError(t, err)
		require.Len(t, frames, 1)
		assert.Empty(t, frames[0])
	})

	t.Run("code 3 padding continuation bytes contribute 254 trailing bytes", func(t *testing.T) {
		t.Parallel()

		// RFC 6716 §3.2.5: a padding length byte of 255 contributes 254 bytes of
		// trailing padding, plus the next padding length byte.
		packet := []byte{tocByte(frameCodeArbitraryFrames), 0b01000001, 255, 0, 0xAA}
		packet = append(packet, make([]byte, 254)...)
		frames, err := parsePacketFrames(packet, tableOfContentsHeader(packet[0]))

		require.NoError(t, err)
		require.Len(t, frames, 1)
		assert.Equal(t, []byte{0xAA}, frames[0])
	})

	t.Run("code 3 VBR rejects oversized final implicit frame", func(t *testing.T) {
		t.Parallel()

		packet := []byte{tocByte(frameCodeArbitraryFrames), 0b10000010, 0}
		packet = append(packet, make([]byte, maxOpusFrameSize+1)...)
		_, err := parsePacketFrames(packet, tableOfContentsHeader(packet[0]))

		require.Error(t, err)
		assert.ErrorIs(t, err, errMalformedPacket)
	})
}

func TestDecodePacketFrames(t *testing.T) {
	t.Parallel()

	t.Run("resizes silk buffer for multiple frames before decode", func(t *testing.T) {
		t.Parallel()

		decoder := NewDecoder()
		_, _, _, err := decoder.decode([]byte{tocByte(frameCodeTwoEqualFrames) | 0b100}, decoder.silkBuffer)

		require.NoError(t, err)
		assert.Equal(t, maxSilkFrameSampleCount*4, len(decoder.silkBuffer))
	})

	t.Run("Decode rejects empty packets", func(t *testing.T) {
		t.Parallel()

		decoder := NewDecoder()
		_, _, err := decoder.Decode(nil, make([]byte, 0))

		require.Error(t, err)
		assert.ErrorIs(t, err, errTooShortForTableOfContentsHeader)
	})

	t.Run("DecodeFloat32 decodes an ogg packet", func(t *testing.T) {
		t.Parallel()

		ogg, _, err := oggreader.NewWith(bytes.NewReader(tinyogg))
		require.NoError(t, err)

		decoder := NewDecoder()
		var out [960]float32
		for {
			segments, _, err := ogg.ParseNextPage()
			if errors.Is(err, io.EOF) {
				require.FailNow(t, "no audio packet found")
			}
			require.NoError(t, err)
			if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
				continue
			}

			_, _, err = decoder.DecodeFloat32(segments[0], out[:])
			require.NoError(t, err)

			return
		}
	})
}
