// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package bitdepth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertFloat32LittleEndianToSigned16LittleEndian(t *testing.T) {
	in := []float32{0.3, 0, .55, .72, -.05}
	out := make([]byte, len(in)*2)

	assert.NoError(t, ConvertFloat32LittleEndianToSigned16LittleEndian(in, out, 1, 1))
	assert.Equal(t, []byte{0x66, 0x26, 0x00, 0x00, 0x65, 0x46, 0x28, 0x5c, 0x99, 0xf9}, out)
}

func TestConvertFloat32LittleEndianToSigned16LittleEndianOutTooSmall(t *testing.T) {
	assert.ErrorIs(t, errOutBufferTooSmall, ConvertFloat32LittleEndianToSigned16LittleEndian([]float32{0}, nil, 1, 1))
}

func TestConvertFloat32LittleEndianToSigned16LittleEndianResample(t *testing.T) {
	in := []float32{0.3, -.05}
	out := make([]byte, len(in)*4)

	assert.NoError(t, ConvertFloat32LittleEndianToSigned16LittleEndian(in, out, 1, 2))
	assert.Equal(t, []byte{0x66, 0x26, 0x66, 0x26, 0x99, 0xf9, 0x99, 0xf9}, out)
}

func TestConvertFloat32LittleEndianToSigned16LittleEndianStereoResample(t *testing.T) {
	in := []float32{0.3, -.05, .55, .72}
	out := make([]byte, len(in)*4)

	assert.NoError(t, ConvertFloat32LittleEndianToSigned16LittleEndian(in, out, 2, 2))
	assert.Equal(t, []byte{
		0x66, 0x26, 0x99, 0xf9,
		0x66, 0x26, 0x99, 0xf9,
		0x65, 0x46, 0x28, 0x5c,
		0x65, 0x46, 0x28, 0x5c,
	}, out)
}

func TestConvertFloat32LittleEndianToSigned16LittleEndianOutBufferTooSmall(t *testing.T) {
	assert.Error(t, ConvertFloat32LittleEndianToSigned16LittleEndian([]float32{0.3}, make([]byte, 1), 1, 1))
	assert.ErrorIs(
		t,
		errInvalidInputLength,
		ConvertFloat32LittleEndianToSigned16LittleEndian([]float32{0.3}, make([]byte, 2), 2, 1),
	)
}

func TestConvertFloat32LittleEndianToSigned16LittleEndianInvalidChannelCount(t *testing.T) {
	assert.Error(t, ConvertFloat32LittleEndianToSigned16LittleEndian([]float32{0.3}, make([]byte, 2), 0, 1))
}

func TestConvertFloat32LittleEndianToSigned16LittleEndianInvalidResampleCount(t *testing.T) {
	assert.Error(t, ConvertFloat32LittleEndianToSigned16LittleEndian([]float32{0.3}, make([]byte, 2), 1, 0))
}
