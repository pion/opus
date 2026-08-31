// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"encoding/hex"
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

// TestEncodeSILKFrameFirstAfterResetSkipsPitch is pinned to libopus 1.6.1
// (22244de5a79bd1d6d623c32e72bf1954b56235be): an active first frame is
// unvoiced, with pitch/LTP state cleared, until first_frame_after_reset is
// consumed. Encoder and decoder must still finish at the same range state.
func TestEncodeSILKFrameFirstAfterResetSkipsPitch(t *testing.T) {
	bandwidth := BandwidthWideband
	fsKHz := silkInternalRate(bandwidth)
	frameLength := 20 * fsKHz
	input := make([]int16, frameLength)
	for i := range input {
		input[i] = int16(6000 * math.Sin(2*math.Pi*float64(i)/50))
	}

	enc := NewEncoder()
	packet := enc.Encode(input, bandwidth, 24000)
	require.NotEmpty(t, packet)
	assert.True(t, enc.vadFlags[0], "libopus vector is VAD-active")
	assert.False(t, enc.isPreviousFrameVoiced, "first frame after reset must not run pitch search")
	assert.Zero(t, enc.ltpCorr, "skipped pitch search must clear LTP correlation")
	assert.Zero(t, enc.previousLag, "skipped pitch search must clear the stored previous lag")
	assert.Zero(t, enc.nsq.lagPrev, "inactive pitch path must give NSQ zero lags")

	dec := NewDecoder()
	out := make([]float32, frameLength)
	require.NoError(t, dec.Decode(packet, out, false, nanoseconds20Ms, bandwidth))
	assert.Equal(t, enc.rangeEncoder.FinalRange(), dec.rangeDecoder.FinalRange())
}

// TestEncodeSILKFrameInactivePeriodicInputClearsPitch follows the libopus
// 1.6.1 low-activity lead: after a loud periodic frame and a long quiet
// periodic tail, VAD eventually becomes inactive. Pitch must not promote an
// inactive frame back to voiced; lag/LTP state is cleared and the range stream
// remains synchronized for every packet.
func TestEncodeSILKFrameInactivePeriodicInputClearsPitch(t *testing.T) {
	bandwidth := BandwidthWideband
	fsKHz := silkInternalRate(bandwidth)
	frameLength := 20 * fsKHz
	makePeriodic := func(amplitude, period float64) []int16 {
		input := make([]int16, frameLength)
		for i := range input {
			input[i] = int16(amplitude * math.Sin(2*math.Pi*float64(i)/period))
		}

		return input
	}

	enc := NewEncoder()
	dec := NewDecoder()
	out := make([]float32, frameLength)

	inputs := [][]int16{makePeriodic(6000, 50)}
	for range 20 {
		inputs = append(inputs, makePeriodic(16, 32))
	}
	for frame, input := range inputs {
		packet := enc.Encode(input, bandwidth, 24000)
		require.NotEmptyf(t, packet, "frame %d", frame)
		require.NoErrorf(t, dec.Decode(packet, out, false, nanoseconds20Ms, bandwidth), "frame %d", frame)
		require.Equalf(t, enc.rangeEncoder.FinalRange(), dec.rangeDecoder.FinalRange(), "frame %d", frame)
	}

	assert.False(t, enc.vadFlags[0], "quiet tail must converge to VAD-inactive")
	assert.False(t, enc.isPreviousFrameVoiced, "inactive frame must not be promoted by pitch")
	assert.Zero(t, enc.ltpCorr, "inactive frame must clear LTP correlation")
	assert.Zero(t, enc.previousLag, "inactive frame must clear the stored previous lag")
	assert.Zero(t, enc.nsq.lagPrev, "inactive frame must clear NSQ pitch lag")
}

// TestSILKVADThresholdBoundaryIsActive pins silk_encode_do_VAD_FLP's strict
// inactive comparison: speech activity is inactive only below the threshold,
// so SILK_FIX_CONST(0.05, 8) == 13 remains active.
func TestSILKVADThresholdBoundaryIsActive(t *testing.T) {
	assert.False(t, silkVADActive(silkVADThreshold-1))
	assert.True(t, silkVADActive(silkVADThreshold))
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

// TestLibopus161VADPolicyVectors pins the VAD header decisions produced by
// libopus 1.6.1 at exact source commit 22244de5a79bd1d6d623c32e72bf1954b56235be.
// Both packets are 20 ms, 16 kHz, mono, restricted SILK:
// the first is a loud periodic first frame, while the second follows one loud
// frame and twenty quiet periodic frames (amplitude 16, period 32).
func TestLibopus161VADPolicyVectors(t *testing.T) {
	for _, test := range []struct {
		name   string
		hex    string
		active bool
	}{
		{
			name: "first frame remains active while pitch is reset-gated",
			hex: "4881A7B7022A8729FA0637420C88CB4371F6B65F9E802E8FD52CF5FAD21127A8103DC8044DD3CF02C9" +
				"B24EBD74E782311962A0696744E9332E2BE8EE390D92F050558E46807036514940E156E1AA74A7E5DF83EC349870EC" +
				"9D87638B19D111841A02973DC0",
			active: true,
		},
		{
			name:   "quiet periodic tail is inactive",
			hex:    "4808AFB2CC0E9461B3D062D308E498AC2604A212E71EB821DF6A4CDEAE902D1CC6621841C87E23E8A378",
			active: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet, err := hex.DecodeString(test.hex)
			require.NoError(t, err)
			require.Equal(t, byte(0x48), packet[0], "20 ms mono wideband SILK TOC")

			dec := NewDecoder()
			dec.rangeDecoder.Init(packet[1:])
			vadFlags, lbrr := dec.decodeHeaderBitsInto(nil, 1)

			require.Equal(t, []bool{test.active}, vadFlags)
			assert.False(t, lbrr)
		})
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
