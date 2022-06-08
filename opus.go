package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pion/opus/internal/oggreader"
)

func main() {
	file, err := os.Open("output.ogg")
	if err != nil {
		panic(err)
	}

	ogg, _, err := oggreader.NewWith(file)
	if err != nil {
		panic(err)
	}

	for {
		pageData, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			panic(err)
		}

		config, isStereo, frames, err := parsePacket(pageData)
		if err != nil {
			panic(err)
		}

		fmt.Printf("Mode(%s) bandwidth(%s) frameDuration(%s) isStereo(%t) framesCount(%d)\n", config.mode(), config.bandwidth(), config.frameDuration(), isStereo, len(frames))
	}
}
