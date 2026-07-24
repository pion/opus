// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEncoderDefaults(t *testing.T) {
	enc := NewEncoder()

	assert.False(t, enc.haveEncoded)
	assert.Equal(t, int32(10), enc.previousLogGain)
	assert.Equal(t, 100, enc.previousLag)
	assert.False(t, enc.isPreviousFrameVoiced)
	assert.True(t, enc.firstFrameAfterReset)
	assert.Equal(t, int32(0), enc.sumLogGainQ7)
	assert.Len(t, enc.prevNLSFq, maxLPCOrder)
	assert.Equal(t, 24000, enc.targetBitrate)
	require.NotNil(t, enc.nsq)
}

func TestResetPredictionStateKeepsExplicitBitrate(t *testing.T) {
	enc := NewEncoder()
	enc.targetBitrate = 32000
	enc.haveEncoded = true
	enc.previousLogGain = 40
	enc.firstFrameAfterReset = false

	enc.resetPredictionState()

	assert.False(t, enc.haveEncoded)
	assert.Equal(t, int32(10), enc.previousLogGain)
	assert.True(t, enc.firstFrameAfterReset)
	// A non-zero bitrate the caller already set is left alone — only the
	// zero-value default (uninitialized Encoder) gets the 24000 fallback.
	assert.Equal(t, 32000, enc.targetBitrate)
}
