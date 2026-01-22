// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main is an example of an Opus decoder that save the output PCM to disk
package main

import (
	"bytes"
	"errors"
	"io"
	"os"

	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

func main() { // nolint:cyclop
	if len(os.Args) != 3 {
		panic("Usage: <in-file> <out-file>")
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}

	ogg, _, err := oggreader.NewWith(file)
	if err != nil {
		panic(err)
	}

	out := make([]byte, 1920)
	fd, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}

	decoder := opus.NewDecoder()
	for {
		segments, _, err := ogg.ParseNextPage()

		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}

		if err != nil {
			panic(err)
		}

		for i := range segments {
			if _, _, err = decoder.Decode(segments[i], out); err != nil {
				panic(err)
			}

			if _, err := fd.Write(out); err != nil {
				panic(err)
			}
		}
	}
}
