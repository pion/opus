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
