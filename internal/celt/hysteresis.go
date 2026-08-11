// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

// hysteresisDecision maps val in [0, 1] to a 0–3 decision level using
// thresholds as the three crossing points.
//
// A small upward bias is added when prevDecision is low (NONE → +hystMag) and
// removed when it is high (AGGRESSIVE → 0). This mirrors the libopus pattern
// in spreading_decision (celt_encoder.c) and prevents chattering: a borderline
// tonal signal that crossed into LIGHT stays there for at least one extra frame.
func hysteresisDecision(val float32, prevDecision int, thresholds [3]float32) int {
	// bias ∈ [0, hystMag]: largest when prev was NONE (0), zero when AGGRESSIVE (3).
	const hystMag = 0.04
	biased := val + float32(3-prevDecision)/3*hystMag
	switch {
	case biased > thresholds[2]:
		return spreadAggressive
	case biased > thresholds[1]:
		return spreadNormal
	case biased > thresholds[0]:
		return spreadLight
	default:
		return spreadNone
	}
}

// intensityThresholds and intensityHysteresis are the reference tables from
// libopus celt_encoder.c, read against equiv_rate in kb/s. Index i is the
// intensity band chosen when the rate clears thresholds[i-1] but not
// thresholds[i].
//
//nolint:gochecknoglobals // Reference tables.
var (
	intensityThresholds = [21]int{1, 2, 3, 4, 5, 6, 7, 8, 16, 24, 36, 44, 50, 56, 62, 67, 72, 79, 88, 106, 134}
	intensityHysteresis = [21]int{1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3, 3, 4, 5, 6, 8, 8}
)

// intensityBandForRate picks the first band coded in intensity stereo, porting
// libopus hysteresis_decision (celt/bands.c). The per-entry hysteresis keeps
// the band from oscillating when the rate sits on a threshold: a move away from
// prev only sticks once the rate clears that entry's margin.
func intensityBandForRate(kbps, prev int) int {
	band := 0
	for ; band < len(intensityThresholds); band++ {
		if kbps < intensityThresholds[band] {
			break
		}
	}
	if band > prev && kbps < intensityThresholds[prev]+intensityHysteresis[prev] {
		band = prev
	}
	if band < prev && kbps > intensityThresholds[prev-1]-intensityHysteresis[prev-1] {
		band = prev
	}

	return band
}
