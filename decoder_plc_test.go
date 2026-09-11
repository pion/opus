// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func shortPLCPackets(t *testing.T, channels int) [][]byte {
	t.Helper()
	encoder, err := NewEncoder(WithChannels(channels))
	require.NoError(t, err)
	packets := make([][]byte, 3)
	for frame := range packets {
		pcm := make([]float32, 960*channels)
		for i := range 960 {
			for channel := range channels {
				pcm[i*channels+channel] = float32((i+frame*960)%(80+channel*31)-40) / 200
			}
		}
		packet := make([]byte, maxOpusFrameSize)
		written, err := encoder.EncodeFloat32(pcm, packet)
		require.NoError(t, err)
		packets[frame] = packet[:written]
	}

	return packets
}

func primeShortPLC(t *testing.T, rate, channels int, packet []byte) Decoder {
	t.Helper()
	decoder, err := NewDecoderWithOutput(rate, channels)
	require.NoError(t, err)
	written, err := decoder.DecodeToInt16(packet, make([]int16, rate/50*channels))
	require.NoError(t, err)
	require.Equal(t, rate/50, written)
	require.Equal(t, configurationModeCELTOnly, decoder.previousMode)

	return decoder
}

// Compare public PLC to a duration-sized invocation of the existing CELT core,
// including the recovery packet. A 20 ms synthesis followed by trimming would
// produce different history and fail this test.
func TestDecodePLCShortCELT(t *testing.T) {
	for _, streamChannels := range []int{1, 2} {
		packets := shortPLCPackets(t, streamChannels)
		for _, rate := range []int{8000, 12000, 16000, 24000, 48000} {
			for _, channels := range []int{1, 2} {
				for _, divisor := range []int{400, 200, 100, 50} {
					name := fmt.Sprintf("stream%d/%dHz/output%d/%dus", streamChannels, rate, channels, 1000000/divisor)
					t.Run(name, func(t *testing.T) {
						decoder := primeShortPLC(t, rate, channels, packets[0])
						control := primeShortPLC(t, rate, channels, packets[0])
						count := rate / divisor * channels
						for range 3 {
							guarded := make([]int16, count+2)
							guarded[0], guarded[count+1] = 1234, -1234
							require.NoError(t, decoder.DecodePLC(guarded[1:count+1]))
							expectedFloat := make([]float32, count)
							require.NoError(t, control.decodeCeltPLCFrame(expectedFloat, count/channels, false))
							expected := make([]int16, count)
							float32ToInt16(expectedFloat, expected, count)
							require.Equal(t, expected, guarded[1:count+1])
							require.Equal(t, int16(1234), guarded[0])
							require.Equal(t, int16(-1234), guarded[count+1])
							require.Zero(t, decoder.rangeFinal)
						}
						actual := make([]int16, rate/50*channels)
						expected := make([]int16, len(actual))
						for _, packet := range packets[1:] {
							n, err := decoder.DecodeToInt16(packet, actual)
							require.NoError(t, err)
							require.Equal(t, rate/50, n)
							_, err = control.DecodeToInt16(packet, expected)
							require.NoError(t, err)
							require.Equal(t, expected, actual)
							require.Equal(t, control.rangeFinal, decoder.rangeFinal)
						}
					})
				}
			}
		}
	}
}

func TestDecodePLCSplitsAtLastCELTFrameDuration(t *testing.T) {
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	packet, err := hex.DecodeString(corpus.Cases[3].Steps[14].Packet)
	require.NoError(t, err)

	aggregated, err := NewDecoderWithOutput(8000, 1)
	require.NoError(t, err)
	segmented, err := NewDecoderWithOutput(8000, 1)
	require.NoError(t, err)
	for _, decoder := range []*Decoder{&aggregated, &segmented} {
		count, decodeErr := decoder.DecodeToInt16(packet, make([]int16, 80))
		require.NoError(t, decodeErr)
		require.Equal(t, 80, count)
		require.Equal(t, 80, decoder.lastPacketFrameSamples)
	}

	actual := make([]int16, 160)
	require.NoError(t, aggregated.DecodePLC(actual))
	expected := make([]int16, 160)
	require.NoError(t, segmented.DecodePLC(expected[:80]))
	require.NoError(t, segmented.DecodePLC(expected[80:]))
	require.Equal(t, expected, actual)
	require.True(t, reflect.DeepEqual(segmented.celtDecoder, aggregated.celtDecoder))
}

func TestDecodePLCShortCELTInvalidLengthPreservesState(t *testing.T) {
	packets := shortPLCPackets(t, 2)
	for _, rate := range []int{8000, 12000, 16000, 24000, 48000} {
		for _, channels := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d/%d", rate, channels), func(t *testing.T) {
				decoder := primeShortPLC(t, rate, channels, packets[0])
				control := primeShortPLC(t, rate, channels, packets[0])
				shortest := rate / 400 * channels
				for _, count := range []int{0, 1, shortest - 1, shortest + 1, shortest * 3, shortest * 16, shortest * 24} {
					out := make([]int16, count)
					for i := range out {
						out[i] = 1234
					}
					require.ErrorIs(t, decoder.DecodePLC(out), errInvalidPLCFrameSize)
					for _, sample := range out {
						require.Equal(t, int16(1234), sample)
					}
					require.True(t, reflect.DeepEqual(control, decoder), "rejected request must not change decoder")
				}
				actual, expected := make([]int16, rate/50*channels), make([]int16, rate/50*channels)
				_, err := decoder.DecodeToInt16(packets[1], actual)
				require.NoError(t, err)
				_, err = control.DecodeToInt16(packets[1], expected)
				require.NoError(t, err)
				require.Equal(t, expected, actual)
			})
		}
	}
}

func TestDecodePLCShortModeContract(t *testing.T) {
	for _, test := range []struct {
		mode                    configurationMode
		redundancy, short, long bool
	}{
		{0, false, false, false},
		{0, true, false, false},
		{configurationModeSilkOnly, false, false, true},
		{configurationModeSilkOnly, true, true, false},
		{configurationModeHybrid, false, false, false},
		{configurationModeHybrid, true, true, false},
		{configurationModeCELTOnly, false, true, false},
		{configurationModeCELTOnly, true, true, false},
	} {
		t.Run(fmt.Sprintf("mode%d/redundancy%t", test.mode, test.redundancy), func(t *testing.T) {
			decoder := NewDecoder()
			decoder.previousMode, decoder.previousRedundancy = test.mode, test.redundancy
			for _, samples := range []int{120, 240, 480, 960, 1920, 2880} {
				wantOK := samples == 960 ||
					(samples < 960 && test.short) || (samples > 960 && test.long)
				err := decoder.validatePLCOutput(samples)
				if wantOK {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, errInvalidPLCFrameSize)
				}
			}
		})
	}
}

func TestDecodePLCShortStreamsIndependent(t *testing.T) {
	mono, stereo := shortPLCPackets(t, 1), shortPLCPackets(t, 2)
	first := primeShortPLC(t, 16000, 1, mono[0])
	second := primeShortPLC(t, 48000, 2, stereo[0])
	control := primeShortPLC(t, 48000, 2, stereo[0])
	for _, divisor := range []int{400, 100, 200} {
		require.NoError(t, first.DecodePLC(make([]int16, 16000/divisor)))
		actual, expected := make([]int16, 48000/divisor*2), make([]int16, 48000/divisor*2)
		require.NoError(t, second.DecodePLC(actual))
		require.NoError(t, control.DecodePLC(expected))
		require.Equal(t, expected, actual)
	}
	actual, expected := make([]int16, 1920), make([]int16, 1920)
	_, err := second.DecodeToInt16(stereo[1], actual)
	require.NoError(t, err)
	_, err = control.DecodeToInt16(stereo[1], expected)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestDecodePLCShortUnprimed(t *testing.T) {
	for _, rate := range []int{8000, 12000, 16000, 24000, 48000} {
		for _, channels := range []int{1, 2} {
			decoder, err := NewDecoderWithOutput(rate, channels)
			require.NoError(t, err)
			control, err := NewDecoderWithOutput(rate, channels)
			require.NoError(t, err)
			for _, divisor := range []int{400, 200, 100} {
				out := make([]int16, rate/divisor*channels)
				out[0] = 1234
				require.ErrorIs(t, decoder.DecodePLC(out), errInvalidPLCFrameSize)
				require.Equal(t, int16(1234), out[0])
				require.True(t, reflect.DeepEqual(control, decoder))
			}
			out := make([]int16, rate/50*channels)
			out[0] = 1234
			require.NoError(t, decoder.DecodePLC(out))
			require.Equal(t, make([]int16, len(out)), out)
		}
	}
}

// The reference fixture measures PCM differences, not bit-identical PLC.
// Both decoders now implement periodic and noise-based concealment.
// Range equality on valid recovery packets checks bitstream decoding; it does
// not imply that synthesis history or recovery PCM are identical.
func TestDecodePLCShortLibopus(t *testing.T) {
	// Measured before the periodic PLC change, at c0d7ee63cecdc35aa81b83cbde40c70148da9e74.
	baseline := [11][5]float64{
		{411.123, 598.648, 930.163, 1123.253, 741.343},
		{907.655, 1089.174, 1008.213, 1023.022, 747.966},
		{1322.234, 1372.677, 1002.722, 1013.598, 744.928},
		{1363.665, 1329.767, 1241.183, 1111.643, 743.134},
		{1921.019, 1953.856, 1564.685, 1652.217, 947.452},
		{2583.418, 1812.759, 1528.360, 1387.674, 867.552},
		{2158.892, 2029.657, 1226.874, 1250.920, 813.806},
		{2356.166, 1818.070, 1468.578, 1225.252, 780.990},
		{1609.671, 2054.622, 1152.518, 1196.385, 1321.388},
		{2020.477, 2030.038, 906.738, 1196.104, 1341.551},
		{2199.725, 1915.222, 1210.131, 1203.665, 1340.951},
	}
	var fixture struct {
		Pin   string `json:"pin"`
		Cases []struct {
			Channels   int  `json:"channels"`
			Rate       int  `json:"rate"`
			Divisor    int  `json:"divisor"`
			Transition bool `json:"transition"`
			Steps      []struct {
				Packet  string  `json:"packet"`
				Samples int     `json:"samples"`
				Range   uint32  `json:"range"`
				PCM     []int16 `json:"pcm"`
			} `json:"steps"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/short-plc/libopus.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", fixture.Pin)
	require.Len(t, fixture.Cases, 11)
	for caseIndex, test := range fixture.Cases {
		name := fmt.Sprintf("%dch/%dus/transition%t", test.Channels, 1000000/test.Divisor, test.Transition)
		t.Run(name, func(t *testing.T) {
			decoder, err := NewDecoderWithOutput(test.Rate, test.Channels)
			require.NoError(t, err)
			require.Len(t, test.Steps, 8)
			var totalError, baselineError float64
			for index, step := range test.Steps {
				out := make([]int16, step.Samples*test.Channels)
				phase := "seed"
				if step.Packet == "" {
					phase = "loss"
					if test.Transition && index == 3 {
						require.True(t, decoder.previousRedundancy, "fixture must end in CELT redundancy")
						require.Equal(t, configurationModeSilkOnly, decoder.previousMode)
					}
					require.NoError(t, decoder.DecodePLC(out))
				} else {
					if index >= 6 {
						phase = "recovery"
					}
					packet, err := hex.DecodeString(step.Packet)
					require.NoError(t, err)
					n, err := decoder.DecodeToInt16(packet, out)
					require.NoError(t, err)
					require.Equal(t, step.Samples, n)
				}
				require.Equal(t, step.Range, decoder.rangeFinal, "step %d", index)
				require.Len(t, step.PCM, len(out))
				var squared float64
				var peak int
				for i, sample := range out {
					delta := int(sample) - int(step.PCM[i])
					peak = max(peak, int(math.Abs(float64(delta))))
					squared += float64(delta) * float64(delta)
				}
				t.Logf("%s step=%d samples=%d RMSE=%.3f peak=%d (int16 units)",
					phase, index, step.Samples, math.Sqrt(squared/float64(len(out))), peak)
				if index >= 3 {
					prior := baseline[caseIndex][index-3]
					require.LessOrEqual(t, math.Sqrt(squared/float64(len(out))), prior+1)
					totalError += squared
					baselineError += prior * prior * float64(len(out))
				}
			}
			if !test.Transition {
				require.LessOrEqual(t, totalError, baselineError/4, "halve aggregate loss/recovery RMSE")
			}
		})
	}
}
