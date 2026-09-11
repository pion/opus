// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,tagliatelle // Exact reference fixtures retain their canonical JSON field names.
package opus

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSILKStereoRecoveryStagesReference(t *testing.T) {
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	data, err := os.ReadFile("testdata/short-plc/silk-stereo-recovery-stage.json")
	require.NoError(t, err)
	var reference struct {
		Pin          string
		PreResample  []int16 `json:"pre_resample"`
		PostResample []int16 `json:"post_resample"`
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Equal(t, corpus.Pin, reference.Pin)
	scenario := corpus.Cases[552]
	decoder, err := NewDecoderWithOutput(scenario.Rate, scenario.OutputChannels)
	require.NoError(t, err)
	var output []int16
	for step, frame := range scenario.Steps[:12] {
		output = make([]int16, frame.Samples*scenario.OutputChannels)
		if frame.Packet == "" {
			require.NoError(t, decoder.DecodePLC(output), "step %d", step)

			continue
		}
		packet, decodeErr := hex.DecodeString(frame.Packet)
		require.NoError(t, decodeErr)
		count, decodeErr := decoder.DecodeToInt16(packet, output)
		require.NoError(t, decodeErr, "step %d", step)
		require.Equal(t, frame.Samples, count, "step %d", step)
	}
	require.Len(t, reference.PreResample, len(decoder.silkBuffer))
	for i, sample := range decoder.silkBuffer {
		require.Equal(t, reference.PreResample[i], decoderFloat32ToSigned16(sample), "pre-resample index %d", i)
	}
	require.Equal(t, reference.PostResample, output)
}

// This is the acceptance gate for the bit-exact follow-up to #246. It must
// remain strict while the implementation is brought into reference agreement.
// A matching entropy range or a low average error cannot satisfy this test.
func TestPLCBitExactCorpus(t *testing.T) {
	if !runPLCBitExactCorpus {
		t.Skip("strict deterministic corpus is covered by non-race jobs")
	}

	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", corpus.Pin)
	require.Len(t, corpus.Cases, 1300)
	totalSteps, totalLosses := 0, 0
	for _, scenario := range corpus.Cases {
		totalSteps += len(scenario.Steps)
		for _, frame := range scenario.Steps {
			if frame.Packet == "" {
				totalLosses++
			}
		}
	}
	require.Equal(t, 28_600, totalSteps)
	require.Equal(t, 10_620, totalLosses)

	for index, scenario := range corpus.Cases {
		t.Run(fmt.Sprintf("%03d/mode%d/signal%d/%dto%d/%d/sequence%d", index, scenario.Mode,
			scenario.Signal, scenario.Channels, scenario.OutputChannels, scenario.Rate, scenario.Sequence), func(t *testing.T) {
			decoder, err := NewDecoderWithOutput(scenario.Rate, scenario.OutputChannels)
			require.NoError(t, err)
			var mismatches [3]int
			var first [3]string
			lost := false
			for step, frame := range scenario.Steps {
				output := make([]int16, frame.Samples*scenario.OutputChannels)
				phase := 0
				if frame.Packet == "" {
					lost, phase = true, 1
					require.NoError(t, decoder.DecodePLC(output))
				} else {
					if lost {
						phase = 2
					}
					packet, err := hex.DecodeString(frame.Packet)
					require.NoError(t, err)
					samples, err := decoder.DecodeToInt16(packet, output)
					require.NoError(t, err)
					require.Equal(t, frame.Samples, samples, "step %d", step)
				}
				require.Equal(t, frame.Range, decoder.rangeFinal, "step %d", step)
				require.Len(t, frame.PCM, len(output))
				for sample, actual := range output {
					if actual == frame.PCM[sample] {
						continue
					}
					mismatches[phase]++
					if first[phase] == "" {
						first[phase] = fmt.Sprintf("step=%d channel=%d sample=%d got=%d want=%d", step,
							sample%scenario.OutputChannels, sample/scenario.OutputChannels, actual, frame.PCM[sample])
					}
				}
			}
			for phase, name := range []string{"before-loss", "loss", "recovery"} {
				if mismatches[phase] != 0 {
					assert.Failf(t, name, "%d unequal samples; first %s", mismatches[phase], first[phase])
				}
			}
		})
	}
}

func TestPLCBitExactRFC8251ClippedRecovery(t *testing.T) {
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))

	scenarioIndex := -1
	for i := range corpus.Cases {
		candidate := &corpus.Cases[i]
		if candidate.Signal == 6 && candidate.Mode == 0 && candidate.Channels == 2 &&
			candidate.OutputChannels == 1 && candidate.Rate == 8000 && candidate.Sequence == 0 {
			scenarioIndex = i

			break
		}
	}
	require.NotEqual(t, -1, scenarioIndex)
	scenario := &corpus.Cases[scenarioIndex]
	decoder, err := NewDecoderWithOutput(scenario.Rate, scenario.OutputChannels)
	require.NoError(t, err)
	for step, frame := range scenario.Steps[:21] {
		output := make([]int16, frame.Samples)
		if frame.Packet == "" {
			require.NoError(t, decoder.DecodePLC(output), "step %d", step)
		} else {
			packet, decodeErr := hex.DecodeString(frame.Packet)
			require.NoError(t, decodeErr)
			count, decodeErr := decoder.DecodeToInt16(packet, output)
			require.NoError(t, decodeErr, "step %d", step)
			require.Equal(t, frame.Samples, count, "step %d", step)
		}
		require.Equal(t, frame.PCM, output, "step %d", step)
	}
}
