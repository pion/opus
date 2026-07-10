// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// voicedTestSignal builds a multi-frame speech-like signal: a pitch fundamental
// with a few harmonics (so the frame is classified voiced and the LTP path is
// exercised) plus a slow formant-like amplitude modulation.
func voicedTestSignal(fsKHz, frames int) []int16 {
	fs := float64(fsKHz * 1000)
	n := frames * 20 * fsKHz
	out := make([]int16, n)
	pitch := 140.0
	for i := range out {
		t := float64(i) / fs
		s := math.Sin(2*math.Pi*pitch*t) +
			0.5*math.Sin(2*math.Pi*2*pitch*t) +
			0.33*math.Sin(2*math.Pi*3*pitch*t) +
			0.2*math.Sin(2*math.Pi*5*pitch*t)
		env := 0.6 + 0.4*math.Sin(2*math.Pi*3*t)
		out[i] = int16(6000 * env * s / 2.03)
	}

	return out
}

// alignedSNR aligns b to a over a small lag range and returns the best
// energy-weighted SNR in dB over the aligned region.
func alignedSNR(a, b []int16, maxLag int) float64 {
	best := math.Inf(-1)
	lo := 20 * len(a) / 100
	hi := 80 * len(a) / 100
	for lag := range maxLag {
		var sig, noise float64
		for i := lo; i < hi; i++ {
			if i+lag >= len(b) {
				break
			}
			d := float64(a[i]) - float64(b[i+lag])
			sig += float64(a[i]) * float64(a[i])
			noise += d * d
		}
		if noise == 0 {
			return math.Inf(1)
		}
		if snr := 10 * math.Log10(sig/noise); snr > best {
			best = snr
		}
	}

	return best
}

// TestEncodeDecodeFidelity encodes a multi-frame voiced signal frame-by-frame,
// decodes it back and asserts the reconstruction quality per bandwidth. It
// locks in the analysis/quantization pipeline (voiced/LTP, noise shaping, NLSF
// interpolation) end to end.
func TestEncodeDecodeFidelity(t *testing.T) {
	const frames = 25
	cases := []struct {
		bandwidth Bandwidth
		minSNR    float64
	}{
		{BandwidthNarrowband, 12.0},
		{BandwidthMediumband, 12.0},
		{BandwidthWideband, 12.0},
	}
	for _, tc := range cases {
		fsKHz := silkInternalRate(tc.bandwidth)
		frameLen := 20 * fsKHz
		input := voicedTestSignal(fsKHz, frames)

		enc := NewEncoder()
		dec := NewDecoder()
		decoded := make([]int16, 0, len(input))
		for f := range frames {
			frame := input[f*frameLen : (f+1)*frameLen]
			payload := enc.Encode(frame, tc.bandwidth, 24000)

			out := make([]float32, frameLen)
			require.NoErrorf(t, dec.Decode(payload, out, false, nanoseconds20Ms, tc.bandwidth),
				"bandwidth %d frame %d", tc.bandwidth, f)
			for _, v := range out {
				s := math.Round(float64(v) * 32768)
				decoded = append(decoded, int16(math.Max(-32768, math.Min(32767, s))))
			}
		}

		snr := alignedSNR(input, decoded, 16)
		t.Logf("bandwidth %d: SNR %.1f dB", tc.bandwidth, snr)
		require.Greaterf(t, snr, tc.minSNR, "bandwidth %d reconstruction SNR too low", tc.bandwidth)
	}
}
