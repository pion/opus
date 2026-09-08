// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Values pin the recovery predictor in celt_decoder.c at libopus commit
// 22244de5a79bd1d6d623c32e72bf1954b56235be. Loss duration is counted in
// 2.5 ms units; missing frames are computed using the RECOVERY frame's LM.
func TestRecoveryEnergyPrediction(t *testing.T) {
	for _, test := range []struct {
		name                           string
		lm, lost                       int
		current, previous, older, want float32
	}{
		{"falling", 3, 8, 4, 5, 6, 2},
		{"older slope dominates", 3, 8, 4, 4, 8, 0},
		{"slope capped", 3, 8, 4, 10, 20, 0},
		{"rising", 3, 8, 8, 6, 7, 6},
		{"floor", 3, 80, -19, -18, -17, -20},
		{"floor before safety", 0, 8, -19, -18, -17, -21.5},
		{"missing capped", 3, 10000, 8, 9, 10, -3},
		{"2.5ms safety", 0, 1, 4, 5, 6, .5},
		{"5ms safety", 1, 2, 4, 5, 6, 1.5},
		{"10ms no safety", 2, 4, 4, 5, 6, 2},
		{"short loss long recovery", 3, 1, 4, 5, 6, 3},
		{"long loss short recovery", 0, 8, 4, 5, 6, -6.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDecoder()
			decoder.lossDuration = test.lost
			for channel := range 2 {
				decoder.previousLogE[channel][3] = test.current
				decoder.previousLogE1[channel][3] = test.previous
				decoder.previousLogE2[channel][3] = test.older
				decoder.previousLogE[channel][2] = 17
				decoder.previousLogE[channel][4] = 19
			}
			info := frameSideInfo{lm: test.lm, startBand: 3, endBand: 4, channelCount: 1}
			decoder.prepareRecoveryEnergy(&info)
			for channel := range 2 {
				require.Equal(t, test.want, decoder.previousLogE[channel][3])
				require.Equal(t, float32(17), decoder.previousLogE[channel][2])
				require.Equal(t, float32(19), decoder.previousLogE[channel][4])
				require.Equal(t, test.previous, decoder.previousLogE1[channel][3])
				require.Equal(t, test.older, decoder.previousLogE2[channel][3])
			}
		})
	}
}

func TestRecoveryEnergyAppliedBeforeCoarseDecoding(t *testing.T) {
	for _, channels := range []int{1, 2} {
		t.Run(fmt.Sprint(channels), func(t *testing.T) {
			decoder, control := NewDecoder(), NewDecoder()
			decoder.lossDuration = 8
			for channel := range 2 {
				for band := range maxBands {
					decoder.previousLogE[channel][band] = 4
					decoder.previousLogE1[channel][band] = 5
					decoder.previousLogE2[channel][band] = 6
					// 20 ms lost, 2.5 ms recovery: 4 - 9*1 - 1.5.
					control.previousLogE[channel][band] = -6.5
				}
			}
			cfg := frameConfig{
				frameSampleCount: shortBlockSampleCount, endBand: maxBands,
				channelCount: channels, outputChannelCount: channels,
			}
			packet := make([]byte, 8)
			got, err := decoder.decodeFrameSideInfo(packet, cfg, nil)
			require.NoError(t, err)
			require.False(t, got.intraEnergy)
			require.False(t, got.silence)
			want, err := control.decodeFrameSideInfo(packet, cfg, nil)
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, control.previousLogE, decoder.previousLogE)
			require.Equal(t, control.FinalRange(), decoder.FinalRange())
		})
	}
}

func TestPLCLossDurationClearedOnRecovery(t *testing.T) {
	for _, test := range []struct {
		name   string
		packet []byte
	}{
		{"normal", make([]byte, 8)},
		{"silence", []byte{0xff, 0xff}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDecoder()
			out := make([]float32, shortBlockSampleCount)
			require.NoError(t, decoder.Decode(nil, out, false, 1, shortBlockSampleCount, 0, maxBands))
			require.Equal(t, 1, decoder.lossDuration)
			require.NoError(t, decoder.Decode(test.packet, out, false, 1, shortBlockSampleCount, 0, maxBands))
			require.Zero(t, decoder.lossDuration)
			require.NoError(t, decoder.Decode(nil, out, false, 1, shortBlockSampleCount, 0, maxBands))
			require.Equal(t, 1, decoder.lossDuration)
		})
	}
}

func TestRecoveryEnergyUnchangedWithoutInterPrediction(t *testing.T) {
	for _, test := range []struct {
		loss  int
		intra bool
	}{{0, false}, {8, true}} {
		decoder := NewDecoder()
		decoder.lossDuration = test.loss
		decoder.previousLogE[0][1] = 7
		before := decoder.previousLogE
		decoder.prepareRecoveryEnergy(&frameSideInfo{lm: 3, endBand: maxBands, intraEnergy: test.intra})
		require.Equal(t, before, decoder.previousLogE)
	}
}

func TestPLCLossDuration(t *testing.T) {
	decoder := NewDecoder()
	for _, samples := range []int{120, 240, 480, 960} {
		t.Run(fmt.Sprint(samples), func(t *testing.T) {
			before := decoder.lossDuration
			require.NoError(t, decoder.Decode(nil, make([]float32, samples), false, 1, samples, 0, maxBands))
			require.Equal(t, before+samples/120, decoder.lossDuration)
		})
	}
	before := decoder.lossDuration
	require.Error(t, decoder.Decode(nil, make([]float32, 123), false, 1, 123, 0, maxBands))
	require.Equal(t, before, decoder.lossDuration)
	decoder.lossDuration = 9999
	require.NoError(t, decoder.Decode(nil, make([]float32, 960), false, 1, 960, 0, maxBands))
	require.Equal(t, 10000, decoder.lossDuration)
	decoder.Reset()
	require.Zero(t, decoder.lossDuration)
}
