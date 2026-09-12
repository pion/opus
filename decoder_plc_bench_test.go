// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop // Cross-product benchmark names remain explicit and searchable.
package opus

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func plcBenchmarkCorpus(tb testing.TB) plcCorpus {
	tb.Helper()
	file, err := os.Open("testdata/short-plc/corpus.json.gz")
	require.NoError(tb, err)
	defer file.Close() //nolint:errcheck
	reader, err := gzip.NewReader(file)
	require.NoError(tb, err)
	defer reader.Close() //nolint:errcheck
	var corpus plcCorpus
	require.NoError(tb, json.NewDecoder(reader).Decode(&corpus))

	return corpus
}

func plcBenchmarkPackets(tb testing.TB, channels int) [][]byte {
	tb.Helper()
	var fixture plcCorpus
	path := os.Getenv("PLC_SHORT_FIXTURE")
	if path == "" {
		path = "testdata/short-plc/libopus.json"
	}
	data, err := os.ReadFile(path) //nolint:gosec // Explicit offline benchmark fixture.
	require.NoError(tb, err)
	require.NoError(tb, json.Unmarshal(data, &fixture))
	packets := make([][]byte, 3)
	for i := range packets {
		packets[i], err = hex.DecodeString(fixture.Cases[(channels-1)*4].Steps[i].Packet)
		require.NoError(tb, err)
	}

	return packets
}

func BenchmarkCELTPLC(b *testing.B) {
	if plcBaselineRace {
		b.Skip("CPU benchmarks are not representative under race instrumentation")
	}
	for _, channels := range []int{1, 2} {
		packets := plcBenchmarkPackets(b, channels)
		for _, scenario := range []struct {
			name        string
			priorLosses int
		}{
			{"normal", -1}, {"first", 0}, {"periodic", 1}, {"noise", 6},
		} {
			b.Run(fmt.Sprintf("%dch/%s", channels, scenario.name), func(b *testing.B) {
				decoder, err := NewDecoderWithOutput(48000, channels)
				require.NoError(b, err)
				out := make([]int16, 960*channels)
				prime := func() {
					for _, packet := range packets {
						_, err = decoder.DecodeToInt16(packet, out)
						require.NoError(b, err)
					}
				}
				prime()
				require.NoError(b, decoder.DecodePLC(out))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					b.StopTimer()
					prime()
					for range max(0, scenario.priorLosses) {
						require.NoError(b, decoder.DecodePLC(out))
					}
					b.StartTimer()
					if scenario.priorLosses < 0 {
						_, err = decoder.DecodeToInt16(packets[2], out)
					} else {
						err = decoder.DecodePLC(out)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkSILKAndHybridPLC(b *testing.B) {
	if plcBaselineRace {
		b.Skip("CPU benchmarks are not representative under race instrumentation")
	}
	corpus := plcBenchmarkCorpus(b)
	for _, mode := range []int{2, 4} {
		modeName := map[int]string{2: "hybrid", 4: "silk"}[mode]
		for _, channels := range []int{1, 2} {
			caseIndex := -1
			for i := range corpus.Cases {
				candidate := &corpus.Cases[i]
				if candidate.Mode == mode && candidate.Signal == 0 && candidate.Channels == channels &&
					candidate.OutputChannels == channels && candidate.Rate == 48000 && candidate.Sequence == 0 {
					caseIndex = i

					break
				}
			}
			require.NotEqual(b, -1, caseIndex)
			scenario := &corpus.Cases[caseIndex]
			packets := make([][]byte, 4)
			for i := range packets {
				var err error
				packets[i], err = hex.DecodeString(scenario.Steps[i].Packet)
				require.NoError(b, err)
			}
			for _, operation := range []struct {
				name        string
				priorLosses int
			}{
				{"normal", -1}, {"first", 0}, {"series", 6},
			} {
				b.Run(fmt.Sprintf("%s/%dch/%s", modeName, channels, operation.name), func(b *testing.B) {
					decoder, err := NewDecoderWithOutput(48000, channels)
					require.NoError(b, err)
					out := make([]int16, 960*channels)
					setup := func() {
						require.NoError(b, decoder.Init(48000, channels))
						for _, packet := range packets[:3] {
							_, err = decoder.DecodeToInt16(packet, out)
							require.NoError(b, err)
						}
						for range max(0, operation.priorLosses) {
							require.NoError(b, decoder.DecodePLC(out))
						}
					}
					setup()
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						b.StopTimer()
						setup()
						b.StartTimer()
						if operation.priorLosses < 0 {
							_, err = decoder.DecodeToInt16(packets[3], out)
						} else {
							err = decoder.DecodePLC(out)
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestCELTPLCWarmAllocations(t *testing.T) {
	for _, channels := range []int{1, 2} {
		packets := plcBenchmarkPackets(t, channels)
		decoder, err := NewDecoderWithOutput(48000, channels)
		require.NoError(t, err)
		out := make([]int16, 1920)
		cycle := func() {
			for _, packet := range packets {
				_, err = decoder.DecodeToInt16(packet, out)
				require.NoError(t, err)
			}
			for range 7 {
				require.NoError(t, decoder.DecodePLC(out[:960*channels]))
			}
		}
		cycle()
		require.Zero(t, testing.AllocsPerRun(100, cycle))
	}
}

func TestSILKAndHybridPLCWarmAllocations(t *testing.T) {
	corpus := plcBenchmarkCorpus(t)
	for _, mode := range []int{2, 4} {
		for _, channels := range []int{1, 2} {
			t.Run(fmt.Sprintf("mode%d/%dch", mode, channels), func(t *testing.T) {
				caseIndex := -1
				for i := range corpus.Cases {
					candidate := &corpus.Cases[i]
					if candidate.Mode == mode && candidate.Signal == 0 && candidate.Channels == channels &&
						candidate.OutputChannels == channels && candidate.Rate == 48000 && candidate.Sequence == 0 {
						caseIndex = i

						break
					}
				}
				require.NotEqual(t, -1, caseIndex)
				decoder, err := NewDecoderWithOutput(48000, channels)
				require.NoError(t, err)
				out := make([]int16, 960*channels)
				for _, frame := range corpus.Cases[caseIndex].Steps[:4] {
					packet, decodeErr := hex.DecodeString(frame.Packet)
					require.NoError(t, decodeErr)
					_, decodeErr = decoder.DecodeToInt16(packet, out)
					require.NoError(t, decodeErr)
				}
				require.NoError(t, decoder.DecodePLC(out))
				require.Zero(t, testing.AllocsPerRun(100, func() {
					require.NoError(t, decoder.DecodePLC(out))
				}))
			})
		}
	}
}

func TestCELTPLCResetAndDecoderIsolation(t *testing.T) {
	for _, channels := range []int{1, 2} {
		packets := plcBenchmarkPackets(t, channels)
		for _, losses := range []int{1, 7} {
			t.Run(fmt.Sprintf("%dch/%dlosses", channels, losses), func(t *testing.T) {
				decoder, err := NewDecoderWithOutput(48000, channels)
				require.NoError(t, err)
				control, err := NewDecoderWithOutput(48000, channels)
				require.NoError(t, err)
				unrelated, err := NewDecoderWithOutput(48000, channels)
				require.NoError(t, err)
				actual, expected, other := make([]int16, 960*channels), make([]int16, 960*channels), make([]int16, 960*channels)
				for _, packet := range packets {
					_, err = decoder.DecodeToInt16(packet, actual)
					require.NoError(t, err)
					_, err = unrelated.DecodeToInt16(packet, other)
					require.NoError(t, err)
				}
				for range losses {
					require.NoError(t, decoder.DecodePLC(actual))
				}
				// Init calls the CELT core's Reset, including periodic history,
				// LPC, background energy, pending overlap and noise skip state.
				require.NoError(t, decoder.Init(48000, channels))
				for step := range 20 {
					// Interleave another live decoder to expose shared scratch/state.
					require.NoError(t, unrelated.DecodePLC(other))
					if step >= 3 && step < 10 {
						require.NoError(t, decoder.DecodePLC(actual))
						require.NoError(t, control.DecodePLC(expected))
					} else {
						clear(actual)
						clear(expected)
						_, err = decoder.DecodeToInt16(packets[step%len(packets)], actual)
						require.NoError(t, err)
						_, err = control.DecodeToInt16(packets[step%len(packets)], expected)
						require.NoError(t, err)
					}
					require.Equal(t, expected, actual, "step %d", step)
					require.Equal(t, control.rangeFinal, decoder.rangeFinal)
				}
			})
		}
	}
}
