// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeSILKFrameDecodable encodes a SILK frame and decodes it with the
// real decoder, proving the bitstream is well-formed and self-consistent. This
// is the end-to-end gate short of opus_compare against libopus.
func TestEncodeSILKFrameDecodable(t *testing.T) {
	for _, bandwidth := range []Bandwidth{BandwidthNarrowband, BandwidthMediumband, BandwidthWideband} {
		fsKHz := silkInternalRate(bandwidth)
		frameLength := 20 * fsKHz

		input := make([]int16, frameLength)
		for i := range input {
			input[i] = int16(4000*math.Sin(2*math.Pi*float64(i)/50) + 1500*math.Sin(2*math.Pi*float64(i)/13))
		}

		enc := NewEncoder()
		enc.rangeEncoder.Init()
		enc.encodeSILKFrame(input, bandwidth)
		data := enc.rangeEncoder.Done()
		require.NotEmpty(t, data)

		dec := NewDecoder()
		out := make([]float32, frameLength)
		err := dec.Decode(data, out, false, nanoseconds20Ms, bandwidth)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)

		var energy float64
		for _, v := range out {
			energy += float64(v) * float64(v)
		}
		assert.Positivef(t, energy, "bandwidth %d: decoded output is silent", bandwidth)
	}
}

// TestEncodeSILKFrameSilence checks a silent input encodes and decodes cleanly.
func TestEncodeSILKFrameSilence(t *testing.T) {
	bandwidth := BandwidthWideband
	frameLength := 20 * silkInternalRate(bandwidth)

	enc := NewEncoder()
	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(make([]int16, frameLength), bandwidth)
	data := enc.rangeEncoder.Done()

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
}

// TestEncodeSILKFrameInterpolatedNLSF exercises the NLSF-interpolation branch
// (nlsfInterpQ2<4): needs SetUseInterpolatedNLSFs(true) and a second frame
// (firstFrameAfterReset only clears after the first encodeSILKFrame call).
// Decodes with the real decoder to confirm the interpolated first-half LPC
// coefficients still produce a valid bitstream.
func TestEncodeSILKFrameInterpolatedNLSF(t *testing.T) {
	bandwidth := BandwidthWideband
	fsKHz := silkInternalRate(bandwidth)
	frameLength := 20 * fsKHz

	gen := func(offset int) []int16 {
		input := make([]int16, frameLength)
		for i := range input {
			input[i] = int16(4000*math.Sin(2*math.Pi*float64(i+offset)/50) + 1500*math.Sin(2*math.Pi*float64(i+offset)/13))
		}

		return input
	}

	enc := NewEncoder()
	enc.SetUseInterpolatedNLSFs(true)
	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(gen(0), bandwidth)
	require.False(t, enc.firstFrameAfterReset, "first call should clear firstFrameAfterReset")

	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(gen(frameLength), bandwidth)
	data := enc.rangeEncoder.Done()
	require.NotEmpty(t, data)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
}

// TestEncodeSILKFrameUnvoicedHighOffset exercises the Unvoiced+High branch of
// emitFrameType (a quiet, high-frequency oscillation classified unvoiced with
// a sparse quantizer offset on the second frame).
func TestEncodeSILKFrameUnvoicedHighOffset(t *testing.T) {
	bandwidth := BandwidthWideband
	fsKHz := silkInternalRate(bandwidth)
	frameLength := 20 * fsKHz

	gen := func(offset int) []int16 {
		input := make([]int16, frameLength)
		for i := range input {
			input[i] = int16(100 * math.Sin(2*math.Pi*float64(i+offset)/5))
		}

		return input
	}

	enc := NewEncoder()
	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(gen(0), bandwidth)

	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(gen(frameLength), bandwidth)
	data := enc.rangeEncoder.Done()
	require.NotEmpty(t, data)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
}

// TestEncodeSILKFrameVoicedHighOffset exercises the Voiced+High branch of
// emitFrameType (processGains picks quantOffsetType=High when the quantized
// LTP prediction gain plus spectral tilt doesn't clear 1.0 — see
// process_gains_FLP.c). A fast upward pitch chirp (period sweeping 35->420
// samples within one frame) stays periodic enough to pass the voicing
// threshold but defeats the fixed-lag LTP filter, driving the quantized LTP
// gain low; found via a sweep over period/amplitude combinations.
func TestEncodeSILKFrameVoicedHighOffset(t *testing.T) {
	bandwidth := BandwidthWideband
	fsKHz := silkInternalRate(bandwidth)
	frameLength := 20 * fsKHz

	gen := func(offset int) []int16 {
		input := make([]int16, frameLength)
		for i := range input {
			frac := float64(i) / float64(frameLength)
			period := 35 + frac*(420-35)
			input[i] = int16(1500 * math.Sin(2*math.Pi*float64(i+offset)/period))
		}

		return input
	}

	enc := NewEncoder()
	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(gen(0), bandwidth)

	enc.rangeEncoder.Init()
	enc.encodeSILKFrame(gen(frameLength), bandwidth)
	data := enc.rangeEncoder.Done()
	require.NotEmpty(t, data)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
}

// TestEncode checks the public Encode wrapper: it must Init the range coder,
// apply an explicit target bitrate, and return a non-empty payload.
func TestEncode(t *testing.T) {
	bandwidth := BandwidthWideband
	frameLength := 20 * silkInternalRate(bandwidth)
	input := make([]int16, frameLength)
	for i := range input {
		input[i] = int16(3000 * math.Sin(2*math.Pi*float64(i)/50))
	}

	enc := NewEncoder()
	data := enc.Encode(input, bandwidth, 20000)

	require.NotEmpty(t, data)
	assert.Equal(t, 20000, enc.targetBitrate)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
}
