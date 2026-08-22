// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLTPScaleControl(t *testing.T) {
	// No packet loss: always minimum scaling (index 0, 15565), regardless of
	// the target SNR — roundLoss is 0, so the product with it never crosses
	// either threshold.
	idx, q14 := ltpScaleControl(20, 10*128, 0, 1, false)
	assert.Equal(t, 0, idx, "no loss: index")
	assert.Equal(t, int32(15565), q14, "no loss: scale")

	// High prediction gain and loss push toward stronger scaling.
	idx, q14 = ltpScaleControl(40, 25*128, 25, 1, false)
	assert.NotEqual(t, 0, idx, "high loss/gain: expected stronger scaling")
	assert.Equal(t, ltpScalesTableQ14[idx], q14, "scale matches table entry")

	// LBRR (low bitrate redundancy) present: roundLoss uses the squared-loss
	// formula instead of the plain product, still with no plain packet loss.
	idx, q14 = ltpScaleControl(40, 18*128, 0, 1, true)
	assert.NotEqual(t, 0, idx, "lbrr: expected stronger scaling")
	assert.Equal(t, ltpScalesTableQ14[idx], q14, "scale matches table entry")

	// Extreme gain and loss cross both thresholds: strongest scaling (index 2).
	idx, q14 = ltpScaleControl(100, 30*128, 100, 3, false)
	assert.Equal(t, 2, idx, "extreme loss/gain: index")
	assert.Equal(t, int32(8192), q14, "extreme loss/gain: scale")
}

func TestLTPScaleForFrame(t *testing.T) {
	const (
		predictionGainDB = float32(1)
		snrDBQ7          = int32(22 * 128)
		packetLoss       = 1
		frameCount       = 3
	)

	oneFrameIndex, _ := ltpScaleControl(predictionGainDB, snrDBQ7, packetLoss, 1, false)
	packetIndex, packetScale := ltpScaleControl(predictionGainDB, snrDBQ7, packetLoss, frameCount, false)
	assert.NotEqual(t, oneFrameIndex, packetIndex, "test vector must be sensitive to frames per packet")

	index, scale := ltpScaleForFrame(
		predictionGainDB, snrDBQ7, packetLoss, frameCount, true,
	)
	assert.Equal(t, packetIndex, index)
	assert.Equal(t, packetScale, scale)

	index, scale = ltpScaleForFrame(100, 30*128, 100, frameCount, false)
	assert.Zero(t, index, "conditional frames do not code an LTP scale index")
	assert.Equal(t, silkLTPScaleQ14, scale)
}
