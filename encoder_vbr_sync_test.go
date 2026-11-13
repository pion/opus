// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tones exercise dynalloc boosts that can desynchronize VBR frames when
// the encoder budgets their flags against the nominal frame size.
func TestEncoderVBRDynallocStaysInSync(t *testing.T) {
	for _, tc := range []struct {
		freq       float64
		bitrate    int
		complexity int
		cvbr       bool
	}{
		{120, 48000, 5, false},
		{120, 32000, 10, false},
		{500, 48000, 5, false},
		{1000, 32000, 10, false},
		{1000, 32000, 10, true},
		{18000, 64000, 10, true},
		{18000, 32000, 10, true},
	} {
		encoder, err := NewEncoder(WithChannels(2), WithBitrate(tc.bitrate), WithComplexity(tc.complexity),
			WithVBR(true), WithConstrainedVBR(tc.cvbr), WithBandwidth(BandwidthFullband))
		require.NoError(t, err)
		decoder, err := NewDecoderWithOutput(48000, 2)
		require.NoError(t, err)
		in := make([]byte, 960*4)
		packet := make([]byte, 1276)
		pcm := make([]int16, 960*2)
		for frame := range 25 {
			for i := range 960 {
				v := int16(math.Round(16384 * math.Sin(2*math.Pi*tc.freq*float64(frame*960+i)/48000)))
				binary.LittleEndian.PutUint16(in[4*i:], uint16(v))   //nolint:gosec // s16 bit pattern.
				binary.LittleEndian.PutUint16(in[4*i+2:], uint16(v)) //nolint:gosec // s16 bit pattern.
			}
			n, err := encoder.Encode(in, packet)
			require.NoError(t, err)
			_, err = decoder.DecodeToInt16(packet[:n], pcm)
			require.NoError(t, err)
			require.Equal(t, encoder.celtEncoder.FinalRange(), decoder.rangeFinal,
				"%g Hz %d bps c%d cvbr %v frame %d", tc.freq, tc.bitrate, tc.complexity, tc.cvbr, frame)
		}
	}
}
