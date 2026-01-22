// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

type opusReader struct {
	oggFile     *oggreader.OggReader
	opusDecoder opus.Decoder

	decodeBuffer       []byte
	decodeBufferOffset int

	segmentBuffer [][]byte
}

func (o *opusReader) Read(p []byte) (n int, err error) {
	if o.decodeBufferOffset == 0 || o.decodeBufferOffset >= len(o.decodeBuffer) {
		if len(o.segmentBuffer) == 0 {
			for {
				o.segmentBuffer, _, err = o.oggFile.ParseNextPage()
				if err != nil {
					return 0, err
				} else if bytes.HasPrefix(o.segmentBuffer[0], []byte("OpusTags")) {
					continue
				}

				break
			}
		}

		var segment []byte
		segment, o.segmentBuffer = o.segmentBuffer[0], o.segmentBuffer[1:]

		o.decodeBufferOffset = 0
		if _, _, err = o.opusDecoder.Decode(segment, o.decodeBuffer); err != nil {
			panic(err)
		}
	}

	n = copy(p, o.decodeBuffer[o.decodeBufferOffset:])
	o.decodeBufferOffset += n
	return n, nil
}

func main() {
	if len(os.Args) != 2 {
		panic("Usage: <in-file>")
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}

	oggFile, _, err := oggreader.NewWith(file)
	if err != nil {
		panic(err)
	}

	r := &opusReader{
		decodeBuffer: make([]byte, 1920),
		oggFile:      oggFile,
		opusDecoder:  opus.NewDecoder(),
	}

	format := beep.Format{
		SampleRate:  beep.SampleRate(48000),
		NumChannels: 1,
		Precision:   2,
	}

	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

	done := make(chan struct{})
	speaker.Play(beep.Seq(&pcmStream{
		r:   r,
		f:   format,
		buf: make([]byte, 512*format.Width()),
	}, beep.Callback(func() {
		close(done)
	})))
	<-done
}

// pcmStream allows faiface to play PCM directly
type pcmStream struct {
	r   io.Reader
	f   beep.Format
	buf []byte
	len int
	pos int
	err error
}

func (s *pcmStream) Err() error { return s.err }

func (s *pcmStream) Stream(samples [][2]float64) (n int, ok bool) {
	width := s.f.Width()
	// if there's not enough data for a full sample, get more
	if size := s.len - s.pos; size < width {
		// if there's a partial sample, move it to the beginning of the buffer
		if size != 0 {
			copy(s.buf, s.buf[s.pos:s.len])
		}
		s.len = size
		s.pos = 0
		// refill the buffer
		nbytes, err := s.r.Read(s.buf[s.len:])
		if err != nil {
			if err != io.EOF {
				s.err = err
			}
			return n, false
		}
		s.len += nbytes
	}
	// decode as many samples as we can
	for n < len(samples) && s.len-s.pos >= width {
		samples[n], _ = s.f.DecodeSigned(s.buf[s.pos:])
		n++
		s.pos += width
	}
	return n, true
}
