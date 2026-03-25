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
	for i := 0; i < b.N; i++ {
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

func TestDecodeUnsupportedSilkRedundancy(t *testing.T) {
	for _, test := range []struct {
		name   string
		packet []byte
	}{
		{
			name: "vector08_packet4",
			packet: []byte{
				0x0c, 0x08, 0xdb, 0xc3, 0x73, 0x77, 0xd2, 0x47, 0x8d, 0x4c,
				0xa4, 0x88, 0xb4, 0x84, 0x00, 0x7e, 0x49, 0x51, 0xf3, 0x0b,
			},
		},
		{
			name: "vector09_packet4",
			packet: []byte{
				0x0c, 0x88, 0x72, 0x8d, 0x45, 0xce, 0xfe, 0x7f, 0xca, 0xff,
				0x96, 0xa7, 0x19, 0xb9, 0x95, 0x67, 0xb7, 0xfa, 0x94, 0xa2,
				0x91, 0x9e, 0x8c, 0x80, 0xf9, 0xa8, 0xd4, 0x3b, 0x90, 0x96,
				0x9c, 0x40, 0x7e, 0x48, 0x43, 0x19, 0xcb, 0x5a, 0x39,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out [1920]byte
			decoder := NewDecoder()
			_, _, err := decoder.Decode(test.packet, out[:])
			assert.ErrorIs(t, err, errUnsupportedSilkRedundancy)
		})
	}
}

func TestBandwidthResampleCount(t *testing.T) {
	assert.Equal(t, 6, BandwidthNarrowband.resampleCount())
	assert.Equal(t, 4, BandwidthMediumband.resampleCount())
	assert.Equal(t, 3, BandwidthWideband.resampleCount())
	assert.Equal(t, 2, BandwidthSuperwideband.resampleCount())
	assert.Equal(t, 1, BandwidthFullband.resampleCount())
	assert.Equal(t, 0, Bandwidth(0).resampleCount())
}
