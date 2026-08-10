// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:gosec // G602: slice indices are bounded by combFilterMinPeriod/MaxPeriod and len(pcm).
package celt

// removeDoubling checks whether the detected pitch period T0 is actually
// an octave of the true fundamental. For each sub-multiple k in {2..15},
// it evaluates T1 = (2*T0 + offset) / (2*k) and switches if the normalized
// autocorrelation at T1 exceeds a threshold.
//
// libopus celt/pitch.c: remove_doubling().
//
//nolint:cyclop // Mirrors libopus octave correction chain.

// detectPitch finds the dominant pitch period via normalized autocorrelation
// over [combFilterMinPeriod, combFilterMaxPeriod-2] (RFC 6716 §4.3.7.1 range).
// Normalizing by sqrt(energyCurrent*lagEnergy) avoids the bias toward short lags.
//
// Simpler than libopus pitch_downsample + pitch_search (no LPC whitening,
// no 4x decimation) — enough for clear tonal signals. libopus: celt/pitch.c.
//
//nolint:cyclop

// quantizePitchGain maps gain to the 3-bit RFC 6716 Table 56 grid.
// Mirrors libopus run_prefilter (celt_encoder.c lines 1532-1538).
func quantizePitchGain(gain float32) (qq int, quantized float32) {
	// qg = floor(gain*32/3 + 0.5) - 1, clamped to [0, 7].
	qq = int(float64(gain)*32.0/3.0+0.5) - 1
	qq = max(0, min(7, qq))
	quantized = postFilterGainStep * float32(qq+1)

	return qq, quantized
}

// prefilterDecision implements the gain-threshold logic from libopus
// run_prefilter (celt_encoder.c lines 1499-1540). Returns enabled=false when
// the pre-filter would hurt more than help (low gain, low bitrate, strong
// transient without continuity).
//
//nolint:cyclop // Mirrors libopus threshold chain with multiple conditions.
func prefilterDecision(
	period int, gain float32, prevPeriod int, prevGain float32,
	frameBytes, channels int, tfEstimate float32,
	totalBits, tell uint,
) (enabled bool, qq int, quantizedGain float32) {
	// Bitrate gate: need enough bytes for the ~15-bit post-filter header.
	if frameBytes <= 12*channels {
		return false, 0, 0
	}
	// Bit budget gate: need 16 bits for the enable flag + parameters.
	if tell+16 > totalBits {
		return false, 0, 0
	}

	// Gain threshold: base 0.2, adjusted for continuity and bitrate.
	threshold := float32(0.2)
	if absInt(period-prevPeriod)*10 > period {
		threshold += 0.2
		// Only a very strong transient kills the prefilter outright; the
		// threshold bump above already handles the merely-discontinuous
		// case (celt_encoder.c:1503-1508).
		if tfEstimate > 0.98 {
			return false, 0, 0
		}
	}
	if frameBytes < 25 {
		threshold += 0.1
	}
	if frameBytes < 35 {
		threshold += 0.1
	}
	if prevGain > 0.4 {
		threshold -= 0.1
	}
	if prevGain > 0.55 {
		threshold -= 0.1
	}
	// Hard floor at 0.2.
	if threshold < 0.2 {
		threshold = 0.2
	}

	if gain < threshold {
		return false, 0, 0
	}

	qq, quantizedGain = quantizePitchGain(gain)

	return true, qq, quantizedGain
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}

	return x
}

// applyPrefilter applies the pitch pre-filter before MDCT by calling
// combFilter with negated gains — the inverse of the decoder's post-filter.
// Mirrors libopus run_prefilter (celt_encoder.c lines 1543-1558).
// applyPrefilter whitens src into dst. dst must not alias src: the taps have to
// read the unfiltered input so the filter stays non-recursive and the decoder's
// postfilter inverts it exactly (see combFilter).
func applyPrefilter(
	dst, src []float32,
	oldPeriod, period int,
	n int,
	oldGain, gain float32,
	oldTapset, tapset int,
) {
	start := postfilterHistorySampleCount

	// Clamp periods to the valid range, matching applyPostfilter.
	oldPeriod = max(oldPeriod, combFilterMinPeriod)
	period = max(period, combFilterMinPeriod)

	combFilter(
		dst, src,
		start,
		oldPeriod,
		period,
		n,
		-oldGain,
		-gain,
		oldTapset,
		tapset,
	)
}
