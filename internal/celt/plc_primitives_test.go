// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPLCReferencePrimitives(t *testing.T) {
	file, err := os.Open("../../testdata/short-plc/primitives.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var fixture struct {
		Pin   string
		Cases []struct {
			Signal, Pitch           int
			Input                   [][]float32
			Low, AC, Corrected, LPC []float32
		}
	}
	require.NoError(t, json.NewDecoder(reader).Decode(&fixture))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", fixture.Pin)
	for _, ref := range fixture.Cases {
		t.Run(fmt.Sprintf("signal%d/channels%d", ref.Signal, len(ref.Input)), func(t *testing.T) {
			var scratch plcScratch
			pitchDownsample(ref.Input, scratch.low[:], plcHistorySize/2, 2, &scratch.pitch)
			// Pion accumulates correlations in float64; the pinned generic C
			// kernel uses float32. Check numerical agreement independently of
			// pitch selection and of the public PCM non-regression gate.
			for i, sample := range ref.Low {
				require.InDelta(t, sample, scratch.low[i], 0.001, "downsample %d", i)
			}
			for _, low := range [][]float32{ref.Low, scratch.low[:]} {
				pitch := plcPitchMax - pitchSearch(low[plcPitchMax/2:], low,
					plcHistorySize-plcPitchMax, plcPitchMax-plcPitchMin, &scratch.pitch)
				if ref.Signal == 5 {
					// This exact square wave has equal-correlation harmonic
					// aliases (240/480/720). Rounding can break the tie differently.
					// Prove that BOTH selected periods repeat the entire input,
					// not merely that they are plausible pitch values.
					for _, period := range []int{ref.Pitch, pitch} {
						require.GreaterOrEqual(t, period, plcPitchMin)
						require.LessOrEqual(t, period, plcPitchMax)
						for _, channel := range ref.Input {
							require.Equal(t, channel[:len(channel)-period], channel[period:])
						}
					}
				} else {
					require.Equal(t, ref.Pitch, pitch)
				}
			}
			copy(scratch.windowed[:], ref.Input[0][plcHistorySize-combFilterMaxPeriod:])
			for i := range shortBlockSampleCount {
				scratch.windowed[i] *= celtWindow120[i]
				scratch.windowed[combFilterMaxPeriod-1-i] *= celtWindow120[i]
			}
			correlation := celtAutocorr(scratch.windowed[:], plcLPCOrder, scratch.ac[:])
			for i, value := range ref.AC {
				require.InDelta(t, value, correlation[i], max(1e-8, math.Abs(float64(ref.AC[0]))*1e-5), "autocorr %d", i)
			}
			var coefficients [plcLPCOrder]float32
			celtLPC(ref.Corrected, plcLPCOrder, coefficients[:])
			for i, value := range ref.LPC {
				// Fused ARM arithmetic differs by about 1e-5 in small
				// coefficients on the nearly singular square-wave case.
				require.InDelta(t, value, coefficients[i], 2e-5, "LPC %d", i)
			}
		})
	}
}
