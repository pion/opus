// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package oggreader

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

// buildOggFile generates a valid oggfile that can
// be used for tests.
func buildOggContainer() []byte {
	return []byte{
		0x4f, 0x67, 0x67, 0x53, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x8e, 0x9b, 0x20, 0xaa, 0x00, 0x00,
		0x00, 0x00, 0x61, 0xee, 0x61, 0x17, 0x01, 0x13, 0x4f, 0x70,
		0x75, 0x73, 0x48, 0x65, 0x61, 0x64, 0x01, 0x02, 0x00, 0x0f,
		0x80, 0xbb, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4f, 0x67, 0x67,
		0x53, 0x00, 0x00, 0xda, 0x93, 0xc2, 0xd9, 0x00, 0x00, 0x00,
		0x00, 0x8e, 0x9b, 0x20, 0xaa, 0x02, 0x00, 0x00, 0x00, 0x49,
		0x97, 0x03, 0x37, 0x01, 0x05, 0x98, 0x36, 0xbe, 0x88, 0x9e,
	}
}

func TestOggReader_ParseValidHeader(t *testing.T) {
	reader, header, err := NewWith(bytes.NewReader(buildOggContainer()))
	assert.NoError(t, err)
	assert.NotNil(t, reader)

	assert.Equal(t, &OggHeader{
		ChannelMap: 0x0,
		Channels:   0x2,
		OutputGain: 0x0,
		PreSkip:    0xf00,
		SampleRate: 0xbb80,
		Version:    0x1,
	},
		header)
}

func TestOggReader_ParseNextPage(t *testing.T) {
	ogg := bytes.NewReader(buildOggContainer())
	reader, _, err := NewWith(ogg)
	assert.NoError(t, err)
	assert.NotNil(t, reader)

	payload, _, err := reader.ParseNextPage()
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{0x98, 0x36, 0xbe, 0x88, 0x9e}}, payload)

	_, _, err = reader.ParseNextPage()
	assert.ErrorIs(t, io.EOF, err)
}

func TestOggReader_ParseErrors(t *testing.T) {
	t.Run("Assert that Reader isn't nil", func(t *testing.T) {
		_, _, err := NewWith(nil)
		assert.ErrorIs(t, errNilStream, err)
	})

	t.Run("Invalid ID Page Header Signature", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[0] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPageSignature, err)
	})

	t.Run("Invalid ID Page Header Type", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[5] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPageType, err)
	})

	t.Run("Invalid ID Page Payload Length", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[27] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPageLength, err)
	})

	t.Run("Invalid ID Page Payload Length", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[35] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		assert.ErrorIs(t, errBadIDPagePayloadSignature, err)
	})

	t.Run("Invalid Page Checksum", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[22] = 0

		_, _, err := NewWith(bytes.NewReader(ogg))
		assert.ErrorIs(t, errChecksumMismatch, err)
	})
}
