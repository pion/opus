// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

//nolint:cyclop // Validate the complete reference matrix and both received/lost paths together.
func TestDecoderReviewReference(t *testing.T) {
	file, err := os.Open("testdata/short-plc/review-corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var reference struct {
		Pin   string
		Cases []struct {
			Profile, Channels, Rate int
			Steps                   []struct {
				Bandwidth int
				Clipped   bool
				Packet    string
				Samples   int
				Range     uint32
				PCM       []int16
			}
		}
	}
	jsonReader := json.NewDecoder(reader)
	jsonReader.DisallowUnknownFields()
	require.NoError(t, jsonReader.Decode(&reference))
	require.ErrorIs(t, jsonReader.Decode(new(any)), io.EOF)
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", reference.Pin)
	require.Len(t, reference.Cases, 120)
	for index, scenario := range reference.Cases {
		require.Equal(t, index/10, scenario.Profile)
		require.Equal(t, 1+(index/5)%2, scenario.Channels)
		require.Equal(t, []int{8000, 12000, 16000, 24000, 48000}[index%5], scenario.Rate)
		t.Run(fmt.Sprintf("profile%d/ch%d/rate%d", scenario.Profile, scenario.Channels, scenario.Rate), func(t *testing.T) {
			decoder, err := NewDecoderWithOutput(scenario.Rate, scenario.Channels)
			require.NoError(t, err)
			require.Len(t, scenario.Steps, 12)
			clippedPackets, preservedMemories := 0, 0
			for step, frame := range scenario.Steps {
				require.Equal(t, scenario.Rate/50, frame.Samples)
				require.Equal(t, step == 3 || step == 5 || step == 9 || step == 10, frame.Packet == "")
				out := make([]int16, frame.Samples*scenario.Channels)
				if frame.Packet == "" {
					before := decoder.softClipMem
					require.NoError(t, decoder.DecodePLC(out))
					require.Equal(t, before, decoder.softClipMem, "clipping memory at loss step %d", step)
					if before != (softClipMemory{}) {
						preservedMemories++
					}
				} else {
					packet, err := hex.DecodeString(frame.Packet)
					require.NoError(t, err)
					require.NotEmpty(t, packet)
					config := tableOfContentsHeader(packet[0]).configuration()
					require.Equal(t, frame.Bandwidth-1100, int(config.bandwidth()))
					modes := []configurationMode{
						configurationModeSilkOnly, configurationModeSilkOnly, configurationModeSilkOnly,
						configurationModeHybrid, configurationModeHybrid,
						configurationModeCELTOnly, configurationModeCELTOnly, configurationModeCELTOnly,
						configurationModeCELTOnly, configurationModeSilkOnly, configurationModeSilkOnly,
						configurationModeCELTOnly,
					}
					require.Equal(t, modes[scenario.Profile], config.mode())
					count, err := decoder.DecodeToInt16(packet, out)
					require.NoError(t, err, "step %d", step)
					require.Equal(t, frame.Samples, count)
					if frame.Clipped {
						clippedPackets++
					}
				}
				require.Equal(t, frame.Range, decoder.rangeFinal, "step %d range", step)
				require.Len(t, frame.PCM, len(out))
				for i, sample := range out {
					require.Equal(t, frame.PCM[i], sample, "step %d bandwidth %d sample %d", step, frame.Bandwidth, i)
				}
			}
			if scenario.Profile == 11 {
				require.Positive(t, clippedPackets, "fixture must exercise received clipping")
				require.Positive(t, preservedMemories, "fixture must carry nonzero clipping memory across loss")
			}
		})
	}
}
