// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package opus provides a Opus Audio Codec RFC 6716 implementation
package opus

import (
	"errors"
	"fmt"
	"math"

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
	silkDecoder            silk.Decoder
	silkBuffer             []float32
	resampleBuffer         []float32
	resampleDelayBuffer    []float32
	resampleDelayBandwidth Bandwidth
	silkResampler          [2]resample.Silk48
	silkResamplerBandwidth Bandwidth
	silkResamplerChannels  int
	floatBuffer            []float32
	sampleRate             int
	channels               int
	lastPacketDuration     int
}

// NewDecoder creates a new Opus Decoder.
func NewDecoder(sampleRate int, channels int) (*Decoder, error) {
	decoder := &Decoder{
		silkDecoder: silk.NewDecoder(),
		silkBuffer:  make([]float32, maxSilkFrameSampleCount),
	}

	if err := decoder.Init(sampleRate, channels); err != nil {
		return nil, err
	}

	return decoder, nil
}

// Init initializes a pre-allocated Opus decoder.
func (d *Decoder) Init(sampleRate int, channels int) error {
	switch sampleRate {
	case 8000, 12000, 16000, 24000, 48000:
	default:
		return errInvalidSampleRate
	}
	switch channels {
	case 1, 2:
	default:
		return errInvalidChannelCount
	}

	d.sampleRate = sampleRate
	d.channels = channels
	d.lastPacketDuration = 0
	d.silkDecoder = silk.NewDecoder()
	d.resampleDelayBuffer = nil
	d.resampleDelayBandwidth = 0
	d.silkResampler = [2]resample.Silk48{}
	d.silkResamplerBandwidth = 0
	d.silkResamplerChannels = 0

	return nil
}

// RFC 6716 Section 4.2.9 resamples SILK's internal 8/12/16 kHz output to
// the decoder API's 48 kHz output domain.
func (b Bandwidth) resampleCount() int {
	sampleRate := b.SampleRate()
	if sampleRate == 0 {
		return 0
	}

	return BandwidthFullband.SampleRate() / sampleRate
}

func (c Configuration) silkFrameSampleCount() int {
	switch c.bandwidth() {
	case BandwidthNarrowband:
		return 8 * c.frameDuration().nanoseconds() / 1000000
	case BandwidthMediumband:
		return 12 * c.frameDuration().nanoseconds() / 1000000
	case BandwidthWideband:
		return 16 * c.frameDuration().nanoseconds() / 1000000
	case BandwidthSuperwideband, BandwidthFullband:
		return 0
	default:
		return 0
	}
}

// RFC 6716 Section 4.2.9 defines the normative delay for SILK's resampler
// for each decoded bandwidth. The values below are the corresponding
// whole-sample delays at each supported output rate.
func (b Bandwidth) resampleDelay(sampleRate int) int { //nolint:cyclop
	switch b {
	case BandwidthNarrowband:
		switch sampleRate {
		case 8000:
			return 4
		case 12000:
			return 6
		case 16000:
			return 9
		case 24000:
			return 13
		case 48000:
			return 26
		}
	case BandwidthMediumband:
		switch sampleRate {
		case 8000:
			return 6
		case 12000:
			return 9
		case 16000:
			return 11
		case 24000:
			return 17
		case 48000:
			return 33
		}
	case BandwidthWideband:
		switch sampleRate {
		case 8000:
			return 6
		case 12000:
			return 8
		case 16000:
			return 12
		case 24000:
			return 17
		case 48000:
			return 34
		}
	case BandwidthSuperwideband, BandwidthFullband:
		return 0
	}

	return 0
}

// applyResampleDelay applies the normative SILK resampler delay from RFC 6716
// Section 4.2.9. The resampling algorithm itself is not normative, but the
// delay is.
func (d *Decoder) applyResampleDelay(out []float32, sampleCount int, bandwidth Bandwidth) {
	resampleDelay := bandwidth.resampleDelay(d.sampleRate) * d.channels
	if resampleDelay == 0 {
		return
	}
	if d.resampleDelayBandwidth != bandwidth || len(d.resampleDelayBuffer) != resampleDelay {
		d.resampleDelayBuffer = make([]float32, resampleDelay)
		d.resampleDelayBandwidth = bandwidth
	}

	previousDelay := make([]float32, resampleDelay)
	copy(previousDelay, d.resampleDelayBuffer)
	copy(d.resampleDelayBuffer, out[sampleCount-resampleDelay:sampleCount])
	copy(out[resampleDelay:sampleCount], out[:sampleCount-resampleDelay])
	copy(out[:resampleDelay], previousDelay)
}

// resampleTo48 uses the RFC 6716 C reference's decoder-side UF resampler path
// for SILK 8/12/16 kHz to 48 kHz output.
func (d *Decoder) resampleTo48(in, out []float32, channelCount int, bandwidth Bandwidth) error {
	if d.silkResamplerBandwidth != bandwidth {
		for i := range d.silkResampler {
			if err := d.silkResampler[i].Init(bandwidth.SampleRate()); err != nil {
				return err
			}
		}
		d.silkResamplerBandwidth = bandwidth
		d.silkResamplerChannels = channelCount
	}
	if channelCount == 2 && d.silkResamplerChannels == 1 {
		d.silkResampler[1] = d.silkResampler[0]
	}
	d.silkResamplerChannels = channelCount

	samplesPerChannel := len(in) / channelCount
	resampledSamplesPerChannel := samplesPerChannel * bandwidth.resampleCount()
	for c := 0; c < channelCount; c++ {
		channelIn := make([]float32, samplesPerChannel)
		channelOut := make([]float32, resampledSamplesPerChannel)
		for i := 0; i < samplesPerChannel; i++ {
			channelIn[i] = in[(i*channelCount)+c]
		}
		if err := d.silkResampler[c].Resample(channelIn, channelOut); err != nil {
			return err
		}
		for i := 0; i < resampledSamplesPerChannel; i++ {
			out[(i*channelCount)+c] = channelOut[i]
		}
	}

	return nil
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

//nolint:cyclop
func (d *Decoder) decode(
	in []byte,
	out []float32,
	decodeFEC bool,
) (bandwidth Bandwidth, isStereo bool, sampleCount int, err error) {
	if len(in) < 1 {
		return 0, false, 0, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	encodedFrames, err := parsePacketFrames(in, tocHeader)
	if err != nil {
		return 0, false, 0, err
	}

	if cfg.mode() != configurationModeSilkOnly {
		return 0, false, 0, fmt.Errorf("%w: %d", errUnsupportedConfigurationMode, cfg.mode())
	}

	frameSampleCount := cfg.silkFrameSampleCount()
	if tocHeader.isStereo() {
		frameSampleCount *= 2
	}
	requiredSamples := frameSampleCount * len(encodedFrames)
	if cap(out) < requiredSamples {
		d.silkBuffer = make([]float32, requiredSamples)
		out = d.silkBuffer
	}
	out = out[:requiredSamples]
	for i := range out {
		out[i] = 0
	}

	for i, encodedFrame := range encodedFrames {
		frameOut := out[i*frameSampleCount : (i+1)*frameSampleCount]
		if decodeFEC {
			err = d.silkDecoder.DecodeFEC(
				encodedFrame,
				frameOut,
				tocHeader.isStereo(),
				cfg.frameDuration().nanoseconds(),
				silk.Bandwidth(cfg.bandwidth()),
			)
		} else {
			err = d.silkDecoder.Decode(
				encodedFrame,
				frameOut,
				tocHeader.isStereo(),
				cfg.frameDuration().nanoseconds(),
				silk.Bandwidth(cfg.bandwidth()),
			)
		}
		if err != nil {
			return 0, false, 0, err
		}

		if decodeFEC {
			continue
		}

		// RFC 6716 Sections 4.5.1.1 and 4.5.1.4 allow SILK-only frames to
		// carry a trailing redundant low-band CELT frame when at least 17 bits
		// remain after SILK decode.
		if d.silkDecoder.Tell()+17 <= len(encodedFrame)*8 {
			return 0, false, 0, errUnsupportedSilkRedundancy
		}
	}

	sampleCount = requiredSamples

	return cfg.bandwidth(), tocHeader.isStereo(), sampleCount, nil
}

func (d *Decoder) decodeToFloat32(in []byte, out []float32, decodeFEC bool) (int, error) { //nolint:cyclop
	if d.sampleRate == 0 {
		return 0, errInvalidSampleRate
	}
	if d.channels == 0 {
		return 0, errInvalidChannelCount
	}

	bandwidth, isStereo, sampleCount, err := d.decode(in, d.silkBuffer, decodeFEC)
	if err != nil {
		return 0, err
	}

	channelCount := 1
	if isStereo {
		channelCount = 2
	}

	resampleCount := bandwidth.resampleCount()
	requiredSamples := sampleCount * resampleCount
	if cap(d.resampleBuffer) < requiredSamples {
		d.resampleBuffer = make([]float32, requiredSamples)
	}
	d.resampleBuffer = d.resampleBuffer[:requiredSamples]
	if d.sampleRate == BandwidthFullband.SampleRate() {
		if err = d.resampleTo48(d.silkBuffer[:sampleCount], d.resampleBuffer, channelCount, bandwidth); err != nil {
			return 0, err
		}
	} else {
		if err = resample.Up(d.silkBuffer[:sampleCount], d.resampleBuffer, channelCount, resampleCount); err != nil {
			return 0, err
		}
	}

	downsampleCount := BandwidthFullband.SampleRate() / d.sampleRate
	if downsampleCount <= 0 || BandwidthFullband.SampleRate()%d.sampleRate != 0 {
		return 0, errInvalidSampleRate
	}
	if len(d.resampleBuffer)%(channelCount*downsampleCount) != 0 {
		return 0, errMalformedPacket
	}

	samplesPerChannel := len(d.resampleBuffer) / channelCount / downsampleCount
	if len(out) < samplesPerChannel*d.channels {
		return 0, errOutBufferTooSmall
	}

	outIndex := 0
	for i := 0; i < len(d.resampleBuffer); i += channelCount * downsampleCount {
		switch {
		case channelCount == d.channels:
			for c := 0; c < d.channels; c++ {
				out[outIndex] = d.resampleBuffer[i+c]
				outIndex++
			}
		case channelCount == 1 && d.channels == 2:
			out[outIndex] = d.resampleBuffer[i]
			out[outIndex+1] = d.resampleBuffer[i]
			outIndex += 2
		case channelCount == 2 && d.channels == 1:
			out[outIndex] = (d.resampleBuffer[i] + d.resampleBuffer[i+1]) / 2
			outIndex++
		}
	}

	if d.sampleRate != BandwidthFullband.SampleRate() {
		d.applyResampleDelay(out, outIndex, bandwidth)
	}

	d.lastPacketDuration = samplesPerChannel

	return samplesPerChannel, nil
}

func float32ToInt16(in []float32, out []int16, sampleCount int) {
	for i := 0; i < sampleCount; i++ {
		sample := math.Floor(float64(in[i] * 32767))
		sample = math.Max(sample, -32768)
		sample = math.Min(sample, 32767)
		out[i] = int16(sample)
	}
}

// Decode decodes Opus data into signed 16-bit PCM.
func (d *Decoder) Decode(in []byte, out []int16) (int, error) {
	if cap(d.floatBuffer) < len(out) {
		d.floatBuffer = make([]float32, len(out))
	}
	d.floatBuffer = d.floatBuffer[:len(out)]

	sampleCount, err := d.decodeToFloat32(in, d.floatBuffer, false)
	if err != nil {
		return 0, err
	}

	float32ToInt16(d.floatBuffer, out, sampleCount*d.channels)

	return sampleCount, nil
}

// DecodeFloat32 decodes Opus data into float32 PCM.
func (d *Decoder) DecodeFloat32(in []byte, out []float32) (int, error) {
	return d.decodeToFloat32(in, out, false)
}

// DecodeFEC decodes Opus in-band FEC data into signed 16-bit PCM.
func (d *Decoder) DecodeFEC(in []byte, out []int16) error {
	if d.lastPacketDuration == 0 {
		return errNoLastPacketDuration
	}
	lastPacketDuration := d.lastPacketDuration
	if len(out) != lastPacketDuration*d.channels {
		return errInvalidPacketDuration
	}

	if cap(d.floatBuffer) < len(out) {
		d.floatBuffer = make([]float32, len(out))
	}
	d.floatBuffer = d.floatBuffer[:len(out)]

	sampleCount, err := d.decodeToFloat32(in, d.floatBuffer, true)
	if err != nil {
		if errors.Is(err, silk.ErrNoLowBitrateRedundancy) {
			return d.DecodePLC(out)
		}

		return err
	}
	if sampleCount != lastPacketDuration {
		d.lastPacketDuration = lastPacketDuration

		return errInvalidPacketDuration
	}

	float32ToInt16(d.floatBuffer, out, sampleCount*d.channels)

	return nil
}

// DecodeFECFloat32 decodes Opus in-band FEC data into float32 PCM.
func (d *Decoder) DecodeFECFloat32(in []byte, out []float32) error {
	if d.lastPacketDuration == 0 {
		return errNoLastPacketDuration
	}
	lastPacketDuration := d.lastPacketDuration
	if len(out) != lastPacketDuration*d.channels {
		return errInvalidPacketDuration
	}

	sampleCount, err := d.decodeToFloat32(in, out, true)
	if err != nil {
		if errors.Is(err, silk.ErrNoLowBitrateRedundancy) {
			return d.DecodePLCFloat32(out)
		}

		return err
	}
	if sampleCount != lastPacketDuration {
		d.lastPacketDuration = lastPacketDuration

		return errInvalidPacketDuration
	}

	return nil
}

// DecodePLC fills a lost packet with silence as signed 16-bit PCM.
func (d *Decoder) DecodePLC(out []int16) error {
	if d.lastPacketDuration == 0 {
		return errNoLastPacketDuration
	}
	if len(out) != d.lastPacketDuration*d.channels {
		return errInvalidPacketDuration
	}
	for i := range out {
		out[i] = 0
	}

	return nil
}

// DecodePLCFloat32 fills a lost packet with silence as float32 PCM.
func (d *Decoder) DecodePLCFloat32(out []float32) error {
	if d.lastPacketDuration == 0 {
		return errNoLastPacketDuration
	}
	if len(out) != d.lastPacketDuration*d.channels {
		return errInvalidPacketDuration
	}
	for i := range out {
		out[i] = 0
	}

	return nil
}

// LastPacketDuration returns the duration of the last decoded packet in samples per channel.
func (d *Decoder) LastPacketDuration() (int, error) {
	if d.lastPacketDuration == 0 {
		return 0, errNoLastPacketDuration
	}

	return d.lastPacketDuration, nil
}

// PrevSignalType returns the previous decoded signal type.
func (d *Decoder) PrevSignalType() (int, error) {
	return 0, errUnsupportedSignalType
}
