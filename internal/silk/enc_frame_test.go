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
		data := enc.Encode(input, bandwidth, 0)
		require.NotEmpty(t, data)
		encRange := enc.rangeEncoder.FinalRange()

		dec := NewDecoder()
		out := make([]float32, frameLength)
		err := dec.Decode(data, out, false, nanoseconds20Ms, bandwidth)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)
		require.Equalf(t, encRange, dec.rangeDecoder.FinalRange(), "bandwidth %d: range coder desync", bandwidth)

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
	data := enc.Encode(make([]int16, frameLength), bandwidth, 0)
	encRange := enc.rangeEncoder.FinalRange()

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
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
	first := enc.Encode(gen(0), bandwidth, 0)
	firstRange := enc.rangeEncoder.FinalRange()
	require.False(t, enc.firstFrameAfterReset, "first call should clear firstFrameAfterReset")

	data := enc.Encode(gen(frameLength), bandwidth, 0)
	dataRange := enc.rangeEncoder.FinalRange()
	require.NotEmpty(t, data)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(first, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, firstRange, dec.rangeDecoder.FinalRange(), "first frame range coder desync")
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, dataRange, dec.rangeDecoder.FinalRange(), "second frame range coder desync")
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
	first := enc.Encode(gen(0), bandwidth, 0)
	firstRange := enc.rangeEncoder.FinalRange()
	data := enc.Encode(gen(frameLength), bandwidth, 0)
	dataRange := enc.rangeEncoder.FinalRange()
	require.NotEmpty(t, data)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(first, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, firstRange, dec.rangeDecoder.FinalRange(), "first frame range coder desync")
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, dataRange, dec.rangeDecoder.FinalRange(), "second frame range coder desync")
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
	first := enc.Encode(gen(0), bandwidth, 0)
	firstRange := enc.rangeEncoder.FinalRange()
	data := enc.Encode(gen(frameLength), bandwidth, 0)
	dataRange := enc.rangeEncoder.FinalRange()
	require.NotEmpty(t, data)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(first, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, firstRange, dec.rangeDecoder.FinalRange(), "first frame range coder desync")
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, dataRange, dec.rangeDecoder.FinalRange(), "second frame range coder desync")
}

// TestEncodeSILKFrameLowVADVoicedHeader verifies that a periodic frame which
// pitch analysis promotes to voiced remains decodable even when the earlier
// VAD threshold classified it as inactive. The packet header and frame-type
// entropy table must use the same final activity decision.
func TestEncodeSILKFrameLowVADVoicedHeader(t *testing.T) {
	bandwidth := BandwidthWideband
	fsKHz := silkInternalRate(bandwidth)
	frameLength := 20 * fsKHz
	first := make([]int16, frameLength)
	for i := range first {
		first[i] = int16(6000 * math.Sin(2*math.Pi*float64(i)/50))
	}
	quiet := make([]int16, frameLength)
	for i := range quiet {
		quiet[i] = int16(1024 * math.Sin(2*math.Pi*float64(i)/32))
	}

	enc := NewEncoder()
	dec := NewDecoder()
	out := make([]float32, frameLength)
	firstPacket := enc.Encode(first, bandwidth, 0)
	firstRange := enc.rangeEncoder.FinalRange()
	require.NoError(t, dec.Decode(firstPacket, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, firstRange, dec.rangeDecoder.FinalRange(), "first frame range coder desync")
	for repeat := 1; repeat <= 10; repeat++ {
		packet := enc.Encode(quiet, bandwidth, 0)
		require.NoErrorf(t, dec.Decode(packet, out, false, nanoseconds20Ms, bandwidth),
			"low-VAD voiced packet failed to decode at repeat %d", repeat)
		require.Equalf(t, enc.rangeEncoder.FinalRange(), dec.rangeDecoder.FinalRange(),
			"low-VAD voiced packet desynchronized the range coder at repeat %d", repeat)
	}
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
	encRange := enc.rangeEncoder.FinalRange()

	require.NotEmpty(t, data)
	assert.Equal(t, 20000, enc.targetBitrate)

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(data, out, false, nanoseconds20Ms, bandwidth))
	require.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
}

func TestEncodeRejectsInvalidInputSizes(t *testing.T) {
	unitSamples := silkUnitSamples(BandwidthWideband)
	for _, test := range []struct {
		name        string
		sampleCount int
	}{
		{name: "empty", sampleCount: 0},
		{name: "partial unit", sampleCount: unitSamples - 1},
		{name: "non-multiple", sampleCount: unitSamples + 1},
		{name: "more than three units", sampleCount: 4 * unitSamples},
	} {
		t.Run(test.name, func(t *testing.T) {
			enc := NewEncoder()

			assert.Nil(t, enc.Encode(make([]int16, test.sampleCount), BandwidthWideband, 0))
		})
	}
}

func TestEncodeSILKPacketHeaderReservesInactiveVAD(t *testing.T) {
	for frameCount := 1; frameCount <= 3; frameCount++ {
		enc := NewEncoder()
		enc.rangeEncoder.Init()
		enc.encodeSILKPacketHeader(frameCount)
		encRange := enc.rangeEncoder.FinalRange()
		payload := enc.rangeEncoder.Done()

		dec := NewDecoder()
		dec.rangeDecoder.Init(payload)
		vadFlags, lbrr := dec.decodeHeaderBitsInto(nil, frameCount)

		assert.Equalf(t, make([]bool, frameCount), vadFlags, "frame count %d", frameCount)
		assert.Falsef(t, lbrr, "frame count %d", frameCount)
		assert.Equalf(t, encRange, dec.rangeDecoder.FinalRange(), "frame count %d", frameCount)
	}
}

// TestEncodeSILKFrameMixedVADFlags covers patching zero placeholders to a
// mixed final header through the real encoder and decoder.
func TestEncodeSILKFrameMixedVADFlags(t *testing.T) {
	bandwidth := BandwidthWideband
	unitSamples := silkUnitSamples(bandwidth)
	input := make([]int16, 2*unitSamples)
	for i := unitSamples; i < len(input); i++ {
		input[i] = int16(6000 * math.Sin(2*math.Pi*float64(i-unitSamples)/50))
	}

	enc := NewEncoder()
	payload := enc.Encode(input, bandwidth, 0)
	require.Equal(t, [3]bool{false, true, false}, enc.vadFlags)

	dec := NewDecoder()
	out := make([]float32, len(input))
	require.NoError(t, dec.Decode(payload, out, false, nanoseconds40Ms, bandwidth))
	assert.Equal(t, []bool{false, true}, dec.midVoiceActivity)
	assert.Equal(t, enc.rangeEncoder.FinalRange(), dec.rangeDecoder.FinalRange())
}
