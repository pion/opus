// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"encoding/binary"
	"fmt"

	"github.com/pion/opus/internal/celt"
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
)

// celtOnlyFullband20msConfig is the TOC config number (bits 3..7) for
// CELT-only, fullband, 20 ms frames per RFC 6716 Table 2. The mono/stereo bit
// is separate (bit 2 of the TOC) and not part of this constant.
const celtOnlyFullband20msConfig = 31

// Encoder encodes PCM into Opus packets.
type Encoder struct {
	celtEncoder    celt.Encoder
	sampleRate     int
	channels       int
	bitrate        int
	complexity     int
	application    Application
	vbr            bool
	constrainedVBR bool
	lossRate       int
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

// NewEncoder creates a new Opus encoder with the supplied options.
//
// Defaults: 48 kHz, mono, 24 kbit/s, complexity 0. Pass options to override
// any of these. The current implementation supports 48 kHz, 1 or 2 channels,
// 20 ms CELT-only packets. Transient detection and SILK encoding will land
// in follow-up PRs.
func NewEncoder(opts ...EncoderOption) (*Encoder, error) {
	encoder := &Encoder{
		celtEncoder:    celt.NewEncoder(),
		sampleRate:     celtSampleRate,
		channels:       1,
		bitrate:        defaultBitrate,
		complexity:     0,
		application:    ApplicationAudio,
		vbr:            false,
		constrainedVBR: true,
		lossRate:       0,
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

	return encoder, nil
}

// SetBitrate updates the target bitrate in bits per second.
func (e *Encoder) SetBitrate(bps int) error {
	return WithBitrate(bps)(e)
}

// SetComplexity updates the encoder complexity on the standard Opus 0..10
// scale.
func (e *Encoder) SetComplexity(complexity int) error {
	if err := WithComplexity(complexity)(e); err != nil {
		return err
	}
	e.celtEncoder.SetComplexity(complexity)
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

// Application returns the current encoder application mode.
func (e *Encoder) Application() Application { return e.application }

// VBR returns whether variable bitrate encoding is enabled.
func (e *Encoder) VBR() bool { return e.vbr }

func (e *Encoder) Complexity() int { return e.complexity }

// ConstrainedVBR returns whether constrained VBR is enabled.
func (e *Encoder) ConstrainedVBR() bool { return e.constrainedVBR }

// LossRate returns the expected packet loss rate (0-100 percent).
func (e *Encoder) LossRate() int { return e.lossRate }

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
// The input must contain one 20 ms 48 kHz frame.
func (e *Encoder) EncodeFloat32(in []float32, out []byte) (int, error) {
	if e.sampleRate != celtSampleRate {
		return 0, errInvalidSampleRate
	}

	frameSamples := e.frameSampleCount()
	if len(in) != frameSamples*e.channels {
		return 0, fmt.Errorf("%w: got %d samples, want %d", errInvalidFrameSize, len(in), frameSamples*e.channels)
	}

	channels := splitChannels(in, e.channels, frameSamples)

	frameBytes := e.frameBytes()
	if frameBytes <= 0 || frameBytes > maxOpusFrameSize {
		return 0, fmt.Errorf("%w: %d", errInvalidFrameByteBudget, frameBytes)
	}
	if len(out) < frameBytes+1 {
		return 0, errOutBufferTooSmall
	}
	out[0] = byte(e.tocHeader())
	n, err := e.celtEncoder.EncodeFrame(channels, out[1:frameBytes+1], frameBytes, 0, e.celtEncoder.Mode().BandCount())
	if err != nil {
		return 0, err
	}

	return 1 + n, nil
}

func (e *Encoder) tocHeader() tableOfContentsHeader {
	header := byte(celtOnlyFullband20msConfig << 3)
	header |= byte(frameCodeOneFrame)
	if e.channels == 2 {
		header |= 1 << 2
	}

	return tableOfContentsHeader(header)
}

// splitChannels splits interleaved PCM into per-channel slices.
// For mono, it returns the input directly without allocation.
func splitChannels(in []float32, numChannels, frameSamples int) [][]float32 {
	ch := make([][]float32, numChannels)
	if numChannels == 1 {
		ch[0] = in

		return ch
	}

	for c := range numChannels {
		ch[c] = make([]float32, frameSamples)
		for i := range frameSamples {
			ch[c][i] = in[i*numChannels+c]
		}
	}

	return ch
}

func (e *Encoder) frameBytes() int {
	return int(int64(e.bitrate) * frame20msNS / 1000000000 / 8)
}

func (e *Encoder) frameSampleCount() int {
	return int(int64(celtSampleRate) * frame20msNS / 1000000000)
}
