// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package resample

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUp(t *testing.T) {
	in := []float32{0.3, 0, -.05}
	out := make([]float32, len(in)*4)

	assert.NoError(t, Up(in, out, 1, 4))
	assert.Equal(t, []float32{
		0.3, 0.3, 0.3, 0.3,
		0, 0, 0, 0,
		-.05, -.05, -.05, -.05,
	}, out)
}

func TestUpStereo(t *testing.T) {
	in := []float32{0.3, -.05, .55, .72}
	out := make([]float32, len(in)*2)

	assert.NoError(t, Up(in, out, 2, 2))
	assert.Equal(t, []float32{
		0.3, -.05,
		0.3, -.05,
		.55, .72,
		.55, .72,
	}, out)
}

func TestUpOutBufferTooSmall(t *testing.T) {
	assert.Error(t, Up([]float32{0.3}, make([]float32, 3), 1, 4))
	assert.Error(t, Up([]float32{0.3}, make([]float32, 1), 2, 1))
}

func TestUpInvalidChannelCount(t *testing.T) {
	assert.Error(t, Up([]float32{0.3}, make([]float32, 1), 0, 1))
}

func TestUpInvalidUpsampleCount(t *testing.T) {
	assert.Error(t, Up([]float32{0.3}, make([]float32, 1), 1, 0))
}
