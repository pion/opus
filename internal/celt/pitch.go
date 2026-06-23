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
