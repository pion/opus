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
func removeDoubling(
	pcm []float32,
	bestPeriod int,
	bestGain float32,
	prevPeriod int,
	prevGain float32,
) (period int, gain float32) {
	const minPeriod = combFilterMinPeriod
	secondCheck := [16]int{0, 0, 3, 2, 3, 2, 5, 2, 3, 2, 3, 2, 5, 2, 3, 2}

	currentPeriod := bestPeriod
	currentGain := bestGain

	if currentPeriod == 0 || currentGain <= 0 {
		return 0, 0
	}

	n := len(pcm)
	if n < minPeriod+1 {
		return currentPeriod, currentGain
	}

	for k := 2; k <= 15; k++ {
		candidatePeriod := (2*currentPeriod + secondCheck[k]) / (2 * k)
		if candidatePeriod < minPeriod {
			continue
		}

		candidateGain := normalizedCorrelation(pcm, candidatePeriod, n)
		if candidateGain <= 0 {
			continue
		}

		var cont float32
		if prevPeriod > 0 && absInt(candidatePeriod-prevPeriod)*10 < candidatePeriod {
			cont = prevGain
		}

		if candidateGain > currentGain && candidateGain > doublingThreshold(currentGain, cont, candidatePeriod) {
			currentPeriod = candidatePeriod
			currentGain = candidateGain
		}
	}

	// Sanity: if period is very short, check 5/8 and 6/8 sub-harmonics.
	if currentPeriod < 2*minPeriod && currentPeriod >= 5 {
		gainFiveEighths := normalizedCorrelation(pcm, currentPeriod*5/8, n)
		gainSixEighths := normalizedCorrelation(pcm, currentPeriod*6/8, n)
		if gainFiveEighths > currentGain || gainSixEighths > currentGain {
			return 0, 0
		}
	}

	return currentPeriod, currentGain
}

func doublingThreshold(gain float32, continuity float32, period int) float32 {
	threshold := max(float32(0.3), 0.7*gain-continuity)
	if period < 3*combFilterMinPeriod {
		threshold = max(float32(0.4), 0.85*gain-continuity)
	}
	if period < 2*combFilterMinPeriod {
		threshold = max(float32(0.5), 0.9*gain-continuity)
	}

	return threshold
}

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
		denom := energyCurrent + lagEnergy
		if denom > 1e-30 {
			var xcorr float64
			for i := lag; i < sampleCount; i++ {
				xcorr += float64(pcm[i]) * float64(pcm[i-lag])
			}

			normGain := 2 * xcorr / denom
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

func shouldCancelPrefilter(
	pcm [][]float32,
	sampleRate int,
	state *analysisState,
	period int,
	gain float32,
	tapset int,
) bool {
	var before [2]float64
	var after [2]float64
	for ch := range pcm {
		pre := state.preScratch[ch][:len(pcm[ch])]
		copy(pre, pcm[ch])
		dcMem := state.dcBlockMem[ch]
		preemphasisMem := state.preemphasisMem[ch]
		applyDCBlock(pre, sampleRate, &dcMem)
		applyPreemphasis(pre, pre, &preemphasisMem)

		buf := state.prefilterBuf[ch][:postfilterHistorySampleCount+len(pre)]
		copy(buf, state.prefilterMem[ch])
		copy(buf[postfilterHistorySampleCount:], pre)
		before[ch] = measureEnergy(buf, postfilterHistorySampleCount, len(pre))
		applyPrefilter(
			buf,
			state.prefilter.oldPeriod, period,
			len(pre),
			state.prefilter.oldGain, gain,
			state.prefilter.oldTapset, tapset,
		)
		after[ch] = measureEnergy(buf, postfilterHistorySampleCount, len(pre))
	}

	return cancelPitch(len(pcm), gain, before, after)
}

// normalizedCorrelation returns the normalized autocorrelation of pcm at the
// given lag. The normalization is 2*xcorr / (e1+e2) which peaks at 1.0 for
// a perfect match.
func normalizedCorrelation(pcm []float32, lag int, n int) float32 {
	if lag < 1 || lag >= n {
		return 0
	}

	var xcorr, e1, e2 float64
	for i := 0; i < n-lag; i++ {
		xcorr += float64(pcm[i]) * float64(pcm[i+lag])
		e1 += float64(pcm[i]) * float64(pcm[i])
		e2 += float64(pcm[i+lag]) * float64(pcm[i+lag])
	}

	denom := e1 + e2
	if denom < 1e-30 {
		return 0
	}

	r := 2 * xcorr / denom
	if r > 1 {
		r = 1
	}
	if r < 0 {
		r = 0
	}

	return float32(r)
}

// cancelPitch measures whether the pre-filter improved or hurt the signal
// by comparing the sum of absolute samples before and after filtering.
// Returns true when the filter should be reverted.
//
// libopus celt_encoder.c: run_prefilter lines 1548-1584.
func cancelPitch(
	channels int,
	gain float32,
	before [2]float64,
	after [2]float64,
) bool {
	if channels == 1 {
		return after[0] > before[0]
	}

	gain64 := float64(gain)
	thresh0 := 0.25*gain64*before[0] + 0.01*before[1]
	thresh1 := 0.25*gain64*before[1] + 0.01*before[0]

	// Revert if either channel worsened beyond its threshold.
	if after[0]-before[0] > thresh0 || after[1]-before[1] > thresh1 {
		return true
	}

	// Revert if neither channel improved enough.
	if before[0]-after[0] < thresh0 && before[1]-after[1] < thresh1 {
		return true
	}

	return false
}

// measureEnergy returns the sum of absolute values of buf[start:start+n].
func measureEnergy(buf []float32, start, n int) float64 {
	var sum float64
	end := min(start+n, len(buf))
	for i := start; i < end; i++ {
		value := buf[i]
		if value < 0 {
			value = -value
		}
		sum += float64(value)
	}

	return sum
}
