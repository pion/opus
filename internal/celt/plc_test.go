// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen,tagliatelle // Reference fixture uses compact channel/state names and snake_case.
package celt

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPLCReferenceState(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Mode, Rate, Channels int
			OutputChannels       int `json:"output_channels"`
			Steps                []struct {
				Packet  string
				Samples int
			}
		}
	}
	open := func(name string) *json.Decoder {
		f, err := os.Open("../../testdata/short-plc/" + name) //nolint:gosec // Constant test fixture names only.
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, f.Close()) })
		z, err := gzip.NewReader(f)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, z.Close()) })

		return json.NewDecoder(z)
	}
	require.NoError(t, open("corpus.json.gz").Decode(&fixture))
	states := open("corpus-state.jsonl.gz")
	for ci, c := range fixture.Cases {
		d := NewDecoder()
		end := maxBands
		for si, step := range c.Steps {
			var ref struct {
				Type, Loss, Skip, Pitch int
				Energy                  []float32
			}
			require.NoError(t, states.Decode(&ref))
			if c.Mode != 0 {
				continue
			}
			var packet []byte
			stereo := c.Channels == 2
			if step.Packet != "" {
				p, err := hex.DecodeString(step.Packet)
				require.NoError(t, err)
				require.Zero(t, p[0]&3)
				end = [4]int{13, 17, 19, 21}[(p[0]>>3-16)>>2]
				stereo = p[0]&4 != 0
				packet = p[1:]
			}
			out := make([]float32, step.Samples*c.OutputChannels)
			require.NoError(t, d.DecodeToSampleRate(packet, out, stereo,
				c.OutputChannels, step.Samples*48000/c.Rate, 0, end, c.Rate))
			label := fmt.Sprintf("case %d step %d", ci, si)
			require.Equal(t, ref.Loss, d.lossDuration, label)
			require.Equal(t, ref.Skip != 0, d.plc.skip, label)
			require.Equal(t, ref.Type == 3, d.plc.periodic, label)
			// Float accumulation and pre-loss PCM rounding can move the final
			// pseudo-interpolation by one sample. Mode/duration remain exact;
			// the public corpus independently gates the resulting PCM error.
			require.InDelta(t, ref.Pitch, d.plc.pitch, 1, label)
			for h, history := range [4][2][maxBands]float32{d.previousLogE, d.previousLogE1, d.previousLogE2, d.plc.background} {
				for ch := range 2 {
					for band := range maxBands {
						require.InDelta(t, ref.Energy[h*2*maxBands+ch*maxBands+band], history[ch][band], 0.001, label)
					}
				}
			}
			for _, v := range out {
				require.False(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0), label)
			}
		}
	}
}

func TestPLCNoiseDoesNotMirrorEnergy(t *testing.T) {
	d := NewDecoder()
	d.previousLogE[0][0], d.previousLogE[1][0] = 4, 3
	require.NoError(t, d.Decode(nil, make([]float32, 120), true, 1, 120, 0, maxBands))
	require.Equal(t, float32(2.5), d.previousLogE[0][0])
	require.Equal(t, float32(3), d.previousLogE[1][0])
}

func FuzzPLCSequence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 4, 4, 4, 4, 4, 0, 4, 0, 0, 4})
	f.Fuzz(func(t *testing.T, operations []byte) {
		d := NewDecoder()
		var out [1920]float32
		for _, op := range operations[:min(len(operations), 100)] {
			if op == 255 {
				d.Reset()

				continue
			}
			size := shortBlockSampleCount << int(op&3)
			var packet []byte
			if op&4 == 0 {
				packet = []byte{0, 0, 0, 0, 0, 0, 0, 0}
			}
			require.NoError(t, d.Decode(packet, out[:size*2], true, 2, size, 0, maxBands))
			for _, v := range out[:size*2] {
				require.False(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0))
			}
		}
	})
}
