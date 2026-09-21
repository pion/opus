// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,tagliatelle // Exact reference fixtures retain their canonical JSON field names.
package opus

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reference generator verifies that float decoding followed by lrintf
// matches its int16 API for this unclipped first frame only.
func TestDecoderFirstFrameFloatReference(t *testing.T) {
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	data, err := os.ReadFile("testdata/short-plc/first-float.json")
	require.NoError(t, err)
	var reference struct {
		Pin  string   `json:"pin"`
		Bits []uint32 `json:"bits"`
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Equal(t, corpus.Pin, reference.Pin)
	frame := corpus.Cases[0].Steps[0]
	packet, err := hex.DecodeString(frame.Packet)
	require.NoError(t, err)
	decoder, err := NewDecoderWithOutput(8000, 1)
	require.NoError(t, err)
	output := make([]float32, frame.Samples)
	count, err := decoder.DecodeToFloat32(packet, output)
	require.NoError(t, err)
	require.Equal(t, frame.Samples, count)
	require.Len(t, reference.Bits, count)
	first := -1
	unequal := 0
	for i, sample := range output {
		if math.Float32bits(sample) != reference.Bits[i] {
			unequal++
			if first < 0 {
				first = i
			}
		}
	}
	if first >= 0 {
		assert.Failf(t, "float mismatch", "%d samples; first=%d got=%08x want=%08x; sample 28 scaled got=%.9g want=%.9g",
			unequal, first, math.Float32bits(output[first]), reference.Bits[first],
			output[28]*32768, math.Float32frombits(reference.Bits[28])*32768)
	}
}

func TestDecoderNoLossFloatSequenceReference(t *testing.T) {
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	data, err := os.ReadFile("testdata/short-plc/no-loss-float.json")
	require.NoError(t, err)
	var reference struct {
		Pin   string
		Cases []struct {
			Signal         int
			Channels       int
			OutputChannels int `json:"output_channels"`
			Rate           int
			Mode           int
			Sequence       int
			Frames         [][]uint32
		}
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Equal(t, corpus.Pin, reference.Pin)
	require.Len(t, reference.Cases, 4)
	for _, referenceCase := range reference.Cases {
		name := fmt.Sprintf("%dto%d/%d/sequence%d", referenceCase.Channels,
			referenceCase.OutputChannels, referenceCase.Rate, referenceCase.Sequence)
		t.Run(name, func(t *testing.T) {
			scenarioIndex := -1
			for i := range corpus.Cases {
				candidate := corpus.Cases[i]
				if candidate.Mode == referenceCase.Mode && candidate.Signal == referenceCase.Signal &&
					candidate.Channels == referenceCase.Channels &&
					candidate.OutputChannels == referenceCase.OutputChannels && candidate.Rate == referenceCase.Rate &&
					candidate.Sequence == referenceCase.Sequence {
					scenarioIndex = i

					break
				}
			}
			require.NotEqual(t, -1, scenarioIndex)
			scenario := corpus.Cases[scenarioIndex]
			require.NotEmpty(t, referenceCase.Frames)
			require.LessOrEqual(t, len(referenceCase.Frames), len(scenario.Steps))
			decoder, decoderErr := NewDecoderWithOutput(scenario.Rate, scenario.OutputChannels)
			require.NoError(t, decoderErr)
			for step, expectedFrame := range referenceCase.Frames {
				frame := scenario.Steps[step]
				packet, decodeErr := hex.DecodeString(frame.Packet)
				require.NoError(t, decodeErr)
				output := make([]float32, frame.Samples*scenario.OutputChannels)
				count, decodeErr := decoder.DecodeToFloat32(packet, output)
				require.NoError(t, decodeErr)
				require.Equal(t, frame.Samples, count)
				require.Len(t, expectedFrame, len(output))
				for sample, actual := range output {
					expected := expectedFrame[sample]
					if math.Float32bits(actual) != expected {
						assert.Failf(t, "float mismatch", "step=%d channel=%d sample=%d got=%08x want=%08x",
							step, sample%scenario.OutputChannels, sample/scenario.OutputChannels,
							math.Float32bits(actual), expected)

						return
					}
				}
			}
		})
	}
}

func TestDecoderReferenceInt16Rounding(t *testing.T) {
	for _, test := range []struct {
		name     string
		scaled   float32
		expected int16
	}{
		{"positive even tie", 1014.5, 1014},
		{"positive odd tie", 1015.5, 1016},
		{"negative even tie", -734.5, -734},
		{"negative odd tie", -733.5, -734},
		{"positive saturation", 32768, 32767},
		{"negative saturation", -32769, -32768},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, decoderFloat32ToSigned16(test.scaled/32768))
		})
	}
}

func TestSILKFirstFrameStagesReference(t *testing.T) {
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	data, err := os.ReadFile("testdata/short-plc/silk-stage.json")
	require.NoError(t, err)
	var reference struct {
		Pin          string
		PreResample  []int16 `json:"pre_resample"`
		PostResample []int16 `json:"post_resample"`
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Equal(t, corpus.Pin, reference.Pin)
	scenario := corpus.Cases[490]
	packet, err := hex.DecodeString(scenario.Steps[0].Packet)
	require.NoError(t, err)
	decoder, err := NewDecoderWithOutput(16000, 1)
	require.NoError(t, err)
	output := make([]float32, scenario.Steps[0].Samples)
	count, err := decoder.DecodeToFloat32(packet, output)
	require.NoError(t, err)
	require.Equal(t, scenario.Steps[0].Samples, count)
	require.Len(t, reference.PreResample, len(decoder.silkBuffer))
	for i, sample := range decoder.silkBuffer {
		require.Equal(t, reference.PreResample[i], decoderFloat32ToSigned16(sample), "pre-resample index %d", i)
	}
	for i, sample := range output {
		require.Equal(t, reference.PostResample[i], decoderFloat32ToSigned16(sample), "post-resample index %d", i)
	}
}
