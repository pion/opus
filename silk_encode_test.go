// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeSILKRoundTrip encodes a SILK frame with the public encoder and
// decodes the resulting Opus packet with the public decoder, for every
// SILK-supported bandwidth, checking the packet is well-formed and decodes to
// non-silent audio.
func TestEncodeSILKRoundTrip(t *testing.T) {
	for _, bandwidth := range []Bandwidth{BandwidthNarrowband, BandwidthMediumband, BandwidthWideband} {
		enc, err := NewEncoder()
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)

		sampleCount := bandwidth.SampleRate() / 50 // 20 ms
		pcm := make([]int16, sampleCount)
		for i := range pcm {
			pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
		}

		packet := make([]byte, maxOpusFrameSize)
		n, err := enc.EncodeSILK(pcm, bandwidth, packet)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)
		require.Greaterf(t, n, 1, "bandwidth %d", bandwidth)
		packet = packet[:n]

		dec, err := NewDecoderWithOutput(bandwidth.SampleRate(), 1)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)

		out := make([]float32, sampleCount)
		got, err := dec.DecodeToFloat32(packet, out)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)
		require.Positivef(t, got, "bandwidth %d", bandwidth)

		var energy float64
		for _, v := range out[:got] {
			energy += float64(v) * float64(v)
		}
		assert.Positivef(t, energy, "bandwidth %d: decoded output is silent", bandwidth)
	}
}

// TestEncodeSILKMultiUnitRoundTrip checks the TOC, frame code, decoded sample
// count, and energy for 20, 40, and 60 ms at every SILK bandwidth. The 60 ms
// case also covers a representative 24 kb/s, complexity-5 VoIP configuration.
func TestEncodeSILKMultiUnitRoundTrip(t *testing.T) {
	durations := []struct {
		units    int // 20 ms coding units
		wbConfig int // expected TOC config (RFC 6716 Table 2)
		nbConfig int
		mbConfig int
	}{
		{1, silkOnlyWideband20msConfig, silkOnlyNarrowband20msConfig, silkOnlyMediumband20msConfig},
		{2, silkOnlyWideband40msConfig, silkOnlyNarrowband40msConfig, silkOnlyMediumband40msConfig},
		{3, silkOnlyWideband60msConfig, silkOnlyNarrowband60msConfig, silkOnlyMediumband60msConfig},
	}
	for _, duration := range durations {
		name := fmt.Sprintf("%dms", duration.units*20)
		t.Run(name, func(t *testing.T) {
			t.Run("default", func(t *testing.T) {
				runMultiUnitRoundTrip(t, name, duration.units, duration.wbConfig, duration.nbConfig, duration.mbConfig)
			})
			if duration.units == 3 {
				t.Run("wideband60", func(t *testing.T) {
					runMultiUnitRoundTripWideband60(t, name)
				})
			}
		})
	}
}

// runMultiUnitRoundTrip encodes one frame of the given duration at each
// SILK-supported bandwidth with a default encoder, decodes it back, and
// checks the TOC config, frame code, sample count, and non-silent energy.
func runMultiUnitRoundTrip(t *testing.T, name string, units int, wbConfig, nbConfig, mbConfig int) {
	t.Helper()
	configs := []int{wbConfig, nbConfig, mbConfig}
	for bandwidthIndex, bandwidth := range []Bandwidth{BandwidthWideband, BandwidthNarrowband, BandwidthMediumband} {
		wantConfig := configs[bandwidthIndex]
		enc, err := NewEncoder()
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)

		sampleCount := bandwidth.SampleRate() / 50 * units
		pcm := make([]int16, sampleCount)
		for i := range pcm {
			pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
		}

		packet := make([]byte, maxOpusFrameSize)
		n, err := enc.EncodeSILK(pcm, bandwidth, packet)
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)
		require.Greaterf(t, n, 1, "%s bw %d", name, bandwidth)
		packet = packet[:n]

		// TOC: mono (s=0), one frame in the packet (c=0), and the
		// SILK-only config for this bandwidth and duration.
		toc := int(packet[0])
		assert.Equalf(t, wantConfig<<3, toc,
			"%s bw %d: TOC %#x, want config %d (0x%02x)", name, bandwidth, toc, wantConfig, wantConfig<<3)
		assert.Equalf(t, 0, toc&1, "%s bw %d: frame code must be 0 (single frame)", name, bandwidth)

		dec, err := NewDecoderWithOutput(bandwidth.SampleRate(), 1)
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)

		out := make([]float32, sampleCount)
		got, err := dec.DecodeToFloat32(packet, out)
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)
		assert.Equalf(t, sampleCount, got, "%s bw %d: decoded %d samples, want %d", name, bandwidth, got, sampleCount)

		var energy float64
		for _, v := range out[:got] {
			energy += float64(v) * float64(v)
		}
		assert.Positivef(t, energy, "%s bw %d: decoded output is silent", name, bandwidth)
	}
}

// runMultiUnitRoundTripWideband60 runs the 60 ms wideband round trip with a
// representative VoIP configuration: 24 kb/s at complexity 5.
func runMultiUnitRoundTripWideband60(t *testing.T, name string) {
	t.Helper()
	enc, err := NewEncoder(
		WithApplication(ApplicationVoIP),
		WithBitrate(24000),
		WithComplexity(5),
	)
	require.NoErrorf(t, err, "%s", name)

	unit := BandwidthWideband.SampleRate() / 50 // 320 samples per 20 ms unit
	pcm := make([]int16, 3*unit)                // 960 samples, one 60 ms frame
	for i := range pcm {
		pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
	}

	packet := make([]byte, maxOpusFrameSize)
	n, err := enc.EncodeSILK(pcm, BandwidthWideband, packet)
	require.NoErrorf(t, err, "%s", name)
	require.Greaterf(t, n, 1, "%s", name)
	packet = packet[:n]

	// TOC: config 11 (SILK-only, WB, 60 ms, RFC 6716 Table 2), s=0 (mono),
	// frame code 0 (one frame in the packet).
	assert.Equalf(t, silkOnlyWideband60msConfig<<3, int(packet[0]),
		"%s: TOC %#x, want 0x%02x (SILK-only WB 60 ms, mono, frame code 0)",
		name, int(packet[0]), silkOnlyWideband60msConfig<<3)

	dec, err := NewDecoderWithOutput(BandwidthWideband.SampleRate(), 1)
	require.NoErrorf(t, err, "%s", name)

	out := make([]float32, 3*unit)
	got, err := dec.DecodeToFloat32(packet, out)
	require.NoErrorf(t, err, "%s", name)
	assert.Equalf(t, 3*unit, got, "%s: decoded %d samples, want %d (960)", name, got, 3*unit)

	var energy float64
	for _, v := range out[:got] {
		energy += float64(v) * float64(v)
	}
	assert.Positivef(t, energy, "%s: decoded output is silent", name)
}

// TestEncodeSILKStateContinuity feeds the same encoder consecutive 20, 40,
// and 60 ms frames and decodes them with one stateful decoder. Prediction
// state (NLSF interpolation, pitch lag, LCG seed) must stay consistent
// across units of a 60 ms packet and across packets; a mismatch between the
// encoder's per-unit emission and the decoder's per-unit consumption would
// desynchronize the range stream and fail closed here.
func TestEncodeSILKStateContinuity(t *testing.T) {
	enc, err := NewEncoder(WithComplexity(8))
	require.NoError(t, err)

	dec, err := NewDecoderWithOutput(BandwidthWideband.SampleRate(), 1)
	require.NoError(t, err)

	unitSamples := BandwidthWideband.SampleRate() / 50 // 320
	for _, units := range []int{1, 1, 2, 3, 3, 1, 2, 3} {
		pcm := make([]int16, unitSamples*units)
		for i := range pcm {
			pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
		}
		packet := make([]byte, maxOpusFrameSize)
		n, err := enc.EncodeSILK(pcm, BandwidthWideband, packet)
		require.NoError(t, err)

		out := make([]float32, len(pcm))
		got, err := dec.DecodeToFloat32(packet[:n], out)
		require.NoError(t, err)
		assert.Equal(t, len(pcm), got)

		var energy float64
		for _, v := range out[:got] {
			energy += float64(v) * float64(v)
		}
		assert.Positive(t, energy)
	}
}

// TestEncodeSILKInvalidSizes rejects lengths that are not a whole number of
// 20 ms coding units at the bandwidth's internal rate, plus the case of more
// than three units (120 ms), which the SILK packet duration cap forbids.
func TestEncodeSILKInvalidSizes(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	unit := BandwidthWideband.SampleRate() / 50 // 320
	for _, samples := range []int{0, unit - 1, unit + 1, 2*unit - 1, 3*unit + 1, 4 * unit} {
		_, err := enc.EncodeSILK(make([]int16, samples), BandwidthWideband, make([]byte, maxOpusFrameSize))
		assert.Errorf(t, err, "%d samples", samples)
	}

	// Exact multiples of a coding unit up to three units are accepted.
	for _, samples := range []int{unit, 2 * unit, 3 * unit} {
		_, err := enc.EncodeSILK(make([]int16, samples), BandwidthWideband, make([]byte, maxOpusFrameSize))
		assert.NoErrorf(t, err, "%d samples", samples)
	}
}

// TestEncodeSILKComplexityGatesInterpolation checks that raising complexity
// above silkComplexityInterpolationThreshold reaches SetUseInterpolatedNLSFs
// on the underlying SILK encoder (end to end: encode two frames and confirm
// both still decode cleanly, exercising the interpolation branch inside
// internal/silk once firstFrameAfterReset clears on the second frame).
func TestEncodeSILKComplexityGatesInterpolation(t *testing.T) {
	enc, err := NewEncoder(WithComplexity(8))
	require.NoError(t, err)

	pcm := make([]int16, BandwidthWideband.SampleRate()/50) // 20 ms
	for i := range pcm {
		pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
	}

	packet := make([]byte, maxOpusFrameSize)
	for range 2 {
		n, encErr := enc.EncodeSILK(pcm, BandwidthWideband, packet)
		require.NoError(t, encErr)

		dec, decErr := NewDecoderWithOutput(BandwidthWideband.SampleRate(), 1)
		require.NoError(t, decErr)
		out := make([]float32, len(pcm))
		_, decErr = dec.DecodeToFloat32(packet[:n], out)
		require.NoError(t, decErr)
	}
}

func TestEncodeSILKInvalidBandwidth(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	for _, bandwidth := range []Bandwidth{BandwidthSuperwideband, BandwidthFullband, BandwidthAuto} {
		_, err = enc.EncodeSILK(make([]int16, 320), bandwidth, make([]byte, maxOpusFrameSize))
		assert.ErrorIsf(t, err, errInvalidBandwidth, "bandwidth %d", bandwidth)
	}
}

func TestEncodeSILKOutBufferTooSmall(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	pcm := make([]int16, BandwidthWideband.SampleRate()/50)
	_, err = enc.EncodeSILK(pcm, BandwidthWideband, make([]byte, 1))
	require.Error(t, err)
}

// TestSILKDCBlockRemovesConstantOffset mirrors celt's TestDCBlockRemovesConstantOffset:
// after 1 s of constant input the filter should settle to near zero.
func TestSILKDCBlockRemovesConstantOffset(t *testing.T) {
	const sampleRate = 16000
	pcm := make([]int16, sampleRate)
	for i := range pcm {
		pcm[i] = 10000
	}
	var mem float32
	out := applySILKDCBlock(pcm, sampleRate, &mem)

	var sum float64
	for i := len(out) - 1600; i < len(out); i++ {
		sum += float64(out[i])
	}
	assert.InDelta(t, 0.0, sum/1600, 100.0,
		"DC filter should attenuate constant offset (mean=%f)", sum/1600)
}

// TestSILKDCBlockPreservesInput checks the original pcm slice is untouched
// (applySILKDCBlock returns a new slice, unlike celt's in-place applyDCBlock).
func TestSILKDCBlockPreservesInput(t *testing.T) {
	pcm := []int16{5000, 5000, 5000, 5000}
	original := append([]int16(nil), pcm...)
	var mem float32
	applySILKDCBlock(pcm, 16000, &mem)
	assert.Equal(t, original, pcm)
}

// TestSILKDCBlockSaturates checks the clamp branches: a step between the
// int16 extremes while mem sits at the opposite extreme pushes y outside
// [-32768, 32767], which must saturate instead of wrapping.
func TestSILKDCBlockSaturates(t *testing.T) {
	memHigh := float32(-32768)
	outHigh := applySILKDCBlock([]int16{32767}, 16000, &memHigh)
	assert.Equal(t, int16(32767), outHigh[0])

	memLow := float32(32767)
	outLow := applySILKDCBlock([]int16{-32768}, 16000, &memLow)
	assert.Equal(t, int16(-32768), outLow[0])
}

// TestSILKDCBlockMultiFrameState checks the filter state persists correctly
// across calls: two half-frame runs must match one full run.
func TestSILKDCBlockMultiFrameState(t *testing.T) {
	const sampleRate = 16000
	pcm := make([]int16, 320)
	for i := range pcm {
		pcm[i] = int16(3000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}

	var memFull float32
	fullOut := applySILKDCBlock(pcm, sampleRate, &memFull)

	var memSplit float32
	out1 := applySILKDCBlock(pcm[:160], sampleRate, &memSplit)
	out2 := applySILKDCBlock(pcm[160:], sampleRate, &memSplit)
	splitOut := append(append([]int16(nil), out1...), out2...)

	assert.Equal(t, fullOut, splitOut)
}
