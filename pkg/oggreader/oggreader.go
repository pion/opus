// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package oggreader implements the Ogg media container reader
package oggreader

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	pageHeaderTypeContinuedPacket   = 0x01
	pageHeaderTypeBeginningOfStream = 0x02
	pageHeaderSignature             = "OggS"
	pageChecksumOffset              = 22
	pageSegmentCountOffset          = 26

	idPageSignature = "OpusHead"

	pageHeaderLen           = 27
	idPagePayloadLength     = 19
	idPageStreamCountIndex  = 19
	idPageCoupledCountIndex = 20
	idPageMappingIndex      = 21

	maxPageSegmentSize = 255
)

var (
	errNilStream                 = errors.New("stream is nil")
	errBadIDPageSignature        = errors.New("bad header signature")
	errBadIDPageType             = errors.New("wrong header, expected beginning of stream")
	errBadIDPageLength           = errors.New("payload for id page has invalid length")
	errBadIDPagePayloadSignature = errors.New("bad payload signature")
	errShortPageHeader           = errors.New("not enough data for payload header")
	errChecksumMismatch          = errors.New("expected and actual checksum do not match")
)

// OggReader is used to read Ogg files and return page payloads.
type OggReader struct {
	stream               io.Reader
	bytesReadSuccesfully int64
	checksumTable        *[256]uint32
	doChecksum           bool
	packetState          *oggPacketState
	channelMapping       *OggChannelMapping
}

// OggHeader is the metadata from the first two pages
// in the file (ID and Comment)
//
// https://tools.ietf.org/html/rfc7845.html#section-3
type OggHeader struct {
	ChannelMap uint8
	Channels   uint8
	OutputGain uint16
	PreSkip    uint16
	SampleRate uint32
	Version    uint8
}

// OggChannelMapping carries extended multistream metadata from the Opus ID header.
type OggChannelMapping struct {
	StreamCount    uint8
	CoupledCount   uint8
	Mapping        []uint8
	DemixingMatrix []int16
}

// OggPageHeader is the metadata for a Page
// Pages are the fundamental unit of multiplexing in an Ogg stream
//
// https://tools.ietf.org/html/rfc7845.html#section-1
type OggPageHeader struct {
	GranulePosition uint64

	sig           [4]byte
	version       uint8
	headerType    uint8
	serial        uint32
	index         uint32
	segmentsCount uint8
}

type oggPacket struct {
	data   []byte
	header *OggPageHeader
}

type oggPacketState struct {
	packetQueue         []oggPacket
	partialPacket       []byte
	discardingPacket    bool
	lastPacketPageIndex uint32
	hasLastPacketPage   bool
}

// NewWith returns a new Ogg reader and Ogg header
// with an io.Reader input.
func NewWith(in io.Reader) (*OggReader, *OggHeader, error) {
	return newWith(in /* doChecksum */, true)
}

func newWith(in io.Reader, doChecksum bool) (*OggReader, *OggHeader, error) {
	if in == nil {
		return nil, nil, errNilStream
	}

	reader := &OggReader{
		stream:        in,
		checksumTable: generateChecksumTable(),
		doChecksum:    doChecksum,
		packetState:   &oggPacketState{},
	}

	header, err := reader.readHeaders()
	if err != nil {
		return nil, nil, err
	}

	return reader, header, nil
}

func (o *OggReader) readHeaders() (*OggHeader, error) {
	packet, pageHeader, err := o.ParseNextPacket()
	if err != nil {
		return nil, err
	}

	header, err := parseIDHeader(pageHeader, packet)
	if err != nil {
		return nil, err
	}

	if err = o.parseChannelMapping(header, packet); err != nil {
		return nil, err
	}

	return header, nil
}

// ChannelMapping returns a copy of the parsed multistream channel mapping metadata.
func (o *OggReader) ChannelMapping() OggChannelMapping {
	if o.channelMapping == nil {
		return OggChannelMapping{}
	}

	return OggChannelMapping{
		StreamCount:    o.channelMapping.StreamCount,
		CoupledCount:   o.channelMapping.CoupledCount,
		Mapping:        append([]uint8(nil), o.channelMapping.Mapping...),
		DemixingMatrix: append([]int16(nil), o.channelMapping.DemixingMatrix...),
	}
}

// ParseNextPage reads from stream and returns Ogg page segments, header,
// and an error if there is incomplete page data.
//
// ParseNextPage and ParseNextPacket are alternative read paths and should not
// be mixed on the same OggReader.
func (o *OggReader) ParseNextPage() ([][]byte, *OggPageHeader, error) {
	segments, _, pageHeader, err := o.parseNextPageData()
	if err != nil {
		return nil, nil, err
	}

	o.markPacketPageRead(pageHeader)

	return segments, pageHeader, nil
}

// ParseNextPacket reads from stream and returns one logical Ogg packet.
//
// ParseNextPage and ParseNextPacket are alternative read paths and should not
// be mixed on the same OggReader.
func (o *OggReader) ParseNextPacket() ([]byte, *OggPageHeader, error) {
	packetState := o.getPacketState()
	if len(packetState.packetQueue) > 0 {
		packet := packetState.packetQueue[0]
		packetState.packetQueue = packetState.packetQueue[1:]

		return packet.data, packet.header, nil
	}

	for {
		segments, sizeBuffer, pageHeader, err := o.parseNextPageData()
		if err != nil {
			if errors.Is(err, io.EOF) && len(packetState.partialPacket) != 0 {
				return nil, nil, io.ErrUnexpectedEOF
			}

			return nil, nil, err
		}

		if err = o.queuePagePackets(segments, sizeBuffer, pageHeader); err != nil {
			return nil, nil, err
		}
		if len(packetState.packetQueue) > 0 {
			packet := packetState.packetQueue[0]
			packetState.packetQueue = packetState.packetQueue[1:]

			return packet.data, packet.header, nil
		}
	}
}

func (o *OggReader) parseNextPageData() ([][]byte, []byte, *OggPageHeader, error) {
	header := make([]byte, pageHeaderLen)

	n, err := io.ReadFull(o.stream, header)
	if err != nil {
		return nil, nil, nil, err
	} else if n < len(header) {
		return nil, nil, nil, errShortPageHeader
	}

	pageHeader := &OggPageHeader{
		sig: [4]byte{header[0], header[1], header[2], header[3]},
	}

	pageHeader.version = header[4]
	pageHeader.headerType = header[5]
	pageHeader.GranulePosition = binary.LittleEndian.Uint64(header[6 : 6+8])
	pageHeader.serial = binary.LittleEndian.Uint32(header[14 : 14+4])
	pageHeader.index = binary.LittleEndian.Uint32(header[18 : 18+4])
	pageHeader.segmentsCount = header[pageSegmentCountOffset]

	sizeBuffer := make([]byte, pageHeader.segmentsCount)
	if _, err = io.ReadFull(o.stream, sizeBuffer); err != nil {
		return nil, nil, nil, err
	}

	segments := [][]byte{}

	for _, s := range sizeBuffer {
		segment := make([]byte, int(s))
		if _, err = io.ReadFull(o.stream, segment); err != nil {
			return nil, nil, nil, err
		}

		segments = append(segments, segment)
	}

	if o.doChecksum {
		if err = o.validateChecksum(header, sizeBuffer, segments); err != nil {
			return nil, nil, nil, err
		}
	}

	return segments, sizeBuffer, pageHeader, nil
}

func (o *OggReader) queuePagePackets(segments [][]byte, sizeBuffer []byte, pageHeader *OggPageHeader) error {
	packetState := o.getPacketState()
	if err := o.preparePacketQueue(pageHeader); err != nil {
		return err
	}

	for i, segment := range segments {
		segmentSize := sizeBuffer[i]
		if packetState.discardingPacket {
			packetState.finishDiscard(segmentSize)

			continue
		}

		packetState.partialPacket = append(packetState.partialPacket, segment...)
		if segmentSize == maxPageSegmentSize {
			continue
		}

		o.enqueuePacket(pageHeader, packetState.partialPacket)
		packetState.partialPacket = nil
	}

	o.markPacketPageRead(pageHeader)

	return nil
}

func (o *OggReader) markPacketPageRead(pageHeader *OggPageHeader) {
	if pageHeader.segmentsCount == 0 {
		return
	}

	packetState := o.getPacketState()
	packetState.lastPacketPageIndex = pageHeader.index
	packetState.hasLastPacketPage = true
}

func (o *OggReader) validateChecksum(header, sizeBuffer []byte, segments [][]byte) error {
	var checksum uint32
	updateChecksum := func(v byte) {
		checksum = (checksum << 8) ^ o.checksumTable[byte(checksum>>24)^v]
	}

	for index := range header {
		// Don't include expected checksum in our generation
		if index >= pageChecksumOffset && index < pageSegmentCountOffset {
			updateChecksum(0)

			continue
		}

		updateChecksum(header[index])
	}
	for _, s := range sizeBuffer {
		updateChecksum(s)
	}

	for i := range segments {
		for index := range segments[i] {
			updateChecksum(segments[i][index])
		}
	}

	if binary.LittleEndian.Uint32(header[pageChecksumOffset:pageChecksumOffset+4]) != checksum {
		return errChecksumMismatch
	}

	return nil
}

// ResetReader resets the internal stream of OggReader. This is useful
// for live streams, where the end of the file might be read without the
// data being finished.
func (o *OggReader) ResetReader(reset func(bytesRead int64) io.Reader) {
	o.stream = reset(o.bytesReadSuccesfully)
}

func generateChecksumTable() *[256]uint32 {
	var table [256]uint32
	const poly = 0x04c11db7

	for tableIndex := range uint32(256) {
		r := tableIndex << 24

		for range 8 {
			if (r & 0x80000000) != 0 {
				r = (r << 1) ^ poly
			} else {
				r <<= 1
			}
			table[tableIndex] = r
		}
	}

	return &table
}

func parseIDHeader(pageHeader *OggPageHeader, packet []byte) (*OggHeader, error) {
	if string(pageHeader.sig[:]) != pageHeaderSignature {
		return nil, errBadIDPageSignature
	}

	if pageHeader.headerType != pageHeaderTypeBeginningOfStream {
		return nil, errBadIDPageType
	}

	if len(packet) < idPagePayloadLength {
		return nil, errBadIDPageLength
	}

	if s := string(packet[:8]); s != idPageSignature {
		return nil, errBadIDPagePayloadSignature
	}

	return &OggHeader{
		Version:    packet[8],
		Channels:   packet[9],
		PreSkip:    binary.LittleEndian.Uint16(packet[10:12]),
		SampleRate: binary.LittleEndian.Uint32(packet[12:16]),
		OutputGain: binary.LittleEndian.Uint16(packet[16:18]),
		ChannelMap: packet[18],
	}, nil
}

func (o *OggReader) parseChannelMapping(header *OggHeader, packet []byte) error {
	switch header.ChannelMap {
	case 0:
		if len(packet) != idPagePayloadLength {
			return errBadIDPageLength
		}
		o.channelMapping = nil

		return nil
	case 1, 2, 255:
		return o.parseStreamChannelMapping(header, packet)
	case 3:
		return o.parseDemixingChannelMapping(header, packet)
	default:
		o.channelMapping = nil

		return nil
	}
}

func (o *OggReader) parseStreamChannelMapping(header *OggHeader, packet []byte) error {
	expectedLength := idPageMappingIndex + int(header.Channels)
	if len(packet) != expectedLength {
		return errBadIDPageLength
	}

	o.channelMapping = &OggChannelMapping{
		StreamCount:  packet[idPageStreamCountIndex],
		CoupledCount: packet[idPageCoupledCountIndex],
		Mapping:      append([]uint8(nil), packet[idPageMappingIndex:]...),
	}

	return nil
}

func (o *OggReader) parseDemixingChannelMapping(header *OggHeader, packet []byte) error {
	if len(packet) < idPageMappingIndex {
		return errBadIDPageLength
	}

	streamCount := packet[idPageStreamCountIndex]
	coupledCount := packet[idPageCoupledCountIndex]
	decodedChannels := int(streamCount) + int(coupledCount)
	expectedLength := idPageMappingIndex + (2 * int(header.Channels) * decodedChannels)
	if len(packet) != expectedLength {
		return errBadIDPageLength
	}

	o.channelMapping = &OggChannelMapping{
		StreamCount:    streamCount,
		CoupledCount:   coupledCount,
		DemixingMatrix: parseDemixingMatrix(packet[idPageMappingIndex:]),
	}

	return nil
}

func parseDemixingMatrix(in []byte) []int16 {
	matrix := make([]int16, len(in)/2)
	for i := range matrix {
		offset := i * 2
		matrix[i] = parseInt16LittleEndian(in[offset : offset+2])
	}

	return matrix
}

func parseInt16LittleEndian(in []byte) int16 {
	return int16(in[0]) | (int16(in[1]) << 8)
}

func (o *OggReader) getPacketState() *oggPacketState {
	if o.packetState == nil {
		o.packetState = &oggPacketState{}
	}

	return o.packetState
}

func (o *OggReader) preparePacketQueue(pageHeader *OggPageHeader) error {
	packetState := o.getPacketState()
	isContinuedPacket := (pageHeader.headerType & pageHeaderTypeContinuedPacket) != 0
	isConsecutivePage := !packetState.hasLastPacketPage || pageHeader.index == packetState.lastPacketPageIndex+1

	if packetState.discardingPacket && !isContinuedPacket {
		packetState.discardingPacket = false
	}

	if len(packetState.partialPacket) != 0 {
		if !isContinuedPacket || !isConsecutivePage {
			return io.ErrUnexpectedEOF
		}

		return nil
	}

	if isContinuedPacket {
		// RFC 7845 allows the first packet on an audio page to be a continuation
		// from data we do not have, such as when joining a live stream mid-broadcast.
		// Skip that incomplete packet and resume at the next packet boundary.
		packetState.discardingPacket = true
	}

	return nil
}

func (o *OggReader) enqueuePacket(pageHeader *OggPageHeader, packet []byte) {
	headerCopy := *pageHeader
	o.getPacketState().packetQueue = append(o.getPacketState().packetQueue, oggPacket{
		data:   append([]byte(nil), packet...),
		header: &headerCopy,
	})
}

func (s *oggPacketState) finishDiscard(segmentSize byte) {
	if segmentSize != maxPageSegmentSize {
		s.discardingPacket = false
	}
}
