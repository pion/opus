// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,varnamelen,tagliatelle // Table-driven reference corpus with explicit baseline recording.
package opus

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

type plcCorpus struct {
	Pin   string
	Cases []struct {
		Signal, Channels, Rate, Mode, Sequence int
		OutputChannels                         int `json:"output_channels"`
		Steps                                  []struct {
			Packet  string
			Samples int
			Range   uint32
			PCM     []int16
		}
	}
}
type plcMeasurement struct {
	RMSE float64
	Peak int
	Hash string
}

func readPLCBaseline(t *testing.T) [][]plcMeasurement {
	t.Helper()
	baselinePath := "testdata/short-plc/baseline.json"
	compressed := runtime.GOARCH == "arm64"
	if compressed {
		baselinePath = "testdata/short-plc/baseline-arm64.json.gz"
	}
	if compressed && plcBaselineRace {
		baselinePath = "testdata/short-plc/baseline-arm64-race.json.gz"
	}
	if path := os.Getenv("PLC_BASELINE_PATH"); path != "" {
		baselinePath, compressed = path, false
	}
	data, err := os.ReadFile(baselinePath) //nolint:gosec // Explicit offline same-build baseline for CI comparison.
	require.NoError(t, err)
	var baseline [][]plcMeasurement
	if compressed {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		require.NoError(t, err)
		require.NoError(t, json.NewDecoder(reader).Decode(&baseline))
		require.NoError(t, reader.Close())
	} else {
		require.NoError(t, json.Unmarshal(data, &baseline))
	}

	return baseline
}

func TestPLCCorpus(t *testing.T) {
	path := os.Getenv("PLC_CORPUS_PATH")
	if path == "" {
		path = "testdata/short-plc/corpus.json.gz"
	}
	f, err := os.Open(path) //nolint:gosec // Operator-supplied offline fixture path for baseline generation.
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck
	z, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer z.Close() //nolint:errcheck
	var corpus plcCorpus
	require.NoError(t, json.NewDecoder(z).Decode(&corpus))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", corpus.Pin)
	var baseline [][]plcMeasurement
	record := os.Getenv("PLC_BASELINE_OUTPUT")
	if record == "" {
		baseline = readPLCBaseline(t)
		require.Len(t, baseline, len(corpus.Cases))
	}
	results := make([][]plcMeasurement, len(corpus.Cases))
	for ci, c := range corpus.Cases {
		name := fmt.Sprintf("%03d/signal%d/%dto%d/%d/mode%d/sequence%d",
			ci, c.Signal, c.Channels, c.OutputChannels, c.Rate, c.Mode, c.Sequence)
		t.Run(name, func(t *testing.T) {
			d, err := NewDecoderWithOutput(c.Rate, c.OutputChannels)
			require.NoError(t, err)
			var currentError, oldError float64
			results[ci] = make([]plcMeasurement, len(c.Steps))
			for si, s := range c.Steps {
				out := make([]int16, s.Samples*c.OutputChannels)
				if s.Packet == "" {
					require.NoError(t, d.DecodePLC(out))
				} else {
					p, err := hex.DecodeString(s.Packet)
					require.NoError(t, err)
					n, err := d.DecodeToInt16(p, out)
					require.NoError(t, err)
					require.Equal(t, s.Samples, n)
				}
				require.Equal(t, s.Range, d.rangeFinal, "step %d", si)
				require.Len(t, s.PCM, len(out))
				m := plcMeasurement{}
				var squared float64
				bytes := make([]byte, 2*len(out))
				for i, v := range out {
					delta := int(v) - int(s.PCM[i])
					squared += float64(delta) * float64(delta)
					m.Peak = max(m.Peak, int(math.Abs(float64(delta))))
					binary.LittleEndian.PutUint16(bytes[i*2:], uint16(v))
				}
				hash := sha256.Sum256(bytes)
				m.Hash = hex.EncodeToString(hash[:])
				m.RMSE = math.Sqrt(squared / float64(len(out)))
				results[ci][si] = m
				if record == "" {
					b := baseline[ci][si]
					require.LessOrEqual(t, m.RMSE, b.RMSE+1, "step %d: RMSE %.6f vs baseline %.6f", si, m.RMSE, b.RMSE)
					if c.Sequence == 2 || si < 4 {
						require.Equal(t, b.Hash, m.Hash, "no-loss PCM changed, step %d", si)
					}
					if si >= 4 {
						currentError += squared
						oldError += b.RMSE * b.RMSE * float64(len(out))
					}
				}
			}
			if record == "" && c.Mode == 0 && c.Signal < 3 && c.Sequence != 2 {
				require.LessOrEqual(t, currentError, oldError/4, "halve aggregate periodic error")
			}
		})
	}
	if record != "" {
		data, err := json.Marshal(results)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(record, data, 0o600)) //nolint:gosec // Explicit offline baseline output.
	}
}
