// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/pion/opus/internal/celt"
	"github.com/pion/opus/internal/silk"
)

// Application selects the encoder's tuning profile, mirroring libopus's
// OPUS_APPLICATION_* control values (opus_defines.h) and their numeric IDs.
// RFC 6716 does not define per-application behavior as part of the
// bitstream; it only describes the underlying control parameters — bitrate
// mode, frame duration, DTX — that each profile is meant to bias (see
// RFC 6716 Section 2.1, "Control Parameters"). Selecting an Application here
// only records the chosen profile, retrievable via Application(); it does
// not change VBR, frame duration, or DTX on its own — pass WithVBR,
// WithConstrainedVBR, etc. explicitly.
type Application int

const (
	// ApplicationAudio tunes the encoder for music and general audio. This
	// is the default application.
	ApplicationAudio Application = 2049

	// ApplicationVoIP tunes the encoder for voice over a lossy,
	// latency-sensitive network. In libopus this profile defaults to VBR
	// (RFC 6716 Section 2.1.8) and DTX (RFC 6716 Section 2.1.9); this
	// encoder does not wire those defaults automatically.
	ApplicationVoIP Application = 2048

	// ApplicationRestrictedLowDelay tunes the encoder for the lowest
	// possible algorithmic delay by skipping mode-switching analysis
	// between the SILK and CELT layers. Frame duration and look-ahead
	// trade-offs are described in RFC 6716 Section 2.1.4; this encoder
	// does not vary either by application.
	ApplicationRestrictedLowDelay Application = 2051
)

const (
	defaultBitrate = 24000
	minBitrate     = 6000
	maxBitrate     = 510000
	frame20msNS    = 20000000
	// encodeFrameSamples is the one frame length the public encode path takes,
	// and encodeMaxChannels the widest layout, so together they bound its
	// per-frame working buffers.
	encodeFrameSamples = celtSampleRate * frame20msNS / 1000000000
	encodeMaxChannels  = 2
)

// encodeScratch holds what Encode and EncodeFloat32 would otherwise allocate on
// every call. The CELT layer already reuses its own analysis buffers; without
// this the wrapper around it still churned ~16 kB per frame, which at 50 frames
// a second is a lot of garbage for a server carrying many streams at once.
type encodeScratch struct {
	pcm      [encodeFrameSamples * encodeMaxChannels]float32
	deinter  [encodeMaxChannels][encodeFrameSamples]float32
	channels [encodeMaxChannels][]float32
}

// celtOnlyFullband20msConfig is the TOC config number (bits 3..7) for
// CELT-only, fullband, 20 ms frames per RFC 6716 Table 2. The mono/stereo bit
// is separate (bit 2 of the TOC) and not part of this constant.
const celtOnlyFullband20msConfig = 31

// SILK-only TOC config numbers per RFC 6716 Table 2, one per bandwidth and
// duration EncodeSILK supports.
const (
	silkOnlyNarrowband20msConfig = 1
	silkOnlyNarrowband40msConfig = 2
	silkOnlyNarrowband60msConfig = 3
	silkOnlyMediumband20msConfig = 5
	silkOnlyMediumband40msConfig = 6
	silkOnlyMediumband60msConfig = 7
	silkOnlyWideband20msConfig   = 9
	silkOnlyWideband40msConfig   = 10
	silkOnlyWideband60msConfig   = 11
)

// silkComplexityInterpolationThreshold is the encoder complexity at or above
// which SILK's NLSF interpolation search is enabled, mirroring libopus's
// silk_setup_complexity (control_codec.c): useInterpolatedNLSFs is 0 below
// complexity 4, 1 from 4 up.
const silkComplexityInterpolationThreshold = 4

// silkDCBlockCutoffHz is the high-pass cutoff EncodeSILK applies to its input
// before handing it to internal/silk. libopus applies this filter (dc_reject,
// src/opus_encoder.c:479-507) to the shared PCM ahead of both the SILK and
// CELT encoders, not inside silk/ itself — so this mirrors celt's
// dcBlockCutoffHz rather than living in internal/silk.
const silkDCBlockCutoffHz = 3.0

// Encoder encodes PCM into Opus packets.
type Encoder struct {
	celtEncoder    celt.Encoder
	silkEncoder    silk.Encoder
	sampleRate     int
	channels       int
	bitrate        int
	complexity     int
	application    Application
	vbr            bool
	constrainedVBR bool
	lossRate       int
	bandwidth      Bandwidth
	maxBandwidth   Bandwidth
	silkDCBlockMem float32
	stereoWidth    int
	scratch        encodeScratch
}

// EncoderOption configures an Encoder during construction.
//
// Options are applied in the order they are passed to NewEncoder. Each option
// returns an error if the requested value is unsupported by the current
// encoder slice, so callers can detect unsupported configurations at
// construction time rather than at first encode.
type EncoderOption func(*Encoder) error

// WithSampleRate sets the input sample rate in Hz. The current encoder only
// supports 48 kHz (the CELT internal rate).
func WithSampleRate(rate int) EncoderOption {
	return func(e *Encoder) error {
		if rate != celtSampleRate {
			return errInvalidSampleRate
		}
		e.sampleRate = rate

		return nil
	}
}

// WithChannels sets the channel count (1 for mono, 2 for stereo).
func WithChannels(channels int) EncoderOption {
	return func(e *Encoder) error {
		if channels < 1 || channels > 2 {
			return errInvalidChannelCount
		}
		e.channels = channels

		return nil
	}
}

// WithBitrate sets the target bitrate in bits per second. Valid range is
// 6000 to 510000.
func WithBitrate(bps int) EncoderOption {
	return func(e *Encoder) error {
		if bps < minBitrate || bps > maxBitrate {
			return fmt.Errorf("%w: %d", errBitrateOutOfRange, bps)
		}
		e.bitrate = bps

		return nil
	}
}

// WithComplexity sets the encoder complexity on the standard Opus 0..10
// scale. Higher values enable more analysis (pitch detection, spreading,
// dynalloc) for better quality at the cost of CPU.
func WithComplexity(complexity int) EncoderOption {
	return func(e *Encoder) error {
		if complexity < 0 || complexity > 10 {
			return fmt.Errorf("%w: %d", errInvalidComplexity, complexity)
		}
		e.complexity = complexity

		return nil
	}
}

// WithApplication sets the encoder application mode.
func WithApplication(app Application) EncoderOption {
	return func(e *Encoder) error {
		switch app {
		case ApplicationAudio, ApplicationVoIP, ApplicationRestrictedLowDelay:
		default:
			return fmt.Errorf("%w: %d", errInvalidApplication, app)
		}
		e.application = app

		return nil
	}
}

// WithVBR enables or disables variable bitrate encoding. VBR is the more
// efficient mode and is the Opus default; CBR is reserved for transports
// that require a fixed frame size or for highly sensitive streams (RFC 6716
// Section 2.1.8).
func WithVBR(vbr bool) EncoderOption {
	return func(e *Encoder) error {
		e.vbr = vbr

		return nil
	}
}

// WithConstrainedVBR enables or disables constrained VBR. When enabled, the
// encoder simulates a "bit reservoir" to bound short-term bitrate variation
// instead of producing plain VBR — recommended for low-latency links over a
// constrained connection (RFC 6716 Section 2.1.8).
func WithConstrainedVBR(cvbr bool) EncoderOption {
	return func(e *Encoder) error {
		e.constrainedVBR = cvbr

		return nil
	}
}

// WithBandwidth sets the encoder bandwidth explicitly (Narrowband through
// Fullband; Mediumband is SILK-only and not supported here). Use
// WithMaxBandwidth instead to cap auto-selection rather than fixing it.
func WithBandwidth(bw Bandwidth) EncoderOption {
	return func(e *Encoder) error {
		if bw == BandwidthAuto {
			return fmt.Errorf("%w: use WithMaxBandwidth for auto selection", errInvalidBandwidth)
		}
		if bw < BandwidthNarrowband || bw > BandwidthFullband {
			return fmt.Errorf("%w: %d", errInvalidBandwidth, bw)
		}
		if bw == BandwidthMediumband {
			return fmt.Errorf("%w: mediumband not supported in CELT-only mode", errInvalidBandwidth)
		}
		e.bandwidth = bw

		return nil
	}
}

// WithMaxBandwidth sets the maximum bandwidth the auto-select algorithm may
// choose. Has no effect when an explicit bandwidth is set via WithBandwidth.
func WithMaxBandwidth(bw Bandwidth) EncoderOption {
	return func(e *Encoder) error {
		if bw == BandwidthAuto {
			return fmt.Errorf("%w: max bandwidth must be explicit", errInvalidBandwidth)
		}
		if bw < BandwidthNarrowband || bw > BandwidthFullband {
			return fmt.Errorf("%w: %d", errInvalidBandwidth, bw)
		}
		if bw == BandwidthMediumband {
			return fmt.Errorf("%w: mediumband not supported in CELT-only mode", errInvalidBandwidth)
		}
		e.maxBandwidth = bw

		return nil
	}
}

// NewEncoder creates a new Opus encoder with the supplied options.
//
// Defaults: 48 kHz, mono, 24 kbit/s, complexity 5. Pass options to override
// any of these. The current implementation supports 48 kHz, 1 or 2 channels,
// 20 ms CELT-only packets, plus SILK-only encoding via EncodeSILK. Transient
// detection is a follow-up.
func NewEncoder(opts ...EncoderOption) (*Encoder, error) {
	encoder := &Encoder{
		celtEncoder:    celt.NewEncoder(),
		silkEncoder:    silk.NewEncoder(),
		sampleRate:     celtSampleRate,
		channels:       1,
		bitrate:        defaultBitrate,
		complexity:     5,
		application:    ApplicationAudio,
		vbr:            false,
		constrainedVBR: true,
		lossRate:       0,
		bandwidth:      BandwidthAuto,
		maxBandwidth:   BandwidthFullband,
		stereoWidth:    stereoWidthFull,
	}

	for _, opt := range opts {
		if err := opt(encoder); err != nil {
			return nil, err
		}
	}

	encoder.celtEncoder.SetVBR(encoder.vbr)
	encoder.celtEncoder.SetConstrainedVBR(encoder.constrainedVBR)
	encoder.celtEncoder.SetLossRate(encoder.lossRate)
	encoder.celtEncoder.SetComplexity(encoder.complexity)
	encoder.celtEncoder.SetBitrate(encoder.bitrate)
	encoder.silkEncoder.SetUseInterpolatedNLSFs(encoder.complexity >= silkComplexityInterpolationThreshold)

	return encoder, nil
}

// SetBitrate updates the target bitrate in bits per second.
func (e *Encoder) SetBitrate(bps int) error {
	if err := WithBitrate(bps)(e); err != nil {
		return err
	}
	e.celtEncoder.SetBitrate(e.bitrate)

	return nil
}

// SetComplexity updates the encoder complexity on the standard Opus 0..10
// scale.
func (e *Encoder) SetComplexity(complexity int) error {
	if err := WithComplexity(complexity)(e); err != nil {
		return err
	}
	e.celtEncoder.SetComplexity(complexity)
	e.silkEncoder.SetUseInterpolatedNLSFs(complexity >= silkComplexityInterpolationThreshold)

	return nil
}

// SetApplication updates the encoder application mode.
func (e *Encoder) SetApplication(app Application) error {
	return WithApplication(app)(e)
}

// SetVBR enables or disables variable bitrate encoding (RFC 6716
// Section 2.1.8).
func (e *Encoder) SetVBR(vbr bool) {
	e.vbr = vbr
	e.celtEncoder.SetVBR(vbr)
}

// SetConstrainedVBR enables or disables constrained VBR (RFC 6716
// Section 2.1.8).
func (e *Encoder) SetConstrainedVBR(cvbr bool) {
	e.constrainedVBR = cvbr
	e.celtEncoder.SetConstrainedVBR(cvbr)
}

// SetLossRate sets the expected packet loss rate (0-100 percent), the
// control parameter behind the packet loss resilience trade-off described
// in RFC 6716 Section 2.1.6.
func (e *Encoder) SetLossRate(rate int) error {
	if rate < 0 || rate > 100 {
		return fmt.Errorf("%w: %d", errInvalidLossRate, rate)
	}
	e.lossRate = rate
	e.celtEncoder.SetLossRate(rate)

	return nil
}

// SetBandwidth sets the encoder bandwidth, overriding auto-selection.
func (e *Encoder) SetBandwidth(bw Bandwidth) error {
	return WithBandwidth(bw)(e)
}

// SetMaxBandwidth sets the maximum bandwidth the auto-select algorithm may
// choose. Only affects encoding when bandwidth is set to BandwidthAuto (the
// default).
func (e *Encoder) SetMaxBandwidth(bw Bandwidth) error {
	return WithMaxBandwidth(bw)(e)
}

// Application returns the current encoder application mode.
func (e *Encoder) Application() Application { return e.application }

// VBR returns whether variable bitrate encoding is enabled.
func (e *Encoder) VBR() bool { return e.vbr }

func (e *Encoder) Complexity() int { return e.complexity }

// ConstrainedVBR returns whether constrained VBR is enabled.
func (e *Encoder) ConstrainedVBR() bool { return e.constrainedVBR }

// LossRate returns the expected packet loss rate (0-100 percent).
func (e *Encoder) LossRate() int { return e.lossRate }

// Bandwidth returns the configured bandwidth (BandwidthAuto by default).
func (e *Encoder) Bandwidth() Bandwidth { return e.bandwidth }

// MaxBandwidth returns the maximum bandwidth the auto-select algorithm may
// choose.
func (e *Encoder) MaxBandwidth() Bandwidth { return e.maxBandwidth }

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

	pcm := e.scratch.pcm[:len(in)/2]
	for i := range pcm {
		sample := int16(binary.LittleEndian.Uint16(in[i*2:])) //nolint:gosec // G115: little-endian s16 round-trip.
		pcm[i] = float32(sample) / 32768
	}

	return e.EncodeFloat32(pcm, out)
}

// EncodeFloat32 encodes float PCM into a single Opus packet.
//
// The input must contain one 20 ms 48 kHz frame.
func (e *Encoder) EncodeFloat32(in []float32, out []byte) (int, error) {
	if e.sampleRate != celtSampleRate {
		return 0, errInvalidSampleRate
	}

	frameSamples := e.frameSampleCount()
	if len(in) != frameSamples*e.channels {
		return 0, fmt.Errorf("%w: got %d samples, want %d", errInvalidFrameSize, len(in), frameSamples*e.channels)
	}

	channels := e.splitChannels(in, e.channels, frameSamples)
	e.narrowStereo(channels)

	frameBytes := e.frameBytes()
	if frameBytes <= 0 || frameBytes > maxOpusFrameSize {
		return 0, fmt.Errorf("%w: %d", errInvalidFrameByteBudget, frameBytes)
	}
	if len(out) < frameBytes+tocHeaderBytes {
		return 0, errOutBufferTooSmall
	}
	out[0] = byte(e.tocHeader())
	bw := e.autoSelectBandwidth()
	startBand, endBand, err := e.celtEncoder.Mode().BandRangeForSampleRate(bw.SampleRate())
	if err != nil {
		return 0, err
	}
	// VBR gets the whole buffer the caller supplied: a demanding frame may run
	// past the nominal rate and the bit reservoir wins it back later. CBR is
	// pinned to its share.
	payload := out[tocHeaderBytes:]
	if !e.vbr && len(payload) > frameBytes {
		payload = payload[:frameBytes]
	}
	n, err := e.celtEncoder.EncodeFrame(channels, payload, frameBytes, startBand, endBand)
	if err != nil {
		return 0, err
	}

	return 1 + n, nil
}

// EncodeSILK encodes one 20, 40, or 60 ms mono SILK frame into a SILK-only
// Opus packet. pcm must hold exactly one frame of mono s16 samples at the
// bandwidth's internal rate: 160/320/480 (Narrowband/8 kHz), 240/480/720
// (Mediumband/12 kHz), or 320/640/960 (Wideband/16 kHz) samples for 20/40/60
// ms. Durations longer than 20 ms are coded as multiple 20 ms SILK coding
// units in a single SILK header, per RFC 6716 Section 4.2.1. This is a
// separate entry point from Encode/EncodeFloat32 — bitrate-based
// auto-selection always picks CELT bandwidths (Wideband and up); SILK is for
// callers who specifically want a SILK-only voice packet (VoIP/narrowband use
// cases), not an automatic CELT/SILK/hybrid switch. Superwideband and
// Fullband aren't SILK bandwidths and are rejected. Applies a fixed DC-removal
// high-pass before encoding (libopus's dc_reject applied to the shared PCM
// path); the pitch-adaptive VoIP cutoff (hp_cutoff) is not implemented.
// Covers voiced/LTP prediction, noise shaping and NLSF interpolation (see
// internal/silk); the delayed-decision NSQ, stereo, hybrid mode, and the
// bitrate-control loop are not yet implemented.
func (e *Encoder) EncodeSILK(pcm []int16, bandwidth Bandwidth, out []byte) (int, error) {
	unitSamples := bandwidth.SampleRate() / 50 // 20 ms
	if unitSamples == 0 || len(pcm)%unitSamples != 0 {
		return 0, fmt.Errorf("%w: got %d samples, want a multiple of %d", errInvalidFrameSize, len(pcm), unitSamples)
	}
	frameCount := len(pcm) / unitSamples
	// A SILK-only packet carries at least one 20 ms coding unit and at most
	// three (RFC 6716 Section 2.1.4); an empty input is not a well-formed
	// frame and fails closed like any other invalid size.
	if frameCount < 1 || frameCount > 3 {
		return 0, fmt.Errorf("%w: got %d samples, want 1..3 frames of %d", errInvalidFrameSize, len(pcm), unitSamples)
	}

	// The SILK-only TOC config numbers in RFC 6716 Table 2 are consecutive
	// per duration within each bandwidth (e.g. Wideband: 9, 10, 11 for
	// 20/40/60 ms), so the duration offset is frameCount-1, not (frameCount-1)*2.
	var config int
	switch bandwidth {
	case BandwidthNarrowband:
		config = silkOnlyNarrowband20msConfig + (frameCount - 1) //nolint:gosec // G115: frameCount is 1..3.
	case BandwidthMediumband:
		config = silkOnlyMediumband20msConfig + (frameCount - 1) //nolint:gosec // G115: frameCount is 1..3.
	case BandwidthWideband:
		config = silkOnlyWideband20msConfig + (frameCount - 1) //nolint:gosec // G115: frameCount is 1..3.
	default:
		return 0, fmt.Errorf("%w: %d", errInvalidBandwidth, bandwidth)
	}

	filtered := applySILKDCBlock(pcm, bandwidth.SampleRate(), &e.silkDCBlockMem)
	// The SILK encoder keeps its prediction state (NLSF interpolation, pitch
	// lag, gain, LCG seed) across packets so consecutive 60 ms frames stay in
	// sync with the decoder's stateful stream. Only the range coder is
	// re-initialized per packet, so each packet is a self-contained bitstream
	// that the decoder can start reading from scratch.
	payload := e.silkEncoder.Encode(filtered, silk.Bandwidth(bandwidth), e.bitrate)
	if len(out) < len(payload)+1 {
		return 0, errOutBufferTooSmall
	}

	out[0] = byte(config<<3) | byte(frameCodeOneFrame) // mono, one frame
	n := copy(out[1:], payload)

	return n + 1, nil
}

// applySILKDCBlock removes DC bias from pcm with a first-order IIR high-pass
// at silkDCBlockCutoffHz, returning a new slice (pcm is left untouched). mem
// must persist across calls for the same stream.
func applySILKDCBlock(pcm []int16, sampleRate int, mem *float32) []int16 {
	coef := float32(6.3) * silkDCBlockCutoffHz / float32(sampleRate)
	coef2 := 1 - coef
	out := make([]int16, len(pcm))
	for i, sample := range pcm {
		x := float32(sample)
		y := x - *mem
		*mem = coef*x + coef2**mem
		switch {
		case y > 32767:
			out[i] = 32767
		case y < -32768:
			out[i] = -32768
		default:
			out[i] = int16(math.Round(float64(y)))
		}
	}

	return out
}

func (e *Encoder) tocHeader() tableOfContentsHeader {
	bw := e.autoSelectBandwidth()
	var config int
	switch bw {
	case BandwidthNarrowband:
		config = 19 // CELT-only, NB, 20 ms
	case BandwidthWideband:
		config = 23 // CELT-only, WB, 20 ms
	case BandwidthSuperwideband:
		config = 27 // CELT-only, SWB, 20 ms
	default: // BandwidthFullband
		config = 31 // CELT-only, FB, 20 ms
	}
	header := byte(config<<3) | byte(frameCodeOneFrame)
	if e.channels == 2 {
		header |= 1 << 2
	}

	return tableOfContentsHeader(header)
}

// equivRate estimates the effective bitrate actually available for coding,
// mirroring libopus's compute_equiv_rate (opus_encoder.c). The CELT-only
// branch there also docks ~10% for complexity<5 lacking the pitch filter;
// omitted here since this encoder's pitch pre-filter always runs regardless
// of complexity. The frame-rate-overhead term is also omitted: it only
// applies above 50 frames/sec, and this encoder is fixed at 20 ms (50/sec).
func (e *Encoder) equivRate() int {
	equiv := e.bitrate
	if !e.vbr {
		equiv -= equiv / 12 // CBR costs about 8%.
	}

	return equiv * (90 + e.complexity) / 100 // complexity spans about 10%.
}

// autoSelectBandwidth selects the best bandwidth for the current bitrate,
// clamped to maxBandwidth. Returns the effective bandwidth to use for encoding.
func (e *Encoder) autoSelectBandwidth() Bandwidth {
	if e.bandwidth != BandwidthAuto {
		return e.bandwidth
	}
	// Thresholds based on libopus voice defaults.
	// NB↔WB: 9000 bps, WB↔SWB: 13500 bps, SWB↔FB: 14000 bps.
	target := e.equivRate()
	var bw Bandwidth
	switch {
	case target < 9000:
		bw = BandwidthNarrowband
	case target < 13500:
		bw = BandwidthWideband
	case target < 14000:
		bw = BandwidthSuperwideband
	default:
		bw = BandwidthFullband
	}
	if bw > e.maxBandwidth {
		bw = e.maxBandwidth
	}

	return bw
}

// splitChannels splits interleaved PCM into per-channel slices.
// For mono, it returns the input directly without allocation.
func (e *Encoder) splitChannels(in []float32, numChannels, frameSamples int) [][]float32 {
	ch := e.scratch.channels[:numChannels]
	if numChannels == 1 {
		ch[0] = in

		return ch
	}

	for c := range numChannels {
		buf := e.scratch.deinter[c][:frameSamples]
		for i := range frameSamples {
			buf[i] = in[i*numChannels+c]
		}
		ch[c] = buf
	}

	return ch
}

// tocHeaderBytes is the single table-of-contents byte every packet starts with.
const tocHeaderBytes = 1

// frameBytes returns the CELT payload budget. The packet carries a TOC byte in
// front of it, so the payload gets one byte less than the frame's share of the
// bitrate — otherwise every packet overshoots the target by a byte, which is
// 400 bps at 20 ms.
func (e *Encoder) frameBytes() int {
	return int(int64(e.bitrate)*frame20msNS/1000000000/8) - tocHeaderBytes
}

func (e *Encoder) frameSampleCount() int {
	return int(int64(celtSampleRate) * frame20msNS / 1000000000)
}

const (
	// stereoWidthFull is Q14 unity: the image is left alone.
	stereoWidthFull = 1 << 14
	// Below stereoWidthMinRate the image is collapsed to mono; above
	// stereoWidthMaxRate it is untouched. In between it narrows gradually.
	stereoWidthMinRate = 16000
	stereoWidthMaxRate = 32000
)

// equivalentRate expresses the configured bitrate as the rate an ideal encoder
// would need for the same quality, which is what libopus compares against its
// stereo-width and mode thresholds (compute_equiv_rate, src/opus_encoder.c).
// The frame-rate term is a no-op here because every frame is 20 ms.
func (e *Encoder) equivalentRate() int {
	equiv := e.bitrate
	// CBR costs about 8%.
	if !e.vbr {
		equiv -= equiv / 12
	}
	equiv = equiv * (90 + e.complexity) / 100
	// Below complexity 5 CELT drops the pitch filter, worth about 10%.
	if e.complexity < silkComplexityInterpolationThreshold+1 {
		equiv = equiv * 9 / 10
	}

	return equiv
}

// stereoWidthQ14 returns how much of the stereo image to keep, in Q14.
// Mirrors the schedule in libopus opus_encode_native.
func stereoWidthQ14(equivRate int) int {
	switch {
	case equivRate > stereoWidthMaxRate:
		return stereoWidthFull
	case equivRate < stereoWidthMinRate:
		return 0
	default:
		return stereoWidthFull - 2048*(stereoWidthMaxRate-equivRate)/(equivRate-14000)
	}
}

// applyStereoFade narrows the stereo image toward mono by scaling the side
// signal, crossfading from the previous frame's width across the MDCT overlap
// so the change does not land as a step. Mirrors libopus stereo_fade
// (src/opus_encoder.c). At low bitrates the side channel is not worth its bits,
// and narrowing it beats letting the allocator starve both channels.
func applyStereoFade(left, right []float32, prevWidth, width float32, window []float32) {
	g1 := 1 - prevWidth
	g2 := 1 - width
	overlap := min(len(window), len(left))
	for i := range overlap {
		w := window[i] * window[i]
		g := w*g2 + (1-w)*g1
		diff := 0.5 * (left[i] - right[i]) * g
		left[i] -= diff
		right[i] += diff
	}
	for i := overlap; i < len(left); i++ {
		diff := 0.5 * (left[i] - right[i]) * g2
		left[i] -= diff
		right[i] += diff
	}
}

// narrowStereo applies the low-bitrate stereo width reduction to the split
// channels and advances the width state.
func (e *Encoder) narrowStereo(channels [][]float32) {
	if len(channels) != 2 {
		return
	}
	width := stereoWidthQ14(e.equivalentRate())
	if e.stereoWidth < stereoWidthFull || width < stereoWidthFull {
		applyStereoFade(
			channels[0], channels[1],
			float32(e.stereoWidth)/stereoWidthFull, float32(width)/stereoWidthFull,
			celt.OverlapWindow(),
		)
	}
	e.stereoWidth = width
}
