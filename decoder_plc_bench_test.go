// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

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
