// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:gosec // G602: slice indices are bounded by combFilterMinPeriod/MaxPeriod and len(pcm).
package celt

import "math"

// detectPitch finds the dominant pitch period via normalized autocorrelation
// over [combFilterMinPeriod, combFilterMaxPeriod-2] (RFC 6716 §4.3.7.1 range).
// Normalizing by sqrt(energyCurrent*lagEnergy) avoids the bias toward short lags.
//
// Simpler than libopus pitch_downsample + pitch_search (no LPC whitening,
// no 4x decimation) — enough for clear tonal signals. libopus: celt/pitch.c.
//
//nolint:cyclop
func detectPitch(pcm []float32) (period int, gain float32) {
	period = combFilterMinPeriod
	gain = 0

	sampleCount := len(pcm)
	if sampleCount < combFilterMinPeriod+1 {
		return period, gain
	}

	maxPeriod := min(combFilterMaxPeriod-2, sampleCount-1)

	// Energy of the current segment pcm[lag..n-1] at lag=combFilterMinPeriod.
	var energyCurrent float64
	for i := combFilterMinPeriod; i < sampleCount; i++ {
		energyCurrent += float64(pcm[i]) * float64(pcm[i])
	}

	// Energy of the lagged segment pcm[0..n-1-lag] at lag=combFilterMinPeriod.
	var lagEnergy float64
	for i := 0; i < sampleCount-combFilterMinPeriod; i++ {
		lagEnergy += float64(pcm[i]) * float64(pcm[i])
	}

	bestGain := 0.0
	bestPeriod := combFilterMinPeriod

	for lag := combFilterMinPeriod; lag <= maxPeriod; lag++ {
		if energyCurrent > 1e-30 && lagEnergy > 1e-30 {
			var xcorr float64
			for i := lag; i < sampleCount; i++ {
				xcorr += float64(pcm[i]) * float64(pcm[i-lag])
			}

			normGain := xcorr / math.Sqrt(energyCurrent*lagEnergy)
			if normGain > bestGain {
				bestGain = normGain
				bestPeriod = lag
			}
		}

		// Shrink both windows by one sample for the next period.
		if lag < maxPeriod {
			energyCurrent -= float64(pcm[lag]) * float64(pcm[lag])
			if energyCurrent < 0 {
				energyCurrent = 0
			}
			idx := sampleCount - lag - 1
			lagEnergy -= float64(pcm[idx]) * float64(pcm[idx])
			if lagEnergy < 0 {
				lagEnergy = 0
			}
		}
	}

	if bestGain < 0 {
		bestGain = 0
	}

	return bestPeriod, float32(bestGain)
}

// quantizePitchGain maps gain to the 3-bit RFC 6716 Table 56 grid.
// Mirrors libopus run_prefilter (celt_encoder.c lines 1532-1538).
func quantizePitchGain(gain float32) (qq int, quantized float32) {
	// qg = floor(gain*32/3 + 0.5) - 1, clamped to [0, 7].
	qq = int(float64(gain)*32.0/3.0+0.5) - 1
	qq = max(0, min(7, qq))
	quantized = postFilterGainStep * float32(qq+1)

	return qq, quantized
}

// tapsetFromSpread maps the spread decision to a pre-filter tapset.
// AGGRESSIVE (tonal) → tapset 2 (strongest), NORMAL → 1, NONE → 0 (lightest).
// This is a simplified version of libopus spreading_decision's hf_sum logic
// (celt/bands.c) — the full HF tonality measure with ±4 hysteresis is left
// for a future PR.
func tapsetFromSpread(spread int) int {
	switch spread {
	case spreadAggressive:
		return 2
	case spreadNormal:
		return 1
	default:
		return 0
	}
}

// prefilterDecision implements the gain-threshold logic from libopus
// run_prefilter (celt_encoder.c lines 1499-1540). Returns enabled=false when
// the pre-filter would hurt more than help (low gain, low bitrate, strong
// transient without continuity).
//
//nolint:cyclop // Mirrors libopus threshold chain with multiple conditions.
func prefilterDecision(
	period int, gain float32, prevPeriod int, prevGain float32,
	frameBytes, channels int, transient bool,
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

	// Strong transient without pitch continuity → disable.
	if transient && absInt(period-prevPeriod)*10 > period {
		return false, 0, 0
	}

	// Gain threshold: base 0.2, adjusted for continuity and bitrate.
	threshold := float32(0.2)
	if absInt(period-prevPeriod)*10 > period {
		threshold += 0.2
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
func applyPrefilter(
	buf []float32,
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
		buf,
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
