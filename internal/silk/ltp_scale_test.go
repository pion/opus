// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLTPScaleControl(t *testing.T) {
	const snrDBQ7 = 18 * 128 // ~18 dB

	// No packet loss: always minimum scaling (index 0, 15565).
	idx, q14 := ltpScaleControl(20, snrDBQ7, 0, 1, false)
	assert.Equal(t, 0, idx, "no loss: index")
	assert.Equal(t, int32(15565), q14, "no loss: scale")

	// High prediction gain and loss push toward stronger scaling.
	idx, q14 = ltpScaleControl(40, snrDBQ7, 25, 1, false)
	assert.NotEqual(t, 0, idx, "high loss/gain: expected stronger scaling")
	assert.Equal(t, ltpScalesTableQ14[idx], q14, "scale matches table entry")
}
