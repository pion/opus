// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package opus provides a Opus Audio Codec RFC 6716 implementation
package opus

import (
	"fmt"

	"github.com/pion/opus/internal/bitdepth"
	"github.com/pion/opus/internal/celt"
	"github.com/pion/opus/internal/rangecoding"
	silkresample "github.com/pion/opus/internal/resample/silk"
	"github.com/pion/opus/internal/silk"
)

const (
	maxOpusFrameSize                = 1275
	maxOpusPacketDurationNanosecond = 120000000
	maxSilkFrameSampleCount         = 320
	maxCeltFrameSampleCount         = 960
	celtSampleRate                  = 48000
	hybridRedundantFrameSampleCount = celtSampleRate / 200
	hybridFadeSampleCount           = celtSampleRate / 400
)

// Decoder decodes the Opus bitstream into PCM.
type Decoder struct {
	silkDecoder            silk.Decoder
	silkBuffer             []float32
	celtDecoder            celt.Decoder
	celtBuffer             []float32
	rangeDecoder           rangecoding.Decoder
	rangeFinal             uint32
	previousMode           configurationMode
	previousRedundancy     bool
	resampleBuffer         []float32
	resampleChannelIn      [2][]float32
	resampleChannelOut     [2][]float32
	silkResampler          [2]silkresample.Resampler
	silkResamplerBandwidth Bandwidth
	silkResamplerChannels  int
	hybridSilkResampler    [2]silkresample.Resampler
	hybridSilkChannels     int
	silkRedundancyFades    []silkRedundancyFade
	silkCeltAdditions      []silkCeltAddition
	floatBuffer            []float32
	sampleRate             int
	channels               int
}

type silkRedundancyFade struct {
	celtToSilk       bool
	audio            []float32
	startSample      int
	frameSampleCount int
	channelCount     int
}

type silkCeltAddition struct {
	audio        []float32
	startSample  int
	channelCount int
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
		celtDecoder: celt.NewDecoder(),
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
	d.celtDecoder.Reset()
	d.celtBuffer = d.celtBuffer[:0]
	d.rangeDecoder = rangecoding.Decoder{}
	d.silkResampler = [2]silkresample.Resampler{}
	d.silkResamplerBandwidth = 0
	d.silkResamplerChannels = 0
	d.hybridSilkResampler = [2]silkresample.Resampler{}
	d.hybridSilkChannels = 0
	d.silkRedundancyFades = d.silkRedundancyFades[:0]
	d.silkCeltAdditions = d.silkCeltAdditions[:0]
	d.rangeFinal = 0
	d.previousMode = 0
	d.previousRedundancy = false

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

// resetModeState applies the decoder resets required by RFC 6716 Section 4.5.2
// before the first frame decoded in a new operating mode.
func (d *Decoder) resetModeState(mode configurationMode) {
	if d.previousMode == mode {
		return
	}

	switch mode {
	case configurationModeSilkOnly:
		if d.previousMode == configurationModeCELTOnly {
			d.silkDecoder = silk.NewDecoder()
		}
		if d.previousMode == configurationModeHybrid {
			d.copyHybridSilkResamplerToSilk()
		}
	case configurationModeCELTOnly:
		if !d.previousRedundancy {
			d.celtDecoder.Reset()
			clear(d.celtBuffer)
		}
	case configurationModeHybrid:
		if d.previousMode == configurationModeCELTOnly {
			d.silkDecoder = silk.NewDecoder()
			d.hybridSilkResampler = [2]silkresample.Resampler{}
			d.hybridSilkChannels = 0
		}
		if d.previousMode == configurationModeSilkOnly {
			d.copySilkResamplerToHybrid()
		}
	}
}

// copySilkResamplerToHybrid preserves the WB SILK resampler history across the
// normatively continuous WB SILK -> Hybrid transition in RFC 6716 Section 4.5.
func (d *Decoder) copySilkResamplerToHybrid() {
	if d.sampleRate != celtSampleRate || d.silkResamplerBandwidth != BandwidthWideband || d.silkResamplerChannels == 0 {
		return
	}
	for i := range d.hybridSilkResampler {
		d.hybridSilkResampler[i].CopyStateFrom(&d.silkResampler[i])
	}
	d.hybridSilkChannels = d.silkResamplerChannels
}

// copyHybridSilkResamplerToSilk preserves the same WB SILK history for the
// reverse Hybrid -> WB SILK transition described by RFC 6716 Section 4.5.
func (d *Decoder) copyHybridSilkResamplerToSilk() {
	if d.sampleRate != celtSampleRate || d.hybridSilkChannels == 0 {
		return
	}
	for i := range d.silkResampler {
		d.silkResampler[i].CopyStateFrom(&d.hybridSilkResampler[i])
	}
	d.silkResamplerBandwidth = BandwidthWideband
	d.silkResamplerChannels = d.hybridSilkChannels
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

func (c Configuration) celtFrameSampleCount() int {
	if c.mode() != configurationModeCELTOnly {
		return 0
	}
	if c.frameDuration() == frameDuration20ms {
		return maxCeltFrameSampleCount
	}

	return int(int64(c.frameDuration().nanoseconds()) * int64(celtSampleRate) / 1000000000)
}

func (c Configuration) hybridFrameSampleCount() int {
	if c.mode() != configurationModeHybrid {
		return 0
	}

	return int(int64(c.frameDuration().nanoseconds()) * int64(celtSampleRate) / 1000000000)
}

func (c Configuration) decodedSampleRate() int {
	switch c.mode() {
	case configurationModeSilkOnly:
		return c.bandwidth().SampleRate()
	case configurationModeCELTOnly, configurationModeHybrid:
		return celtSampleRate
	default:
		return 0
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

func (d *Decoder) decode(
	in []byte,
	out []float32,
) (bandwidth Bandwidth, isStereo bool, sampleCount int, decodedChannelCount int, err error) {
	if len(in) < 1 {
		return 0, false, 0, 0, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	encodedFrames, err := parsePacketFrames(in, tocHeader)
	if err != nil {
		return 0, false, 0, 0, err
	}

	switch cfg.mode() {
	case configurationModeSilkOnly:
		d.resetModeState(configurationModeSilkOnly)

		return d.decodeSilkFrames(cfg, tocHeader, encodedFrames, out)
	case configurationModeCELTOnly:
		d.resetModeState(configurationModeCELTOnly)

		return d.decodeCeltFrames(cfg, tocHeader, encodedFrames, out)
	case configurationModeHybrid:
		d.resetModeState(configurationModeHybrid)

		return d.decodeHybridFrames(cfg, tocHeader, encodedFrames, out)
	default:
		return 0, false, 0, 0, fmt.Errorf("%w: %d", errUnsupportedConfigurationMode, cfg.mode())
	}
}

// decodeCeltFrames decodes the CELT-only path at CELT's internal 48 kHz rate.
func (d *Decoder) decodeCeltFrames(
	cfg Configuration,
	tocHeader tableOfContentsHeader,
	encodedFrames [][]byte,
	out []float32,
) (bandwidth Bandwidth, isStereo bool, sampleCount int, decodedChannelCount int, err error) {
	frameSampleCount := cfg.celtFrameSampleCount()
	streamChannelCount := 1
	if tocHeader.isStereo() {
		streamChannelCount = 2
	}
	decodedChannelCount = d.channels
	if decodedChannelCount == 0 {
		decodedChannelCount = streamChannelCount
	}
	requiredSamples := frameSampleCount * len(encodedFrames) * decodedChannelCount
	if cap(out) < requiredSamples {
		d.silkBuffer = make([]float32, requiredSamples)
		out = d.silkBuffer
	}
	out = out[:requiredSamples]
	for i := range out {
		out[i] = 0
	}

	startBand, endBand, err := d.celtDecoder.Mode().BandRangeForSampleRate(cfg.bandwidth().SampleRate())
	if err != nil {
		return 0, false, 0, 0, err
	}
	frameOutputSamples := frameSampleCount * decodedChannelCount
	for i, encodedFrame := range encodedFrames {
		frameOut := out[i*frameOutputSamples : (i+1)*frameOutputSamples]
		if err = d.celtDecoder.Decode(
			encodedFrame,
			frameOut,
			tocHeader.isStereo(),
			decodedChannelCount,
			frameSampleCount,
			startBand,
			endBand,
		); err != nil {
			return 0, false, 0, 0, err
		}
		d.previousMode = configurationModeCELTOnly
		d.previousRedundancy = false
		if len(encodedFrame) <= 1 {
			d.rangeFinal = 0
		} else {
			d.rangeFinal = d.celtDecoder.FinalRange()
		}
	}

	return cfg.bandwidth(), tocHeader.isStereo(), requiredSamples, decodedChannelCount, nil
}

// decodeHybridFrames combines the SILK and CELT layers for Hybrid packets.
func (d *Decoder) decodeHybridFrames(
	cfg Configuration,
	tocHeader tableOfContentsHeader,
	encodedFrames [][]byte,
	out []float32,
) (bandwidth Bandwidth, isStereo bool, sampleCount int, decodedChannelCount int, err error) {
	frameSampleCount := cfg.hybridFrameSampleCount()
	streamChannelCount := 1
	if tocHeader.isStereo() {
		streamChannelCount = 2
	}
	decodedChannelCount = d.channels
	if decodedChannelCount == 0 {
		decodedChannelCount = streamChannelCount
	}
	requiredSamples := frameSampleCount * len(encodedFrames) * decodedChannelCount
	if cap(out) < requiredSamples {
		d.silkBuffer = make([]float32, requiredSamples)
		out = d.silkBuffer
	}
	out = out[:requiredSamples]
	for i := range out {
		out[i] = 0
	}

	startBand, endBand, err := d.celtDecoder.Mode().HybridBandRange(cfg.bandwidth().SampleRate())
	if err != nil {
		return 0, false, 0, 0, err
	}
	frameOutputSamples := frameSampleCount * decodedChannelCount
	silkSamplesPerChannel := frameSampleCount * BandwidthWideband.SampleRate() / celtSampleRate
	for i, encodedFrame := range encodedFrames {
		frameOut := out[i*frameOutputSamples : (i+1)*frameOutputSamples]
		if err = d.decodeHybridFrame(
			encodedFrame,
			frameOut,
			tocHeader.isStereo(),
			streamChannelCount,
			decodedChannelCount,
			frameSampleCount,
			silkSamplesPerChannel,
			cfg.frameDuration().nanoseconds(),
			startBand,
			endBand,
		); err != nil {
			return 0, false, 0, 0, err
		}
	}

	return cfg.bandwidth(), tocHeader.isStereo(), requiredSamples, decodedChannelCount, nil
}

type hybridRedundancy struct {
	present     bool
	celtToSilk  bool
	celtDataLen int
	endBand     int
	data        []byte
	audio       []float32
	rng         uint32
}

// decodeHybridFrame follows RFC 6716 Sections 4.5.1 and 4.5.2 for one Hybrid
// frame: decode shared-range SILK, split optional CELT redundancy, decode CELT,
// then apply the required transition cross-lap when redundancy is present.
//
//nolint:cyclop
func (d *Decoder) decodeHybridFrame(
	encodedFrame []byte,
	out []float32,
	isStereo bool,
	streamChannelCount int,
	outputChannelCount int,
	frameSampleCount int,
	silkSamplesPerChannel int,
	frameNanoseconds int,
	startBand int,
	endBand int,
) error {
	d.rangeDecoder.Init(encodedFrame)

	silkInternal := make([]float32, silkSamplesPerChannel*streamChannelCount)
	if err := d.silkDecoder.DecodeWithRange(
		&d.rangeDecoder,
		silkInternal,
		isStereo,
		frameNanoseconds,
		silk.Bandwidth(BandwidthWideband),
	); err != nil {
		return err
	}

	var err error
	redundancy := d.decodeHybridRedundancyHeader(encodedFrame)
	if redundancy.present && redundancy.celtToSilk {
		if err = d.decodeHybridRedundantFrame(&redundancy, isStereo, outputChannelCount, endBand); err != nil {
			return err
		}
	}
	if d.previousMode != configurationModeHybrid && d.previousMode != 0 && !d.previousRedundancy {
		d.celtDecoder.Reset()
		clear(d.celtBuffer)
	}
	if err = d.celtDecoder.DecodeWithRange(
		encodedFrame[:redundancy.celtDataLen],
		out,
		isStereo,
		outputChannelCount,
		frameSampleCount,
		startBand,
		endBand,
		&d.rangeDecoder,
	); err != nil {
		return err
	}

	silk48 := make([]float32, frameSampleCount*streamChannelCount)
	if err = d.resampleHybridSilkTo48(silkInternal, silk48, streamChannelCount); err != nil {
		return err
	}
	d.addHybridSilk(out, silk48, streamChannelCount, outputChannelCount, frameSampleCount)
	if redundancy.present && !redundancy.celtToSilk {
		d.celtDecoder.Reset()
		clear(d.celtBuffer)
		if err = d.decodeHybridRedundantFrame(&redundancy, isStereo, outputChannelCount, endBand); err != nil {
			return err
		}
		fadeStart := (frameSampleCount - hybridFadeSampleCount) * outputChannelCount
		redundantStart := hybridFadeSampleCount * outputChannelCount
		celt.SmoothFade(
			out[fadeStart:],
			redundancy.audio[redundantStart:],
			out[fadeStart:],
			hybridFadeSampleCount,
			outputChannelCount,
		)
	}
	if redundancy.present && redundancy.celtToSilk {
		for sample := range hybridFadeSampleCount {
			for channel := range outputChannelCount {
				index := sample*outputChannelCount + channel
				out[index] = redundancy.audio[index]
			}
		}
		fadeStart := hybridFadeSampleCount * outputChannelCount
		celt.SmoothFade(
			redundancy.audio[fadeStart:],
			out[fadeStart:],
			out[fadeStart:],
			hybridFadeSampleCount,
			outputChannelCount,
		)
	}
	if len(encodedFrame) <= 1 {
		d.rangeFinal = 0
	} else {
		d.rangeFinal = d.rangeDecoder.FinalRange() ^ redundancy.rng
	}
	d.previousMode = configurationModeHybrid
	d.previousRedundancy = redundancy.present && !redundancy.celtToSilk

	return nil
}

// decodeHybridRedundancyHeader parses the Hybrid transition side information
// from RFC 6716 Sections 4.5.1.1 through 4.5.1.3.
//
//nolint:gosec
func (d *Decoder) decodeHybridRedundancyHeader(
	encodedFrame []byte,
) hybridRedundancy {
	redundancy := hybridRedundancy{celtDataLen: len(encodedFrame)}
	if int(d.rangeDecoder.Tell())+17+20 > 8*len(encodedFrame) {
		return redundancy
	}
	if d.rangeDecoder.DecodeSymbolLogP(12) == 0 {
		return redundancy
	}

	celtToSilk := d.rangeDecoder.DecodeSymbolLogP(1) != 0
	redundancyBytesRaw, _ := d.rangeDecoder.DecodeUniform(256)
	redundancyBytes := int(redundancyBytesRaw) + 2
	redundancy.celtDataLen -= redundancyBytes
	if redundancy.celtDataLen < 0 || redundancy.celtDataLen*8 < int(d.rangeDecoder.Tell()) {
		return hybridRedundancy{celtDataLen: len(encodedFrame)}
	}
	d.rangeDecoder.SetStorageSize(redundancy.celtDataLen)
	redundancy.present = true
	redundancy.celtToSilk = celtToSilk
	redundancy.data = encodedFrame[redundancy.celtDataLen:]

	return redundancy
}

// decodeSilkOnlyRedundancyHeader parses the SILK-only variant of the transition
// side information defined by RFC 6716 Sections 4.5.1.1 and 4.5.1.2.
//
//nolint:gosec
func (d *Decoder) decodeSilkOnlyRedundancyHeader(
	encodedFrame []byte,
	bandwidth Bandwidth,
) (hybridRedundancy, error) {
	redundancy := hybridRedundancy{celtDataLen: len(encodedFrame)}
	if int(d.rangeDecoder.Tell())+17 > 8*len(encodedFrame) {
		return redundancy, nil
	}

	celtToSilk := d.rangeDecoder.DecodeSymbolLogP(1) != 0
	redundancyBytes := len(encodedFrame) - int((d.rangeDecoder.Tell()+7)>>3)
	redundancy.celtDataLen -= redundancyBytes
	if redundancyBytes <= 0 || redundancy.celtDataLen < 0 || redundancy.celtDataLen*8 < int(d.rangeDecoder.Tell()) {
		return hybridRedundancy{celtDataLen: len(encodedFrame)}, nil
	}
	d.rangeDecoder.SetStorageSize(redundancy.celtDataLen)

	endBand, err := d.celtEndBandForSilkBandwidth(bandwidth)
	if err != nil {
		return redundancy, err
	}
	redundancy.present = true
	redundancy.celtToSilk = celtToSilk
	redundancy.data = encodedFrame[redundancy.celtDataLen:]
	redundancy.endBand = endBand

	return redundancy, nil
}

// celtEndBandForSilkBandwidth selects the redundant CELT bandwidth required by
// RFC 6716 Section 4.5.1.4; MB SILK transitions use WB CELT bandwidth.
func (d *Decoder) celtEndBandForSilkBandwidth(bandwidth Bandwidth) (int, error) {
	sampleRate := bandwidth.SampleRate()
	if bandwidth == BandwidthMediumband {
		sampleRate = BandwidthWideband.SampleRate()
	}
	_, endBand, err := d.celtDecoder.Mode().BandRangeForSampleRate(sampleRate)

	return endBand, err
}

// decodeHybridRedundantFrame decodes the fixed 5 ms redundant CELT frame from
// RFC 6716 Section 4.5.1.4.
func (d *Decoder) decodeHybridRedundantFrame(
	redundancy *hybridRedundancy,
	isStereo bool,
	outputChannelCount int,
	endBand int,
) error {
	redundancy.audio = make([]float32, hybridRedundantFrameSampleCount*outputChannelCount)
	if err := d.celtDecoder.Decode(
		redundancy.data,
		redundancy.audio,
		isStereo,
		outputChannelCount,
		hybridRedundantFrameSampleCount,
		0,
		endBand,
	); err != nil {
		return err
	}
	redundancy.rng = d.celtDecoder.FinalRange()

	return nil
}

// resampleHybridSilkTo48 lifts the Hybrid packet's WB SILK layer to the 48 kHz
// CELT domain before the two layers are summed.
func (d *Decoder) resampleHybridSilkTo48(in []float32, out []float32, channelCount int) error {
	if d.hybridSilkChannels == 0 {
		for i := range d.hybridSilkResampler {
			if err := d.hybridSilkResampler[i].Init(BandwidthWideband.SampleRate(), celtSampleRate); err != nil {
				return err
			}
		}
	}
	if channelCount == 2 && d.hybridSilkChannels == 1 {
		d.hybridSilkResampler[1].CopyStateFrom(&d.hybridSilkResampler[0])
	}
	d.hybridSilkChannels = channelCount

	samplesPerChannel := len(in) / channelCount
	resampledSamplesPerChannel := len(out) / channelCount
	for channelIndex := range channelCount {
		if err := d.resampleHybridSilkChannel(
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

func (d *Decoder) resampleHybridSilkChannel(
	in []float32,
	out []float32,
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
	if err := d.hybridSilkResampler[channelIndex].Resample(channelIn, channelOut); err != nil {
		return err
	}
	for i := range resampledSamplesPerChannel {
		out[(i*channelCount)+channelIndex] = channelOut[i]
	}

	return nil
}

// addHybridSilk combines the decoded WB SILK contribution with the CELT layer
// after both are represented at 48 kHz.
func (d *Decoder) addHybridSilk(
	out []float32,
	silk48 []float32,
	streamChannelCount int,
	outputChannelCount int,
	samplesPerChannel int,
) {
	for i := range silk48 {
		silk48[i] = float32(bitdepth.Float32ToSigned16(silk48[i])) / 32768
	}
	for sample := range samplesPerChannel {
		silkIndex := sample * streamChannelCount
		outIndex := sample * outputChannelCount
		switch {
		case streamChannelCount == outputChannelCount:
			for channel := range outputChannelCount {
				out[outIndex+channel] += silk48[silkIndex+channel]
			}
		case streamChannelCount == 1 && outputChannelCount == 2:
			out[outIndex] += silk48[silkIndex]
			out[outIndex+1] += silk48[silkIndex]
		case streamChannelCount == 2 && outputChannelCount == 1:
			out[outIndex] += 0.5 * (silk48[silkIndex] + silk48[silkIndex+1])
		}
	}
}

// decodeSilkFrames handles ordinary SILK packets plus the redundant CELT side
// data that RFC 6716 Section 4.5.1 allows on mode transitions.
//
//nolint:cyclop
func (d *Decoder) decodeSilkFrames(
	cfg Configuration,
	tocHeader tableOfContentsHeader,
	encodedFrames [][]byte,
	out []float32,
) (bandwidth Bandwidth, isStereo bool, sampleCount int, decodedChannelCount int, err error) {
	frameSamplesPerChannel := cfg.silkFrameSampleCount()
	frameSampleCount := frameSamplesPerChannel
	decodedChannelCount = 1
	if tocHeader.isStereo() {
		frameSampleCount *= 2
		decodedChannelCount = 2
	}
	d.silkRedundancyFades = d.silkRedundancyFades[:0]
	d.silkCeltAdditions = d.silkCeltAdditions[:0]
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
		previousMode := d.previousMode
		previousRedundancy := d.previousRedundancy
		frameOut := out[i*frameSampleCount : (i+1)*frameSampleCount]
		d.rangeDecoder.Init(encodedFrame)
		err := d.silkDecoder.DecodeWithRange(
			&d.rangeDecoder,
			frameOut,
			tocHeader.isStereo(),
			cfg.frameDuration().nanoseconds(),
			silk.Bandwidth(cfg.bandwidth()),
		)
		if err != nil {
			return 0, false, 0, 0, err
		}
		redundancy, err := d.decodeSilkOnlyRedundancyHeader(encodedFrame, cfg.bandwidth())
		if err != nil {
			return 0, false, 0, 0, err
		}
		if redundancy.present {
			if !redundancy.celtToSilk {
				d.celtDecoder.Reset()
				clear(d.celtBuffer)
			}
			if err = d.decodeHybridRedundantFrame(
				&redundancy,
				tocHeader.isStereo(),
				decodedChannelCount,
				redundancy.endBand,
			); err != nil {
				return 0, false, 0, 0, err
			}
			d.silkRedundancyFades = append(d.silkRedundancyFades, silkRedundancyFade{
				celtToSilk:       redundancy.celtToSilk,
				audio:            redundancy.audio,
				startSample:      i * frameSamplesPerChannel * celtSampleRate / cfg.bandwidth().SampleRate(),
				frameSampleCount: frameSamplesPerChannel * celtSampleRate / cfg.bandwidth().SampleRate(),
				channelCount:     decodedChannelCount,
			})
		}
		if previousMode == configurationModeHybrid &&
			(!redundancy.present || !redundancy.celtToSilk || !previousRedundancy) {
			endBand, err := d.celtEndBandForSilkBandwidth(cfg.bandwidth())
			if err != nil {
				return 0, false, 0, 0, err
			}
			transitionAudio := make([]float32, hybridFadeSampleCount*decodedChannelCount)
			if err = d.celtDecoder.Decode(
				[]byte{0xff, 0xff},
				transitionAudio,
				tocHeader.isStereo(),
				decodedChannelCount,
				hybridFadeSampleCount,
				0,
				endBand,
			); err != nil {
				return 0, false, 0, 0, err
			}
			d.silkCeltAdditions = append(d.silkCeltAdditions, silkCeltAddition{
				audio:        transitionAudio,
				startSample:  i * frameSamplesPerChannel * celtSampleRate / cfg.bandwidth().SampleRate(),
				channelCount: decodedChannelCount,
			})
		}
		if len(encodedFrame) <= 1 {
			d.rangeFinal = 0
		} else {
			d.rangeFinal = d.rangeDecoder.FinalRange() ^ redundancy.rng
		}
		d.previousMode = configurationModeSilkOnly
		d.previousRedundancy = redundancy.present && !redundancy.celtToSilk
	}

	sampleCount = requiredSamples

	return cfg.bandwidth(), tocHeader.isStereo(), sampleCount, decodedChannelCount, nil
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

	bandwidth, isStereo, sampleCount, decodedChannelCount, err := d.decode(in, d.silkBuffer)
	if err != nil {
		return 0, 0, false, err
	}

	samplesPerChannel = (sampleCount / decodedChannelCount) * d.sampleRate / bandwidth.SampleRate()
	requiredSamples := samplesPerChannel * decodedChannelCount
	if cap(d.resampleBuffer) < requiredSamples {
		d.resampleBuffer = make([]float32, requiredSamples)
	}
	d.resampleBuffer = d.resampleBuffer[:requiredSamples]
	if d.sampleRate == bandwidth.SampleRate() {
		copy(d.resampleBuffer, d.silkBuffer[:sampleCount])
	} else {
		if err = d.resampleSilk(d.silkBuffer[:sampleCount], d.resampleBuffer, decodedChannelCount, bandwidth); err != nil {
			return 0, 0, false, err
		}
	}
	d.applySilkRedundancyFades(decodedChannelCount)

	if len(out) < samplesPerChannel*d.channels {
		return 0, 0, false, errOutBufferTooSmall
	}

	d.copyResampledSamples(out, decodedChannelCount)

	return samplesPerChannel, bandwidth, isStereo, nil
}

// applySilkRedundancyFades applies the leading/trailing 2.5 ms cross-laps from
// RFC 6716 Section 4.5.1.4 after SILK output has been resampled to 48 kHz.
//
//nolint:cyclop
func (d *Decoder) applySilkRedundancyFades(channelCount int) {
	fades := d.silkRedundancyFades
	additions := d.silkCeltAdditions
	d.silkRedundancyFades = d.silkRedundancyFades[:0]
	d.silkCeltAdditions = d.silkCeltAdditions[:0]
	if d.sampleRate != celtSampleRate {
		return
	}
	for _, addition := range additions {
		if addition.channelCount != channelCount {
			continue
		}
		start := addition.startSample * channelCount
		if start < 0 || start+len(addition.audio) > len(d.resampleBuffer) {
			continue
		}
		for i, sample := range addition.audio {
			d.resampleBuffer[start+i] += sample
		}
	}
	for _, fade := range fades {
		if fade.channelCount != channelCount {
			continue
		}
		frameStart := fade.startSample * channelCount
		if fade.celtToSilk {
			copyCount := hybridFadeSampleCount * channelCount
			if frameStart+2*copyCount > len(d.resampleBuffer) || copyCount > len(fade.audio) {
				continue
			}
			copy(d.resampleBuffer[frameStart:frameStart+copyCount], fade.audio[:copyCount])
			celt.SmoothFade(
				fade.audio[copyCount:],
				d.resampleBuffer[frameStart+copyCount:],
				d.resampleBuffer[frameStart+copyCount:],
				hybridFadeSampleCount,
				channelCount,
			)

			continue
		}

		fadeStart := (fade.startSample + fade.frameSampleCount - hybridFadeSampleCount) * channelCount
		redundantStart := hybridFadeSampleCount * channelCount
		if fadeStart < 0 || fadeStart+hybridFadeSampleCount*channelCount > len(d.resampleBuffer) ||
			redundantStart+hybridFadeSampleCount*channelCount > len(fade.audio) {
			continue
		}
		celt.SmoothFade(
			d.resampleBuffer[fadeStart:],
			fade.audio[redundantStart:],
			d.resampleBuffer[fadeStart:],
			hybridFadeSampleCount,
			channelCount,
		)
	}
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
