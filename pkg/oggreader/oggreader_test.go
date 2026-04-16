// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package oggreader

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

// buildOggFile generates a valid oggfile that can
// be used for tests.
func buildOggContainer() []byte {
	return []byte{
		0x4f, 0x67, 0x67, 0x53, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x8e, 0x9b, 0x20, 0xaa, 0x00, 0x00,
		0x00, 0x00, 0x61, 0xee, 0x61, 0x17, 0x01, 0x13, 0x4f, 0x70,
		0x75, 0x73, 0x48, 0x65, 0x61, 0x64, 0x01, 0x02, 0x00, 0x0f,
		0x80, 0xbb, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4f, 0x67, 0x67,
		0x53, 0x00, 0x00, 0xda, 0x93, 0xc2, 0xd9, 0x00, 0x00, 0x00,
		0x00, 0x8e, 0x9b, 0x20, 0xaa, 0x02, 0x00, 0x00, 0x00, 0x49,
		0x97, 0x03, 0x37, 0x01, 0x05, 0x98, 0x36, 0xbe, 0x88, 0x9e,
	}
}

func TestOggReader_ParseValidHeader(t *testing.T) {
	reader, header, err := NewWith(bytes.NewReader(buildOggContainer()))
	assert.NoError(t, err)
	assert.NotNil(t, reader)

	assert.Equal(t, &OggHeader{
		ChannelMap: 0x0,
		Channels:   0x2,
		OutputGain: 0x0,
		PreSkip:    0xf00,
		SampleRate: 0xbb80,
		Version:    0x1,
	},
		header)
}

func TestOggReader_ParseNextPage(t *testing.T) {
	ogg := bytes.NewReader(buildOggContainer())
	reader, _, err := NewWith(ogg)
	assert.NoError(t, err)
	assert.NotNil(t, reader)

	payload, _, err := reader.ParseNextPage()
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{0x98, 0x36, 0xbe, 0x88, 0x9e}}, payload)

	_, _, err = reader.ParseNextPage()
	assert.ErrorIs(t, io.EOF, err)
}

func TestOggReader_ParseErrors(t *testing.T) {
	t.Run("Assert that Reader isn't nil", func(t *testing.T) {
		_, _, err := NewWith(nil)
		assert.ErrorIs(t, errNilStream, err)
	})

	t.Run("Invalid ID Page Header Signature", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[0] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPageSignature, err)
	})

	t.Run("Invalid ID Page Header Type", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[5] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPageType, err)
	})

	t.Run("Invalid ID Page Payload Length", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[27] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPageLength, err)
	})

	t.Run("Invalid ID Page Payload Length", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[35] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPagePayloadSignature, err)
	})

	t.Run("Invalid Page Checksum", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[22] = 0

		_, _, err := NewWith(bytes.NewReader(ogg))
		assert.ErrorIs(t, errChecksumMismatch, err)
	})
}

func TestOggReader_ParseHeaderFamily1(t *testing.T) {
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(1, 3, 2, 1, []uint8{0, 1, 2}, nil),
		}),
	))

	reader, header, err := NewWith(stream)
	assert.NoError(t, err)
	assert.Equal(t, uint8(1), header.ChannelMap)
	assert.Equal(t, uint8(3), header.Channels)
	assert.Equal(t, OggChannelMapping{
		StreamCount:  2,
		CoupledCount: 1,
		Mapping:      []uint8{0, 1, 2},
	}, reader.ChannelMapping())
}

func TestOggReader_ParseHeaderFamily3(t *testing.T) {
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(3, 4, 2, 1, nil, []int16{
				32767, 0, 0, 0,
				0, 32767, 0, 0,
				0, 0, 32767, 0,
			}),
		}),
	))

	reader, header, err := NewWith(stream)
	assert.NoError(t, err)
	assert.Equal(t, uint8(3), header.ChannelMap)
	assert.Equal(t, uint8(4), header.Channels)
	assert.Equal(t, OggChannelMapping{
		StreamCount:  2,
		CoupledCount: 1,
		DemixingMatrix: []int16{
			32767, 0, 0, 0,
			0, 32767, 0, 0,
			0, 0, 32767, 0,
		},
	}, reader.ChannelMapping())
}

func TestOggReader_ChannelMappingReturnsCopy(t *testing.T) {
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(1, 3, 2, 1, []uint8{0, 1, 2}, nil),
		}),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	mapping := reader.ChannelMapping()
	mapping.Mapping[0] = 9

	assert.Equal(t, OggChannelMapping{
		StreamCount:  2,
		CoupledCount: 1,
		Mapping:      []uint8{0, 1, 2},
	}, reader.ChannelMapping())
}

func TestOggReader_ParseHeaderErrors(t *testing.T) {
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(1, 3, 2, 1, []uint8{0, 1}, nil),
		}),
	))

	_, _, err := NewWith(stream)
	assert.ErrorIs(t, err, errBadIDPageLength)
}

func TestOggReader_ParseNextPacketMultipleSegmentsOnePage(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 300)
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(0, 2, 0, 0, nil, nil),
		}),
		buildOggPage(0, 960, 1, 1, splitSegments(payload, 255, 45)),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	packet, header, err := reader.ParseNextPacket()
	assert.NoError(t, err)
	assert.Equal(t, payload, packet)
	assert.Equal(t, uint64(960), header.GranulePosition)
}

func TestOggReader_ParseNextPacketContinuedAcrossPages(t *testing.T) {
	payload := bytes.Repeat([]byte{0xCD}, 300)
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(0, 2, 0, 0, nil, nil),
		}),
		buildOggPage(0, 0, 1, 1, splitSegments(payload[:255], 255)),
		buildOggPage(pageHeaderTypeContinuedPacket, 960, 1, 2, splitSegments(payload[255:], 45)),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	packet, header, err := reader.ParseNextPacket()
	assert.NoError(t, err)
	assert.Equal(t, payload, packet)
	assert.Equal(t, uint64(960), header.GranulePosition)
}

func TestOggReader_ParseNextPacketSkipsLeadingContinuedPacketAcrossPages(t *testing.T) {
	discarded := bytes.Repeat([]byte{0xDE}, 300)
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(0, 2, 0, 0, nil, nil),
		}),
		buildOggPage(pageHeaderTypeContinuedPacket, 0, 1, 1, splitSegments(discarded[:255], 255)),
		buildOggPage(pageHeaderTypeContinuedPacket, 960, 1, 2, [][]byte{
			discarded[255:],
			{0xAA, 0xBB},
		}),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	packet, header, err := reader.ParseNextPacket()
	assert.NoError(t, err)
	assert.Equal(t, []byte{0xAA, 0xBB}, packet)
	assert.Equal(t, uint64(960), header.GranulePosition)
}

func TestOggReader_ParseNextPacketMultiplePacketsOnePage(t *testing.T) {
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(0, 2, 0, 0, nil, nil),
		}),
		buildOggPage(0, 960, 1, 1, [][]byte{
			{0xAA},
			{0xBB, 0xCC},
		}),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	firstPacket, _, err := reader.ParseNextPacket()
	assert.NoError(t, err)
	assert.Equal(t, []byte{0xAA}, firstPacket)

	secondPacket, _, err := reader.ParseNextPacket()
	assert.NoError(t, err)
	assert.Equal(t, []byte{0xBB, 0xCC}, secondPacket)

	_, _, err = reader.ParseNextPacket()
	assert.ErrorIs(t, err, io.EOF)
}

func TestOggReader_ParseNextPacketUnexpectedEOF(t *testing.T) {
	payload := bytes.Repeat([]byte{0xEF}, 255)
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(0, 2, 0, 0, nil, nil),
		}),
		buildOggPage(0, 0, 1, 1, splitSegments(payload, 255)),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	_, _, err = reader.ParseNextPacket()
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestOggReader_ParseNextPacketSequenceGapReturnsUnexpectedEOF(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 300)
	stream := bytes.NewReader(buildOggStream(
		buildOggPage(pageHeaderTypeBeginningOfStream, 0, 1, 0, [][]byte{
			buildOpusIDHeader(0, 2, 0, 0, nil, nil),
		}),
		buildOggPage(0, 0, 1, 1, splitSegments(payload[:255], 255)),
		buildOggPage(pageHeaderTypeContinuedPacket, 960, 1, 3, splitSegments(payload[255:], 45)),
	))

	reader, _, err := NewWith(stream)
	assert.NoError(t, err)

	_, _, err = reader.ParseNextPacket()
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func buildOggStream(pages ...[]byte) []byte {
	stream := make([]byte, 0)
	for _, page := range pages {
		stream = append(stream, page...)
	}

	return stream
}

func buildOggPage(headerType byte, granule uint64, serial, index uint32, segments [][]byte) []byte {
	page := make([]byte, pageHeaderLen)
	copy(page[:4], []byte(pageHeaderSignature))
	page[4] = 0
	page[5] = headerType
	binary.LittleEndian.PutUint64(page[6:14], granule)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], index)
	page[26] = byte(len(segments))

	for _, segment := range segments {
		page = append(page, byte(len(segment)))
	}
	for _, segment := range segments {
		page = append(page, segment...)
	}

	checksum := oggChecksum(page)
	binary.LittleEndian.PutUint32(page[22:26], checksum)

	return page
}

func buildOpusIDHeader(
	channelMapFamily, channels, streamCount, coupledCount uint8,
	mapping []uint8,
	demixingMatrix []int16,
) []byte {
	header := make([]byte, 0, 21+len(mapping)+(2*len(demixingMatrix)))
	header = append(header, []byte(idPageSignature)...)
	header = append(header, 1)
	header = append(header, channels)

	preskip := make([]byte, 2)
	binary.LittleEndian.PutUint16(preskip, 0x0F00)
	header = append(header, preskip...)

	sampleRate := make([]byte, 4)
	binary.LittleEndian.PutUint32(sampleRate, 48000)
	header = append(header, sampleRate...)

	header = append(header, 0, 0)
	header = append(header, channelMapFamily)

	switch channelMapFamily {
	case 1, 2, 255:
		header = append(header, streamCount, coupledCount)
		header = append(header, mapping...)
	case 3:
		header = append(header, streamCount, coupledCount)
		for _, coefficient := range demixingMatrix {
			packed := make([]byte, 2)
			binary.LittleEndian.PutUint16(packed, uint16(coefficient))
			header = append(header, packed...)
		}
	}

	return header
}

func splitSegments(in []byte, sizes ...int) [][]byte {
	segments := make([][]byte, 0, len(sizes))
	offset := 0
	for _, size := range sizes {
		segments = append(segments, append([]byte(nil), in[offset:offset+size]...))
		offset += size
	}

	return segments
}

func oggChecksum(page []byte) uint32 {
	table := generateChecksumTable()
	var checksum uint32
	for index, value := range page {
		if index >= 22 && index < 26 {
			value = 0
		}
		checksum = (checksum << 8) ^ table[byte(checksum>>24)^value]
	}

	return checksum
}
