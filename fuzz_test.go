// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build go1.18
// +build go1.18

package opus

import (
	"bytes"
	_ "embed"
	"errors"
	"io"
	"testing"

	"github.com/pion/opus/pkg/oggreader"
)

func FuzzDecoder(f *testing.F) {
	f.Add([]byte{})
	f.Add(tinyogg)

	f.Fuzz(func(_ *testing.T, data []byte) {
		var out [1920]byte

		ogg, _, err := oggreader.NewWith(bytes.NewReader(data))
		if err != nil {
			return
		}

		decoder := NewDecoder()
		for {
			segments, _, err := ogg.ParseNextPage()
			if errors.Is(err, io.EOF) {
				break
			} else if len(segments) > 0 && bytes.HasPrefix(segments[0], []byte("OpusTags")) {
				continue
			}
			if err != nil {
				return
			}

			for i := range segments {
				if _, _, err = decoder.Decode(segments[i], out[:]); err != nil {
					return
				}
			}
		}
	})
}
