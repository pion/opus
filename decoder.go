// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package opus provides a Opus Audio Codec RFC 6716 implementation
package opus

import (
	"fmt"

	"github.com/pion/opus/internal/bitdepth"
	silkresample "github.com/pion/opus/internal/resample/silk"
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
	resampleChannelIn      [2][]float32
	resampleChannelOut     [2][]float32
	silkResampler          [2]silkresample.Resampler
	silkResamplerBandwidth Bandwidth
	silkResamplerChannels  int
	floatBuffer            []float32
	sampleRate             int
	channels               int
}

// NewDecoder creates a new Opus Decoder.
func NewDecoder() Decoder {
	decoder, _ := NewDecoderWithOutput(BandwidthFullband.SampleRate(), 1)

	return decoder
}

// NewDecoderWithOutput creates a new Opus Decoder with the requested output sample rate and channel count.
func NewDecoderWithOutput(sampleRate, channels int) (Decoder, error) {
	decoder := Decoder{
		silkDecoder: silk.NewDecoder(),
		silkBuffer:  make([]float32, maxSilkFrameSampleCount),
	}
	if err := decoder.Init(sampleRate, channels); err != nil {
		return Decoder{}, err
	}

	return decoder, nil
}

// Init initializes a pre-allocated Opus decoder.
func (d *Decoder) Init(sampleRate, channels int) error {
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
	d.silkDecoder = silk.NewDecoder()
	d.silkResampler = [2]silkresample.Resampler{}
	d.silkResamplerBandwidth = 0
	d.silkResamplerChannels = 0

	return nil
}

// resampleSilk uses the RFC 6716 C reference's decoder-side SILK resampler.
func (d *Decoder) resampleSilk(in, out []float32, channelCount int, bandwidth Bandwidth) error {
	if err := d.initSilkResampler(channelCount, bandwidth); err != nil {
		return err
	}

	samplesPerChannel := len(in) / channelCount
	resampledSamplesPerChannel := samplesPerChannel * d.sampleRate / bandwidth.SampleRate()
	for channelIndex := range channelCount {
		if err := d.resampleSilkChannel(
			in,
			out,
			channelIndex,
			channelCount,
			samplesPerChannel,
			resampledSamplesPerChannel,
		); err != nil {
			return err
		}
	}

	return nil
}

func (d *Decoder) initSilkResampler(channelCount int, bandwidth Bandwidth) error {
	if d.silkResamplerBandwidth != bandwidth {
		for i := range d.silkResampler {
			if err := d.silkResampler[i].Init(bandwidth.SampleRate(), d.sampleRate); err != nil {
				return err
			}
		}
		d.silkResamplerBandwidth = bandwidth
		d.silkResamplerChannels = channelCount
	}
	if channelCount == 2 && d.silkResamplerChannels == 1 {
		d.silkResampler[1].CopyStateFrom(&d.silkResampler[0])
	}
	d.silkResamplerChannels = channelCount

	return nil
}

func (d *Decoder) resampleSilkChannel(
	in, out []float32,
	channelIndex, channelCount, samplesPerChannel, resampledSamplesPerChannel int,
) error {
	if cap(d.resampleChannelIn[channelIndex]) < samplesPerChannel {
		d.resampleChannelIn[channelIndex] = make([]float32, samplesPerChannel)
	}
	if cap(d.resampleChannelOut[channelIndex]) < resampledSamplesPerChannel {
		d.resampleChannelOut[channelIndex] = make([]float32, resampledSamplesPerChannel)
	}
	channelIn := d.resampleChannelIn[channelIndex][:samplesPerChannel]
	channelOut := d.resampleChannelOut[channelIndex][:resampledSamplesPerChannel]
	for i := range samplesPerChannel {
		channelIn[i] = in[(i*channelCount)+channelIndex]
	}
	if err := d.silkResampler[channelIndex].Resample(channelIn, channelOut); err != nil {
		return err
	}
	for i := range resampledSamplesPerChannel {
		out[(i*channelCount)+channelIndex] = channelOut[i]
	}

	return nil
}

func (c Configuration) silkFrameSampleCount() int {
	if c.mode() != configurationModeSilkOnly {
		return 0
	}

	switch c.bandwidth() {
	case BandwidthNarrowband:
		return 8 * c.frameDuration().nanoseconds() / 1000000
	case BandwidthMediumband:
		return 12 * c.frameDuration().nanoseconds() / 1000000
	case BandwidthWideband:
		return 16 * c.frameDuration().nanoseconds() / 1000000
	case BandwidthSuperwideband, BandwidthFullband:
		return 0
	}

	return 0
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
	for range int(frameCount) {
		frames = append(frames, in[offset:offset+frameSize])
		offset += frameSize
	}

	return frames, nil
}

func parsePacketFramesCode3VBR(in []byte, offset, payloadEnd int, frameCount byte) ([][]byte, error) {
	frameSizes := make([]int, 0, frameCount)
	for range int(frameCount) - 1 {
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

func (d *Decoder) decode(in []byte, out []float32) (bandwidth Bandwidth, isStereo bool, sampleCount int, err error) {
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
		err := d.silkDecoder.Decode(
			encodedFrame,
			frameOut,
			tocHeader.isStereo(),
			cfg.frameDuration().nanoseconds(),
			silk.Bandwidth(cfg.bandwidth()),
		)
		if err != nil {
			return 0, false, 0, err
		}
	}

	sampleCount = requiredSamples

	return cfg.bandwidth(), tocHeader.isStereo(), sampleCount, nil
}

func (d *Decoder) decodeToFloat32(
	in []byte,
	out []float32,
) (samplesPerChannel int, bandwidth Bandwidth, isStereo bool, err error) {
	if d.sampleRate == 0 {
		return 0, 0, false, errInvalidSampleRate
	}
	if d.channels == 0 {
		return 0, 0, false, errInvalidChannelCount
	}

	bandwidth, isStereo, sampleCount, err := d.decode(in, d.silkBuffer)
	if err != nil {
		return 0, 0, false, err
	}

	channelCount := 1
	if isStereo {
		channelCount = 2
	}

	samplesPerChannel = (sampleCount / channelCount) * d.sampleRate / bandwidth.SampleRate()
	requiredSamples := samplesPerChannel * channelCount
	if cap(d.resampleBuffer) < requiredSamples {
		d.resampleBuffer = make([]float32, requiredSamples)
	}
	d.resampleBuffer = d.resampleBuffer[:requiredSamples]
	if err = d.resampleSilk(d.silkBuffer[:sampleCount], d.resampleBuffer, channelCount, bandwidth); err != nil {
		return 0, 0, false, err
	}

	if len(out) < samplesPerChannel*d.channels {
		return 0, 0, false, errOutBufferTooSmall
	}

	d.copyResampledSamples(out, channelCount)

	return samplesPerChannel, bandwidth, isStereo, nil
}

func (d *Decoder) copyResampledSamples(out []float32, channelCount int) {
	outIndex := 0
	for i := 0; i < len(d.resampleBuffer); i += channelCount {
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
}

func float32ToInt16(in []float32, out []int16, sampleCount int) {
	for i := range sampleCount {
		out[i] = bitdepth.Float32ToSigned16(in[i])
	}
}

// Decode decodes the Opus bitstream into S16LE PCM.
func (d *Decoder) Decode(in, out []byte) (bandwidth Bandwidth, isStereo bool, err error) {
	if cap(d.floatBuffer) < len(out)/2 {
		d.floatBuffer = make([]float32, len(out)/2)
	}
	d.floatBuffer = d.floatBuffer[:len(out)/2]

	sampleCount, bandwidth, isStereo, err := d.decodeToFloat32(in, d.floatBuffer)
	if err != nil {
		return
	}

	err = bitdepth.ConvertFloat32LittleEndianToSigned16LittleEndian(
		d.floatBuffer[:sampleCount*d.channels],
		out,
		d.channels,
		1,
	)

	return
}

// DecodeFloat32 decodes the Opus bitstream into F32LE PCM.
func (d *Decoder) DecodeFloat32(in []byte, out []float32) (bandwidth Bandwidth, isStereo bool, err error) {
	_, bandwidth, isStereo, err = d.decodeToFloat32(in, out)

	return
}

// DecodeToInt16 decodes Opus data into signed 16-bit PCM and returns the sample count per channel.
func (d *Decoder) DecodeToInt16(in []byte, out []int16) (int, error) {
	if cap(d.floatBuffer) < len(out) {
		d.floatBuffer = make([]float32, len(out))
	}
	d.floatBuffer = d.floatBuffer[:len(out)]

	sampleCount, _, _, err := d.decodeToFloat32(in, d.floatBuffer)
	if err != nil {
		return 0, err
	}

	float32ToInt16(d.floatBuffer, out, sampleCount*d.channels)

	return sampleCount, nil
}

// DecodeToFloat32 decodes Opus data into float32 PCM and returns the sample count per channel.
func (d *Decoder) DecodeToFloat32(in []byte, out []float32) (int, error) {
	sampleCount, _, _, err := d.decodeToFloat32(in, out)
	if err != nil {
		return 0, err
	}

	return sampleCount, nil
}
