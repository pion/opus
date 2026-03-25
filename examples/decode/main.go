// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main is an example of an Opus decoder that save the output PCM to disk
package main

import (
	"bytes"
	"encoding/binary"
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

	file, err := os.Open(os.Args[1]) // #nosec G703
	if err != nil {
		panic(err)
	}

	ogg, _, err := oggreader.NewWith(file)
	if err != nil {
		panic(err)
	}

	out := make([]int16, 960)
	fd, err := os.Create(os.Args[2]) // #nosec G703
	if err != nil {
		panic(err)
	}

	decoder, err := opus.NewDecoder(48000, 1)
	if err != nil {
		panic(err)
	}
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
			n, err := decoder.Decode(segments[i], out)
			if err != nil {
				panic(err)
			}

			if err := binary.Write(fd, binary.LittleEndian, out[:n]); err != nil {
				panic(err)
			}
		}
	}
}
