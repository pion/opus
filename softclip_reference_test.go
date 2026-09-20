// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"compress/gzip"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSoftClipLibopusReference(t *testing.T) {
	file, err := os.Open("testdata/short-plc/softclip-reference.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var reference struct {
		Pin   string
		Cases []struct {
			Channels              int
			Input, Output, Memory []uint32
		}
	}
	require.NoError(t, json.NewDecoder(reader).Decode(&reference))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", reference.Pin)
	require.Len(t, reference.Cases, 256)
	var memory softClipMemory
	for index, frame := range reference.Cases {
		if index == 128 {
			memory = softClipMemory{}
		}
		require.Equal(t, 1+index/128, frame.Channels)
		require.Len(t, frame.Input, 48*frame.Channels)
		require.Len(t, frame.Output, len(frame.Input))
		require.Len(t, frame.Memory, frame.Channels)
		pcm := make([]float32, len(frame.Input))
		for i, bits := range frame.Input {
			pcm[i] = math.Float32frombits(bits)
		}
		softClip(pcm, frame.Channels, &memory)
		for i, sample := range pcm {
			require.Equal(t, frame.Output[i], math.Float32bits(sample), "frame %d sample %d", index, i)
		}
		for channel, bits := range frame.Memory {
			require.Equal(t, bits, math.Float32bits(memory[channel]), "frame %d channel %d memory", index, channel)
		}
	}
}
