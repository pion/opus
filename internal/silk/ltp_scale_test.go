// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "testing"

func TestLTPScaleControl(t *testing.T) {
	const snrDBQ7 = 18 * 128 // ~18 dB

	// No packet loss: always minimum scaling (index 0, 15565).
	if idx, q14 := ltpScaleControl(20, snrDBQ7, 0, 1, false); idx != 0 || q14 != 15565 {
		t.Fatalf("no loss: got index %d scale %d, want 0/15565", idx, q14)
	}

	// High prediction gain and loss push toward stronger scaling.
	idx, q14 := ltpScaleControl(40, snrDBQ7, 25, 1, false)
	if idx == 0 {
		t.Fatalf("high loss/gain: expected stronger scaling, got index 0")
	}
	if q14 != ltpScalesTableQ14[idx] {
		t.Fatalf("scale %d does not match table entry %d", q14, ltpScalesTableQ14[idx])
	}
}
