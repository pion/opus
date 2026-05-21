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
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	assert.Equal(t, 48000, encoder.sampleRate)
	assert.Equal(t, 1, encoder.channels)
	assert.Equal(t, defaultBitrate, encoder.bitrate)

	_, err = NewEncoder(16000, 1)
	assert.ErrorIs(t, err, errInvalidSampleRate)

	_, err = NewEncoder(48000, 2)
	assert.ErrorIs(t, err, errInvalidChannelCount)
}

func TestEncodeFloat32RoundTrip(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	decoder, err := NewDecoderWithOutput(48000, 1)
	require.NoError(t, err)

	pcm := testEncoderSineFloat32()
	packet := make([]byte, 256)

	n, err := encoder.EncodeFloat32(pcm, packet)
	require.NoError(t, err)
	require.Positive(t, n)

	assert.Equal(t, byte(celtOnlyFullband20msMonoConfig<<3)|byte(frameCodeOneFrame), packet[0])

	out := make([]float32, encoderTestFrameSampleCount)
	bandwidth, isStereo, err := decoder.DecodeFloat32(packet[:n], out)
	require.NoError(t, err)

	assert.Equal(t, BandwidthFullband, bandwidth)
	assert.False(t, isStereo)
	assert.Greater(t, vectorEnergyFloat32(out), 1e-6)
}

func TestEncodeS16LERoundTrip(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
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

func TestEncodeRejectsInvalidS16LEInputLength(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	_, err = encoder.Encode(make([]byte, 3), make([]byte, 64))
	assert.ErrorIs(t, err, errInvalidInputLength)
}

func TestEncodeFloat32RejectsInvalidFrameSize(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	_, err = encoder.EncodeFloat32(make([]float32, encoderTestFrameSampleCount-1), make([]byte, 64))
	assert.ErrorIs(t, err, errInvalidFrameSize)
}

func TestEncodeRejectsSmallOutputBuffer(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	pcm := testEncoderSineFloat32()
	packet := make([]byte, 8)

	_, err = encoder.EncodeFloat32(pcm, packet)
	assert.ErrorIs(t, err, errOutBufferTooSmall)
}

func TestSetBitrate(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	require.NoError(t, encoder.SetBitrate(32000))
	assert.Equal(t, 32000, encoder.bitrate)

	assert.Error(t, encoder.SetBitrate(1000))
	assert.Error(t, encoder.SetBitrate(999999))
}

func TestSetComplexity(t *testing.T) {
	encoder, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	require.NoError(t, encoder.SetComplexity(10))
	assert.Equal(t, 10, encoder.complexity)

	assert.Error(t, encoder.SetComplexity(-1))
	assert.Error(t, encoder.SetComplexity(11))
}

func testEncoderSineFloat32() []float32 {
	pcm := make([]float32, encoderTestFrameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
	}

	return pcm
}

func testEncoderSineS16LE() []byte {
	pcm := make([]byte, encoderTestFrameSampleCount*2)
	for i := range encoderTestFrameSampleCount {
		sample := int16(16000 * math.Sin(2*math.Pi*440*float64(i)/48000))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample)) //nolint:gosec // G115: little-endian s16 round-trip.
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
