// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeSILKRoundTrip encodes a SILK wideband frame with the public
// encoder and decodes the resulting Opus packet with the public decoder,
// checking the packet is well-formed and decodes to non-silent audio.
func TestEncodeSILKRoundTrip(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	pcm := make([]int16, silkWidebandSampleCount)
	for i := range pcm {
		pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
	}

	packet := make([]byte, 1275)
	n, err := enc.EncodeSILK(pcm, BandwidthWideband, packet)
	require.NoError(t, err)
	require.Greater(t, n, 1)
	packet = packet[:n]

	dec, err := NewDecoderWithOutput(16000, 1)
	require.NoError(t, err)

	out := make([]float32, silkWidebandSampleCount)
	got, err := dec.DecodeToFloat32(packet, out)
	require.NoError(t, err)
	require.Positive(t, got)

	var energy float64
	for _, v := range out[:got] {
		energy += float64(v) * float64(v)
	}
	assert.Positive(t, energy, "decoded output is silent")
}

func TestEncodeSILKInvalidLength(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	_, err = enc.EncodeSILK(make([]int16, 100), BandwidthWideband, make([]byte, 1275))
	require.Error(t, err)
}
