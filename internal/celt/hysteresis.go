// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

// hysteresisDecision maps val in [0, 1] to a 0–3 decision level using
// thresholds as the three crossing points.
//
// The bias term mirrors the libopus pattern in spreading_decision
// (celt_encoder.c): when prevDecision is high, val is biased down, so the
// signal must be clearly tonal to stay aggressive; when prevDecision is low,
// val is biased up, so a mildly tonal burst stays active for an extra frame.
// This damps chattering around threshold crossings without needing per-caller
// dead-band logic.
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
