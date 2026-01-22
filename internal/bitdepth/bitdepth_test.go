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

	assert.NoError(t, ConvertFloat32LittleEndianToSigned16LittleEndian(in, out, 1))
	assert.Equal(t, []byte{0x66, 0x26, 0x00, 0x00, 0x65, 0x46, 0x28, 0x5c, 0x99, 0xf9}, out)
}
