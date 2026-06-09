// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main is an example of an Opus decoder that from webm save the output PCM to disk
package main

import (
	"os"

	"github.com/at-wat/ebml-go"
	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/opus"
)

func main() { // nolint:cyclop
	if len(os.Args) != 3 {
		panic("Usage: <in-file> <out-file>")
	}

	inputFile, err := os.Open(os.Args[1]) // #nosec G703
	if err != nil {
		panic(err)
	}

	var webmData struct {
		Segment webm.Segment `ebml:"Segment"`
	}

	err = ebml.Unmarshal(inputFile, &webmData)
	if err != nil {
		panic(err)
	}

	var opusTrackNumber uint64
	for _, trackEntry := range webmData.Segment.Tracks.TrackEntry {
		if trackEntry.CodecID == "A_OPUS" {
			opusTrackNumber = trackEntry.TrackNumber
		}
	}

	if opusTrackNumber == 0 {
		panic("Missing opus track")
	}

	buffer := make([]byte, 1920)
	outputFile, err := os.Create(os.Args[2]) // #nosec G703
	if err != nil {
		panic(err)
	}

	decoder := opus.NewDecoder()
	for _, cluster := range webmData.Segment.Cluster {
		for _, simpleBlock := range cluster.SimpleBlock {
			if simpleBlock.TrackNumber != opusTrackNumber {
				continue
			}

			for _, data := range simpleBlock.Data {
				_, _, err = decoder.Decode(data, buffer)
				if err != nil {
					panic(err)
				}

				_, err := outputFile.Write(buffer)
				if err != nil {
					panic(err)
				}
			}
		}
	}
}
