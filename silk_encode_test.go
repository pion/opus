// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
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

func TestEncodeSILKInvalidLength(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	_, err = enc.EncodeSILK(make([]int16, 100), BandwidthWideband, make([]byte, maxOpusFrameSize))
	require.Error(t, err)
}

func TestEncodeSILKInvalidBandwidth(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	for _, bandwidth := range []Bandwidth{BandwidthSuperwideband, BandwidthFullband, BandwidthAuto} {
		_, err = enc.EncodeSILK(make([]int16, 320), bandwidth, make([]byte, maxOpusFrameSize))
		assert.Errorf(t, err, "bandwidth %d", bandwidth)
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
