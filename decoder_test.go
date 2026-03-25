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

func firstTinyOggPacket(tb testing.TB) []byte {
	tb.Helper()

	ogg, _, err := oggreader.NewWith(bytes.NewReader(tinyogg))
	assert.NoError(tb, err)

	for {
		segments, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			tb.Fatal("no audio packet found")
		}
		assert.NoError(tb, err)
		if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}

		return segments[0]
	}
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

	var out [960]int16
	ogg, _, err := oggreader.NewWith(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}

	decoder, err := NewDecoder(48000, 1)
	if err != nil {
		b.Fatal(err)
	}
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
			if _, err = decoder.Decode(segments[i], out[:]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

//go:embed testdata/tiny.ogg
var tinyogg []byte // nolint: gochecknoglobals

func TestTinyOgg(t *testing.T) {
	var out [960]int16

	ogg, _, err := oggreader.NewWith(bytes.NewReader(tinyogg))
	assert.NoError(t, err)

	decoder, err := NewDecoder(48000, 1)
	assert.NoError(t, err)
	for {
		segments, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}
		assert.NoError(t, err)

		for i := range segments {
			if _, err = decoder.Decode(segments[i], out[:]); err != nil {
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
			decoder, err := NewDecoder(48000, 1)
			assert.NoError(t, err)
			_, _, _, err = decoder.decode([]byte{byte(test.configuration<<3) | byte(frameCodeOneFrame)}, nil, false)
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
			var out [960]int16
			decoder, err := NewDecoder(48000, 2)
			assert.NoError(t, err)
			_, err = decoder.Decode(test.packet, out[:])
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

func TestBandwidthResampleDelay(t *testing.T) {
	assert.Equal(t, 4, BandwidthNarrowband.resampleDelay(8000))
	assert.Equal(t, 9, BandwidthMediumband.resampleDelay(12000))
	assert.Equal(t, 12, BandwidthWideband.resampleDelay(16000))
	assert.Equal(t, 26, BandwidthNarrowband.resampleDelay(48000))
	assert.Equal(t, 33, BandwidthMediumband.resampleDelay(48000))
	assert.Equal(t, 34, BandwidthWideband.resampleDelay(48000))
	assert.Equal(t, 0, BandwidthFullband.resampleDelay(48000))
}

func TestApplyResampleDelay(t *testing.T) {
	decoder, err := NewDecoder(16000, 1)
	assert.NoError(t, err)

	out := make([]float32, 16)
	for i := range out {
		out[i] = float32(i + 1)
	}
	decoder.applyResampleDelay(out, len(out), BandwidthWideband)
	assert.Equal(t, []float32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4}, out)

	next := make([]float32, 16)
	for i := range next {
		next[i] = float32(i + 17)
	}
	decoder.applyResampleDelay(next, len(next), BandwidthWideband)
	assert.Equal(t, []float32{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, next)

	decoder.applyResampleDelay(next, len(next), BandwidthNarrowband)
	assert.Equal(t, []float32{0, 0, 0, 0, 0, 0, 0, 0, 0, 5, 6, 7, 8, 9, 10, 11}, next)
}

func TestSilkFrameSampleCountUnsupportedBandwidth(t *testing.T) {
	assert.Equal(t, 0, Configuration(12).silkFrameSampleCount())
	assert.Equal(t, 0, Configuration(14).silkFrameSampleCount())
}

func TestNewDecoderValidation(t *testing.T) {
	_, err := NewDecoder(44100, 1)
	assert.ErrorIs(t, err, errInvalidSampleRate)

	_, err = NewDecoder(48000, 3)
	assert.ErrorIs(t, err, errInvalidChannelCount)

	decoder := Decoder{}
	_, err = decoder.Decode(nil, make([]int16, 0))
	assert.ErrorIs(t, err, errInvalidSampleRate)
}

func TestLastPacketDuration(t *testing.T) {
	decoder, err := NewDecoder(48000, 1)
	assert.NoError(t, err)

	_, err = decoder.LastPacketDuration()
	assert.ErrorIs(t, err, errNoLastPacketDuration)

	var out [960]float32
	n, err := decoder.DecodeFloat32(firstTinyOggPacket(t), out[:])
	assert.NoError(t, err)
	assert.Equal(t, 960, n)

	duration, err := decoder.LastPacketDuration()
	assert.NoError(t, err)
	assert.Equal(t, 960, duration)
}

func TestDecodeOutputSampleRateAndChannels(t *testing.T) {
	packet := firstTinyOggPacket(t)

	decoder, err := NewDecoder(24000, 1)
	assert.NoError(t, err)
	var monoOut [480]float32
	n, err := decoder.DecodeFloat32(packet, monoOut[:])
	assert.NoError(t, err)
	assert.Equal(t, 480, n)

	decoder, err = NewDecoder(48000, 2)
	assert.NoError(t, err)
	var stereoOut [1920]float32
	n, err = decoder.DecodeFloat32(packet, stereoOut[:])
	assert.NoError(t, err)
	assert.Equal(t, 960, n)
}

func TestDecodePLC(t *testing.T) {
	decoder, err := NewDecoder(48000, 1)
	assert.NoError(t, err)

	err = decoder.DecodePLC(make([]int16, 960))
	assert.ErrorIs(t, err, errNoLastPacketDuration)

	var out [960]int16
	_, err = decoder.Decode(firstTinyOggPacket(t), out[:])
	assert.NoError(t, err)

	for i := range out {
		out[i] = 1
	}
	err = decoder.DecodePLC(out[:])
	assert.NoError(t, err)
	assert.Equal(t, [960]int16{}, out)
}

func TestDecodeFECFallsBackToPLC(t *testing.T) {
	decoder, err := NewDecoder(48000, 1)
	assert.NoError(t, err)

	packet := firstTinyOggPacket(t)
	var out [960]int16
	_, err = decoder.Decode(packet, out[:])
	assert.NoError(t, err)

	for i := range out {
		out[i] = 1
	}
	err = decoder.DecodeFEC(packet, out[:])
	assert.NoError(t, err)
	assert.Equal(t, [960]int16{}, out)
}

func TestPrevSignalType(t *testing.T) {
	decoder, err := NewDecoder(48000, 1)
	assert.NoError(t, err)

	_, err = decoder.PrevSignalType()
	assert.ErrorIs(t, err, errUnsupportedSignalType)
}
