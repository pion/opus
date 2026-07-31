// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func paddingTestSignal() []float32 {
	pcm := make([]float32, maxFrameSampleCount)
	for i := range pcm {
		pcm[i] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000))
	}

	return pcm
}

// TestEncodeFrameFillsRequestedSize checks the encoder emits exactly the frame
// size its bit allocation was derived from. A shorter packet makes the decoder
// derive a different allocation, so the two sides disagree on how many raw bits
// each band's fine energy occupies.
func TestEncodeFrameFillsRequestedSize(t *testing.T) {
	pcm := paddingTestSignal()
	for _, frameBytes := range []int{240, 640, 1275} {
		encoder := NewEncoder()
		packet := make([]byte, frameBytes)
		n, err := encoder.EncodeFrame([][]float32{pcm}, packet, frameBytes, 0, maxBands)
		require.NoErrorf(t, err, "frameBytes %d", frameBytes)
		assert.Equalf(t, frameBytes, n, "packet must fill the requested frame size")
	}
}

// TestEncodeDecodeAgreeOnBandEnergy is the invariant the padding protects: both
// sides run the same allocation, so they must land on identical band energies.
// Fine energy rides on raw bits, which do not disturb the range coder state, so
// a mismatch here would not show up as a FinalRange desync.
func TestEncodeDecodeAgreeOnBandEnergy(t *testing.T) {
	pcm := paddingTestSignal()
	for _, frameBytes := range []int{240, 640, 1275} {
		encoder := NewEncoder()
		decoder := &Decoder{}
		decoder.Reset()
		packet := make([]byte, frameBytes)
		out := make([]float32, maxFrameSampleCount)

		// Three frames so the inter-frame energy prediction is exercised: a
		// divergence compounds through it rather than staying local.
		for frame := range 3 {
			n, err := encoder.EncodeFrame([][]float32{pcm}, packet, frameBytes, 0, maxBands)
			require.NoErrorf(t, err, "frameBytes %d frame %d", frameBytes, frame)
			require.NoErrorf(t,
				decoder.Decode(packet[:n], out, false, 1, maxFrameSampleCount, 0, maxBands),
				"frameBytes %d frame %d", frameBytes, frame)
			require.Equalf(t, encoder.FinalRange(), decoder.FinalRange(),
				"frameBytes %d frame %d: range coder desync", frameBytes, frame)
		}

		for band := range maxBands {
			assert.InDeltaf(t, encoder.previousLogE[0][band], decoder.previousLogE[0][band], 1e-6,
				"frameBytes %d band %d: encoder and decoder disagree on band energy", frameBytes, band)
		}
	}
}
