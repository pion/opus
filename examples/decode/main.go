package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

func main() {
	decoder := opus.NewDecoder()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	file, err := os.Open(homeDir + "/opus/silk.ogg")
	if err != nil {
		panic(err)
	}

	ogg, _, err := oggreader.NewWith(file)
	if err != nil {
		panic(err)
	}

	out := make([]float64, 1500)
	for {
		pageData, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(pageData, []byte("OpusTags")) {
			continue
		}

		if err != nil {
			panic(err)
		}

		bandwidth, isStereo, err := decoder.Decode(pageData, out)
		if err != nil {
			panic(err)
		}

		fmt.Printf("bandwidth(%s) isStereo(%t) framesCount(%d)\n", bandwidth.String(), isStereo, len(out))
	}
}
