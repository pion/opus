// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestControlSNR(t *testing.T) {
	// Below the table's lowest bucket: id<=0, always 0.
	assert.Equal(t, int32(0), controlSNR(8, 4, 0), "very low rate: NB")
	assert.Equal(t, int32(0), controlSNR(16, 4, 100), "very low rate: WB")

	// Smallest reachable index (id-10==1) for each table.
	assert.Equal(t, int32(silkTargetRateNB21[1])*21, controlSNR(8, 4, 4200), "NB first reachable bucket")
	assert.Equal(t, int32(silkTargetRateMB21[1])*21, controlSNR(12, 4, 4200), "MB first reachable bucket")
	assert.Equal(t, int32(silkTargetRateWB21[1])*21, controlSNR(16, 4, 4200), "WB first reachable bucket")

	// Very high bitrate clamps to the table's last entry.
	assert.Equal(t, int32(silkTargetRateNB21[len(silkTargetRateNB21)-1])*21, controlSNR(8, 4, 200000), "NB clamps high")
	assert.Equal(t, int32(silkTargetRateWB21[len(silkTargetRateWB21)-1])*21, controlSNR(16, 4, 200000), "WB clamps high")

	// nb_subfr==2 shifts the target rate down before the lookup (10 ms frames
	// need less bitrate for the same quality), so the same nominal rate lands
	// on a lower table index than the nb_subfr==4 case above.
	full := controlSNR(16, 4, 4200)
	half := controlSNR(16, 2, 4200)
	assert.LessOrEqualf(t, half, full, "10ms subframe count should not increase the target SNR")
}
