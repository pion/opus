// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle,varnamelen // Reference fixtures retain pinned libopus field names.
package silk

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirstFrameParametersReference(t *testing.T) {
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus struct {
		Cases []struct {
			Steps []struct {
				Packet string
				Range  uint32
			}
		}
	}
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	packet, err := hex.DecodeString(corpus.Cases[490].Steps[0].Packet)
	require.NoError(t, err)
	// The pinned encoder emits this one-frame sample as a padded Code 3
	// packet: TOC, count byte, one-byte padding length, frame, padding.
	// Decode receives only the entropy-coded frame, as the outer Opus
	// decoder has already stripped the packet framing.
	require.Equal(t, byte(0x4b), packet[0])
	require.Equal(t, byte(0x41), packet[1])
	require.Equal(t, byte(1), packet[2])
	packet = packet[3 : len(packet)-1]
	data, err := os.ReadFile("../../testdata/short-plc/silk-stage.json")
	require.NoError(t, err)
	var reference struct {
		Pulses        []int16
		ExcitationQ14 []int32 `json:"excitation_q14"`
		CorePCM       []int16 `json:"core_pcm"`
		PreResample   []int16 `json:"pre_resample"`
		GainsQ16      []int32 `json:"gains_q16"`
		LPCQ12        []int16 `json:"lpc_q12"`
		LTPQ14        []int16 `json:"ltp_q14"`
		Pitch         []int
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	d := NewDecoder()
	out := make([]float32, 320)
	require.NoError(t, d.Decode(packet, out, false, 20_000_000, BandwidthWideband))
	require.Equal(t, corpus.Cases[490].Steps[0].Range, d.rangeDecoder.FinalRange())
	t.Run("pulses", func(t *testing.T) {
		require.Equal(t, reference.Pulses, int32SliceToInt16(d.eRaw))
	})
	t.Run("excitation", func(t *testing.T) {
		require.Len(t, reference.Pulses, len(d.eQ23))
		for i, excitation := range d.eQ23 {
			require.Equal(t, reference.ExcitationQ14[i], excitation<<6, "excitation %d", i)
		}
	})
	t.Run("gains", func(t *testing.T) {
		for i, gain := range d.gainQ16 {
			require.Equal(t, reference.GainsQ16[i], int32(gain), "gain %d", i)
		}
	})
	t.Run("pitch", func(t *testing.T) {
		pitch := d.pitchLags
		if pitch == nil {
			pitch = make([]int, len(reference.Pitch))
		}
		require.Equal(t, reference.Pitch, pitch)
	})
	t.Run("lpc", func(t *testing.T) {
		for set, coefficients := range d.aQ12Int {
			for i, coefficient := range coefficients {
				require.Equal(t, reference.LPCQ12[set*len(reference.LPCQ12)/2+i], coefficient,
					"LPC set %d coefficient %d", set, i)
			}
		}
	})
	t.Run("ltp", func(t *testing.T) {
		for subframe, coefficients := range d.bQ7 {
			for i, coefficient := range coefficients {
				require.Equal(t, reference.LTPQ14[subframe*ltpOrder+i], int16(coefficient)*128,
					"LTP subframe %d coefficient %d", subframe, i)
			}
		}
	})
	t.Run("core PCM", func(t *testing.T) {
		actual := append([]int16(nil), d.fixedPCM[:len(reference.CorePCM)]...)
		require.Equal(t, reference.CorePCM, actual)
	})
	t.Run("pre resample", func(t *testing.T) {
		actual := make([]int16, len(out))
		for i := range out {
			actual[i] = int16(out[i] * 32768)
		}
		require.Equal(t, reference.PreResample, actual)
	})
}

func int32SliceToInt16(values []int32) []int16 {
	result := make([]int16, len(values))
	for i, value := range values {
		result[i] = int16(value)
	}

	return result
}
