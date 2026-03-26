// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/pion/opus/pkg/oggreader"
	"github.com/stretchr/testify/assert"
)

// nolint: gochecknoglobals
var (
	testoggfile = flag.String("oggfile", "", "ogg file for benchmark")
	_testogg    struct {
		once sync.Once
		err  error
		data []byte
	}
)

func loadTestOgg(tb testing.TB) []byte {
	tb.Helper()

	if *testoggfile == "" {
		tb.Skip("-oggfile not specified")
	}

	_testogg.once.Do(func() {
		_testogg.data, _testogg.err = os.ReadFile(*testoggfile)
	})
	if _testogg.err != nil {
		tb.Fatal("unable to load -oggfile", _testogg.err)
	}

	return _testogg.data
}

func BenchmarkDecode(b *testing.B) {
	data := loadTestOgg(b)
	b.ResetTimer()
	for range b.N {
		benchmarkData(b, data)
	}
}

func benchmarkData(b *testing.B, data []byte) {
	b.Helper()

	var out [1920]byte
	ogg, _, err := oggreader.NewWith(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}

	decoder := NewDecoder()
	for {
		segments, _, err := ogg.ParseNextPage()

		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}

		if err != nil {
			b.Fatal(err)
		}

		for i := range segments {
			if _, _, err = decoder.Decode(segments[i], out[:]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

//go:embed testdata/tiny.ogg
var tinyogg []byte // nolint: gochecknoglobals

func TestTinyOgg(t *testing.T) {
	var out [1920]byte

	ogg, _, err := oggreader.NewWith(bytes.NewReader(tinyogg))
	assert.NoError(t, err)

	decoder := NewDecoder()
	for {
		segments, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}
		assert.NoError(t, err)

		for i := range segments {
			if _, _, err = decoder.Decode(segments[i], out[:]); err != nil {
				return
			}
		}
	}
}

func TestNewDecoderWithOutput(t *testing.T) {
	decoder, err := NewDecoderWithOutput(16000, 2)
	assert.NoError(t, err)
	assert.Equal(t, 16000, decoder.sampleRate)
	assert.Equal(t, 2, decoder.channels)

	_, err = NewDecoderWithOutput(44100, 1)
	assert.ErrorIs(t, err, errInvalidSampleRate)

	_, err = NewDecoderWithOutput(48000, 3)
	assert.ErrorIs(t, err, errInvalidChannelCount)
}

func TestDecodeToFloat32(t *testing.T) {
	decoder, err := NewDecoderWithOutput(16000, 2)
	assert.NoError(t, err)

	out := make([]float32, 320)
	sampleCount, err := decoder.DecodeToFloat32([]byte{byte(8<<3) | byte(frameCodeOneFrame)}, out)
	assert.NoError(t, err)
	assert.Equal(t, 160, sampleCount)

	_, err = decoder.DecodeToFloat32([]byte{byte(8<<3) | byte(frameCodeOneFrame)}, out[:319])
	assert.ErrorIs(t, err, errOutBufferTooSmall)
}

func TestDecodeToInt16(t *testing.T) {
	decoder, err := NewDecoderWithOutput(8000, 1)
	assert.NoError(t, err)

	out := make([]int16, 80)
	sampleCount, err := decoder.DecodeToInt16([]byte{byte(0<<3) | byte(frameCodeOneFrame)}, out)
	assert.NoError(t, err)
	assert.Equal(t, 80, sampleCount)
}

func TestDecodeSilkFrameDurations(t *testing.T) {
	for _, test := range []struct {
		name          string
		configuration Configuration
		sampleCount   int
	}{
		{name: "10ms", configuration: 8, sampleCount: 160},
		{name: "20ms", configuration: 9, sampleCount: 320},
		{name: "40ms", configuration: 10, sampleCount: 640},
		{name: "60ms", configuration: 11, sampleCount: 960},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDecoder()
			_, _, _, err := decoder.decode([]byte{byte(test.configuration<<3) | byte(frameCodeOneFrame)}, nil)
			assert.NoError(t, err)
			assert.Len(t, decoder.silkBuffer, test.sampleCount)
		})
	}
}

func TestSilkFrameSampleCount(t *testing.T) {
	assert.Equal(t, 80, Configuration(0).silkFrameSampleCount())
	assert.Equal(t, 120, Configuration(4).silkFrameSampleCount())
	assert.Equal(t, 160, Configuration(8).silkFrameSampleCount())
	assert.Equal(t, 0, Configuration(12).silkFrameSampleCount())
	assert.Equal(t, 0, Configuration(16).silkFrameSampleCount())
}
