// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHysteresisDecisionThresholds(t *testing.T) {
	thresholds := [3]float32{0.15, 0.40, 0.65}

	// Clearly below all thresholds → NONE regardless of prev.
	assert.Equal(t, spreadNone, hysteresisDecision(0.0, spreadNormal, thresholds))
	// Clearly above all thresholds → AGGRESSIVE regardless of prev.
	assert.Equal(t, spreadAggressive, hysteresisDecision(1.0, spreadNone, thresholds))
	// Middle → NORMAL when prev is NORMAL.
	assert.Equal(t, spreadNormal, hysteresisDecision(0.50, spreadNormal, thresholds))
}

func TestHysteresisDecisionBias(t *testing.T) {
	thresholds := [3]float32{0.15, 0.40, 0.65}

	// Near the 0.15 boundary (val=0.12): with prevDecision=NONE the upward
	// bias should push it into spreadLight; with prevDecision=AGGRESSIVE it stays NONE.
	near := float32(0.12)
	assert.Equal(t, spreadLight, hysteresisDecision(near, spreadNone, thresholds),
		"upward bias from NONE should cross into LIGHT at val=%.2f", near)
	assert.Equal(t, spreadNone, hysteresisDecision(near, spreadAggressive, thresholds),
		"no bias from AGGRESSIVE should leave val=%.2f as NONE", near)
}

func TestHysteresisDecisionNoBias(t *testing.T) {
	thresholds := [3]float32{0.15, 0.40, 0.65}

	// prevDecision=AGGRESSIVE adds zero bias; prevDecision=NONE adds full hystMag.
	aggressive := hysteresisDecision(0.63, spreadAggressive, thresholds)
	none := hysteresisDecision(0.63, spreadNone, thresholds)
	// 0.63 + 0 = 0.63 < 0.65 → NORMAL; 0.63 + 0.04 = 0.67 > 0.65 → AGGRESSIVE
	assert.Equal(t, spreadNormal, aggressive)
	assert.Equal(t, spreadAggressive, none)
}
