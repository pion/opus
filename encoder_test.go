// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const encoderTestFrameSampleCount = 960

func TestNewEncoder(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	assert.Equal(t, 48000, encoder.sampleRate)
	assert.Equal(t, 1, encoder.channels)
	assert.Equal(t, defaultBitrate, encoder.bitrate)

	_, err = NewEncoder(WithSampleRate(16000))
	assert.ErrorIs(t, err, errInvalidSampleRate)

	encoder, err = NewEncoder(WithChannels(2))
	require.NoError(t, err)
	assert.Equal(t, 2, encoder.channels)
}

func TestNewEncoderOptions(t *testing.T) {
	encoder, err := NewEncoder(
		WithSampleRate(48000),
		WithChannels(1),
		WithBitrate(64000),
		WithComplexity(5),
	)
	require.NoError(t, err)

	assert.Equal(t, 64000, encoder.bitrate)
	assert.Equal(t, 5, encoder.complexity)

	_, err = NewEncoder(WithBitrate(1000))
	assert.ErrorIs(t, err, errBitrateOutOfRange)

	_, err = NewEncoder(WithComplexity(11))
	assert.ErrorIs(t, err, errInvalidComplexity)
}

func TestEncodeFloat32RoundTrip(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	decoder, err := NewDecoderWithOutput(48000, 1)
	require.NoError(t, err)

	pcm := testEncoderSineFloat32()
	packet := make([]byte, 256)

	n, err := encoder.EncodeFloat32(pcm, packet)
	require.NoError(t, err)
	require.Positive(t, n)

	assert.Equal(t, byte(celtOnlyFullband20msConfig<<3)|byte(frameCodeOneFrame), packet[0])

	out := make([]float32, encoderTestFrameSampleCount)
	bandwidth, isStereo, err := decoder.DecodeFloat32(packet[:n], out)
	require.NoError(t, err)

	assert.Equal(t, BandwidthFullband, bandwidth)
	assert.False(t, isStereo)
	assert.Greater(t, vectorEnergyFloat32(out), 1e-6)

	// Output amplitude must stay in a sane range. Opus is perceptual so some
	// overshoot above the input peak is expected, but a sample reaching ±2
	// indicates a gain or scaling defect in the analysis/synthesis pair.
	for i, sample := range out {
		require.InDelta(t, 0, sample, 2.0, "decoded sample %d out of sane amplitude range", i)
	}
}

func TestEncodeS16LERoundTrip(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	decoder, err := NewDecoderWithOutput(48000, 1)
	require.NoError(t, err)

	pcm := testEncoderSineS16LE()
	packet := make([]byte, 256)

	n, err := encoder.Encode(pcm, packet)
	require.NoError(t, err)
	require.Positive(t, n)

	out := make([]float32, encoderTestFrameSampleCount)
	_, _, err = decoder.DecodeFloat32(packet[:n], out)
	require.NoError(t, err)

	assert.Greater(t, vectorEnergyFloat32(out), 1e-6)
}

func TestEncodeFloat32StereoRoundTrip(t *testing.T) {
	encoder, err := NewEncoder(WithChannels(2))
	require.NoError(t, err)

	decoder, err := NewDecoderWithOutput(48000, 2)
	require.NoError(t, err)

	pcm := testEncoderStereoSineFloat32()
	packet := make([]byte, 256)

	n, err := encoder.EncodeFloat32(pcm, packet)
	require.NoError(t, err)
	require.Positive(t, n)

	assert.Equal(t, byte(celtOnlyFullband20msConfig<<3)|byte(frameCodeOneFrame)|(1<<2), packet[0])

	out := make([]float32, encoderTestFrameSampleCount*2)
	bandwidth, isStereo, err := decoder.DecodeFloat32(packet[:n], out)
	require.NoError(t, err)

	assert.Equal(t, BandwidthFullband, bandwidth)
	assert.True(t, isStereo)
	assert.Greater(t, vectorEnergyFloat32(out), 1e-6)

	L := make([]float32, encoderTestFrameSampleCount)
	R := make([]float32, encoderTestFrameSampleCount)
	for i := range encoderTestFrameSampleCount {
		L[i] = out[i*2]
		R[i] = out[i*2+1]
	}
	L440 := freqEnergy(L, 440)
	L660 := freqEnergy(L, 660)
	R440 := freqEnergy(R, 440)
	R660 := freqEnergy(R, 660)
	assert.Greater(t, L440, L660*1.5, "L channel: 440 Hz should dominate over 660 Hz")
	assert.Greater(t, R660, R440*1.5, "R channel: 660 Hz should dominate over 440 Hz")
}

func TestEncodeS16LEStereoRoundTrip(t *testing.T) {
	encoder, err := NewEncoder(WithChannels(2))
	require.NoError(t, err)

	decoder, err := NewDecoderWithOutput(48000, 2)
	require.NoError(t, err)

	pcm := testEncoderStereoSineS16LE()
	packet := make([]byte, 256)

	n, err := encoder.Encode(pcm, packet)
	require.NoError(t, err)
	require.Positive(t, n)

	out := make([]float32, encoderTestFrameSampleCount*2)
	_, isStereo, err := decoder.DecodeFloat32(packet[:n], out)
	require.NoError(t, err)

	assert.True(t, isStereo)
	assert.Greater(t, vectorEnergyFloat32(out), 1e-6)
}

func TestStereoMultiFramePersistence(t *testing.T) {
	encoder, err := NewEncoder(WithChannels(2))
	require.NoError(t, err)

	decoder, err := NewDecoderWithOutput(48000, 2)
	require.NoError(t, err)

	pcm := testEncoderStereoSineFloat32()
	packet := make([]byte, 256)
	out := make([]float32, encoderTestFrameSampleCount*2)

	const frames = 10
	energies := make([]float64, frames)
	for i := range frames {
		n, encErr := encoder.EncodeFloat32(pcm, packet)
		require.NoError(t, encErr, "frame %d encode failed", i)
		require.Positive(t, n)

		_, _, decErr := decoder.DecodeFloat32(packet[:n], out)
		require.NoError(t, decErr, "frame %d decode failed", i)

		energies[i] = vectorEnergyFloat32(out)
		assert.Greater(t, energies[i], 1e-6, "frame %d should have non-zero energy", i)
	}

	for i := 1; i < frames; i++ {
		ratio := energies[i] / energies[0]
		assert.InDelta(t, 1.0, ratio, 0.75,
			"frame %d energy ratio %.3f deviates too far from frame 0", i, ratio)
	}
}

func TestEncodeRejectsInvalidS16LEInputLength(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	_, err = encoder.Encode(make([]byte, 3), make([]byte, 64))
	assert.ErrorIs(t, err, errInvalidInputLength)
}

func TestEncodeFloat32RejectsInvalidFrameSize(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	_, err = encoder.EncodeFloat32(make([]float32, encoderTestFrameSampleCount-1), make([]byte, 64))
	assert.ErrorIs(t, err, errInvalidFrameSize)
}

func TestEncodeRejectsSmallOutputBuffer(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	pcm := testEncoderSineFloat32()
	packet := make([]byte, 8)

	_, err = encoder.EncodeFloat32(pcm, packet)
	assert.ErrorIs(t, err, errOutBufferTooSmall)
}

func TestSetBitrate(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	require.NoError(t, encoder.SetBitrate(32000))
	assert.Equal(t, 32000, encoder.bitrate)

	assert.ErrorIs(t, encoder.SetBitrate(1000), errBitrateOutOfRange)
	assert.ErrorIs(t, encoder.SetBitrate(999999), errBitrateOutOfRange)
}

func TestSetComplexity(t *testing.T) {
	encoder, err := NewEncoder()
	require.NoError(t, err)

	require.NoError(t, encoder.SetComplexity(10))
	assert.Equal(t, 10, encoder.complexity)

	assert.ErrorIs(t, encoder.SetComplexity(-1), errInvalidComplexity)
	assert.ErrorIs(t, encoder.SetComplexity(11), errInvalidComplexity)
}

func testEncoderSineFloat32() []float32 {
	pcm := make([]float32, encoderTestFrameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
	}

	return pcm
}

func testEncoderStereoSineFloat32() []float32 {
	pcm := make([]float32, encoderTestFrameSampleCount*2)
	for i := range encoderTestFrameSampleCount {
		left := float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
		right := float32(math.Sin(2 * math.Pi * 660 * float64(i) / 48000))
		pcm[i*2] = left
		pcm[i*2+1] = right
	}

	return pcm
}

func testEncoderSineS16LE() []byte {
	pcm := make([]byte, encoderTestFrameSampleCount*2)
	for i := range encoderTestFrameSampleCount {
		// math.Round breaks gosec's constant-folding so the int16() conversion
		// is analyzed against a runtime float, not a constant expression.
		sample := int16(math.Round(math.Sin(2*math.Pi*440*float64(i)/48000) * 16000))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample)) //nolint:gosec // G115: little-endian s16 round-trip.
	}

	return pcm
}

func testEncoderStereoSineS16LE() []byte {
	pcm := make([]byte, encoderTestFrameSampleCount*4) // 2 channels × 2 bytes each
	for i := range encoderTestFrameSampleCount {
		left := int16(math.Round(math.Sin(2*math.Pi*440*float64(i)/48000) * 16000))
		right := int16(math.Round(math.Sin(2*math.Pi*660*float64(i)/48000) * 16000))
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(left))    //nolint:gosec // G115
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(right)) //nolint:gosec // G115
	}

	return pcm
}

func vectorEnergyFloat32(x []float32) float64 {
	var e float64
	for _, v := range x {
		e += float64(v * v)
	}

	return math.Sqrt(e)
}

// freqEnergy returns the DFT magnitude at freq Hz over a 48 kHz signal.
// It is phase-invariant so it survives the CELT analysis/synthesis delay.
func freqEnergy(samples []float32, freq float64) float64 {
	var re, im float64
	for i, s := range samples {
		angle := 2 * math.Pi * freq * float64(i) / 48000
		re += float64(s) * math.Cos(angle)
		im += float64(s) * math.Sin(angle)
	}

	return math.Sqrt(re*re+im*im) / float64(len(samples))
}
