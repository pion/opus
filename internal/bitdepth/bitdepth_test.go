// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package bitdepth

import (
	"bytes"
	"testing"
)

func TestConvertFloat32LittleEndianToSigned16LittleEndian(t *testing.T) {
	in := []float32{0.3, 0, .55, .72, -.05}
	out := make([]byte, len(in)*2)

	err := ConvertFloat32LittleEndianToSigned16LittleEndian(in, out, 1)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal([]byte{0x66, 0x26, 0x00, 0x00, 0x65, 0x46, 0x28, 0x5c, 0x99, 0xf9}, out) {
		t.Fatal("buffer mismatch")
	}
}
