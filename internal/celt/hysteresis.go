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
