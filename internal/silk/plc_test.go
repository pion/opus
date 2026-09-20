// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodePLCReference(t *testing.T) {
	data, err := os.ReadFile("../../testdata/short-plc/silk-direct-plc.json")
	require.NoError(t, err)
	var reference struct {
		Pin    string
		Frames [][]int16
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", reference.Pin)
	require.Len(t, reference.Frames, 4)

	decoder := NewDecoder()
	initialPLC := make([]float32, 320)
	require.NoError(t, decoder.DecodePLC(initialPLC, false, 1, nanoseconds20Ms, BandwidthWideband))
	require.Equal(t, make([]float32, len(initialPLC)), initialPLC)

	frames := [][]byte{
		testSilkFrame(),
		{0x07, 0xc9, 0x72, 0x27, 0xe1, 0x44, 0xea, 0x50},
	}
	for step, expected := range reference.Frames {
		actual := make([]float32, len(expected))
		switch step {
		case 0:
			require.NoError(t, decoder.Decode(frames[0], actual, false, nanoseconds20Ms, BandwidthWideband))
		case 1, 2:
			require.NoError(t, decoder.DecodePLC(actual, false, 1, nanoseconds20Ms, BandwidthWideband))
		case 3:
			require.NoError(t, decoder.Decode(frames[1], actual, false, nanoseconds20Ms, BandwidthWideband))
		}
		for i, sample := range actual {
			require.Equal(t, expected[i], int16(sample*32768), "step %d sample %d", step, i)
		}
	}
	require.Zero(t, decoder.plcLossCount)
}

func TestDecodePLCValidation(t *testing.T) {
	decoder := NewDecoder()
	out := make([]float32, 320)

	assert.ErrorIs(t, decoder.DecodePLC(out, false, 0, nanoseconds20Ms, BandwidthWideband), errOutBufferTooSmall)
	assert.ErrorIs(t, decoder.DecodePLC(out, false, 1, 0, BandwidthWideband), errUnsupportedSilkFrameDuration)
	assert.ErrorIs(t, decoder.DecodePLC(out[:319], false, 1, nanoseconds20Ms, BandwidthWideband), errOutBufferTooSmall)
}

func TestDecodePLCBeforeHistory(t *testing.T) {
	for _, stereo := range []bool{false, true} {
		for _, bandwidth := range []Bandwidth{BandwidthNarrowband, BandwidthMediumband, BandwidthWideband} {
			decoder := NewDecoder()
			channels := 1
			if stereo {
				channels = 2
			}
			out := make([]float32, channels*4*decoder.samplesInSubframe(bandwidth))
			for i := range out {
				out[i] = 0.5
			}
			require.NoError(t, decoder.DecodePLC(out, stereo, channels, nanoseconds20Ms, bandwidth))
			require.Equal(t, make([]float32, len(out)), out)
			require.False(t, decoder.haveDecoded)
			require.False(t, decoder.sideDecoder.haveDecoded)
		}
	}
}
