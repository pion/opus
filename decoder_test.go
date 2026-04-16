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

func TestInitResetsCeltState(t *testing.T) {
	decoder := NewDecoder()
	_, stereo, sampleCount, decodedChannelCount, err := decoder.decode(
		[]byte{byte(16<<3) | byte(frameCodeOneFrame), 0xff, 0xff},
		nil,
	)
	assert.NoError(t, err)
	assert.False(t, stereo)
	assert.Positive(t, sampleCount)
	assert.Equal(t, 1, decodedChannelCount)
	assert.NotZero(t, decoder.celtDecoder.FinalRange())

	decoder.celtBuffer = []float32{1}
	decoder.rangeFinal = 42

	err = decoder.Init(48000, 1)

	assert.NoError(t, err)
	assert.Zero(t, decoder.celtDecoder.FinalRange())
	assert.Empty(t, decoder.celtBuffer)
	assert.Zero(t, decoder.rangeFinal)
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
			_, _, _, _, err := decoder.decode([]byte{byte(test.configuration<<3) | byte(frameCodeOneFrame)}, nil)
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

func TestCeltFrameSampleCount(t *testing.T) {
	assert.Equal(t, 120, Configuration(16).celtFrameSampleCount())
	assert.Equal(t, 240, Configuration(17).celtFrameSampleCount())
	assert.Equal(t, 480, Configuration(18).celtFrameSampleCount())
	assert.Equal(t, 960, Configuration(19).celtFrameSampleCount())
	assert.Equal(t, 960, Configuration(31).celtFrameSampleCount())
	assert.Equal(t, 0, Configuration(0).celtFrameSampleCount())
	assert.Equal(t, 0, Configuration(12).celtFrameSampleCount())
}

func TestDecodedSampleRate(t *testing.T) {
	assert.Equal(t, 8000, Configuration(0).decodedSampleRate())
	assert.Equal(t, 16000, Configuration(8).decodedSampleRate())
	assert.Equal(t, celtSampleRate, Configuration(16).decodedSampleRate())
	assert.Equal(t, celtSampleRate, Configuration(31).decodedSampleRate())
	assert.Equal(t, celtSampleRate, Configuration(12).decodedSampleRate())
}

func TestDecodeCeltOnly(t *testing.T) {
	decoder := NewDecoder()

	bandwidth, isStereo, sampleCount, _, err := decoder.decode([]byte{byte(16<<3) | byte(frameCodeOneFrame)}, nil)

	assert.NoError(t, err)
	assert.Equal(t, BandwidthNarrowband, bandwidth)
	assert.False(t, isStereo)
	assert.Equal(t, 120, sampleCount)
	assert.Zero(t, decoder.rangeFinal)
	assert.Equal(t, configurationModeCELTOnly, decoder.previousMode)
}

func TestDecodeHybrid(t *testing.T) {
	decoder := NewDecoder()

	bandwidth, isStereo, sampleCount, _, err := decoder.decode([]byte{byte(12<<3) | byte(frameCodeOneFrame)}, nil)

	assert.NoError(t, err)
	assert.Equal(t, BandwidthSuperwideband, bandwidth)
	assert.False(t, isStereo)
	assert.Equal(t, 480, sampleCount)
	assert.Equal(t, configurationModeHybrid, decoder.previousMode)
}

func TestResetModeStateCopiesSilkResamplerAcrossHybridTransitions(t *testing.T) {
	decoder := NewDecoder()
	assert.NoError(t, decoder.silkResampler[0].Init(BandwidthWideband.SampleRate(), celtSampleRate))
	decoder.silkResamplerBandwidth = BandwidthWideband
	decoder.silkResamplerChannels = 1
	decoder.previousMode = configurationModeSilkOnly

	decoder.resetModeState(configurationModeHybrid)

	assert.Equal(t, 1, decoder.hybridSilkChannels)

	decoder.previousMode = configurationModeHybrid
	decoder.silkResamplerBandwidth = 0
	decoder.silkResamplerChannels = 0

	decoder.resetModeState(configurationModeSilkOnly)

	assert.Equal(t, BandwidthWideband, decoder.silkResamplerBandwidth)
	assert.Equal(t, 1, decoder.silkResamplerChannels)
}

func TestDecodeHybridRedundancyHeader(t *testing.T) {
	// These deterministic payloads drive the RFC 6716 Section 4.5.1 Hybrid
	// redundancy parser through both valid transition directions.
	for _, test := range []struct {
		name       string
		frame      []byte
		celtToSilk bool
		celtData   int
	}{
		{
			name: "silk to celt",
			frame: []byte{
				255, 240, 20, 244, 193, 153, 114, 153, 174, 176, 113, 79, 114, 176, 30, 111,
				78, 251, 135, 241, 38, 152, 99, 238, 115, 216, 157, 159, 172, 149, 251, 21,
			},
			celtToSilk: false,
			celtData:   28,
		},
		{
			name: "celt to silk",
			frame: []byte{
				255, 248, 200, 183, 233, 107, 204, 67, 193, 228, 222, 25, 186, 202, 13, 26,
				79, 90, 131, 149, 102, 178, 120, 213, 146, 125, 92, 227, 83, 96, 134, 146,
			},
			celtToSilk: true,
			celtData:   5,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDecoder()
			decoder.rangeDecoder.Init(test.frame)

			redundancy := decoder.decodeHybridRedundancyHeader(test.frame)

			assert.True(t, redundancy.present)
			assert.Equal(t, test.celtToSilk, redundancy.celtToSilk)
			assert.Equal(t, test.celtData, redundancy.celtDataLen)
			assert.Equal(t, test.frame[test.celtData:], redundancy.data)
		})
	}
}

func TestDecodeSilkOnlyRedundancyHeader(t *testing.T) {
	decoder := NewDecoder()
	frame := make([]byte, 32)
	// Start after the SILK payload so the remaining bytes become redundant CELT
	// data, as described by RFC 6716 Section 4.5.1.2.
	decoder.rangeDecoder.SetInternalValues(frame, 32, 1<<30, 0)

	redundancy, err := decoder.decodeSilkOnlyRedundancyHeader(frame, BandwidthMediumband)

	assert.NoError(t, err)
	assert.True(t, redundancy.present)
	assert.True(t, redundancy.celtToSilk)
	assert.Equal(t, 1, redundancy.celtDataLen)
	assert.Equal(t, frame[1:], redundancy.data)

	_, expectedEndBand, err := decoder.celtDecoder.Mode().BandRangeForSampleRate(BandwidthWideband.SampleRate())
	assert.NoError(t, err)
	assert.Equal(t, expectedEndBand, redundancy.endBand)
}

func TestDecodeHybridRedundantFrame(t *testing.T) {
	decoder := NewDecoder()
	redundancy := hybridRedundancy{data: []byte{0xff, 0xff}}
	endBand, err := decoder.celtEndBandForSilkBandwidth(BandwidthWideband)
	assert.NoError(t, err)

	err = decoder.decodeHybridRedundantFrame(&redundancy, false, 1, endBand)

	assert.NoError(t, err)
	assert.Len(t, redundancy.audio, hybridRedundantFrameSampleCount)
	assert.NotZero(t, redundancy.rng)
}

func TestAddHybridSilkMapsChannels(t *testing.T) {
	for _, test := range []struct {
		name               string
		streamChannelCount int
		outputChannelCount int
		silk48             []float32
		expected           []float32
	}{
		{
			name:               "mono",
			streamChannelCount: 1,
			outputChannelCount: 1,
			silk48:             []float32{0.25},
			expected:           []float32{0.25},
		},
		{
			name:               "mono to stereo",
			streamChannelCount: 1,
			outputChannelCount: 2,
			silk48:             []float32{0.25},
			expected:           []float32{0.25, 0.25},
		},
		{
			name:               "stereo to mono",
			streamChannelCount: 2,
			outputChannelCount: 1,
			silk48:             []float32{0.25, 0.5},
			expected:           []float32{0.375},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDecoder()
			out := make([]float32, len(test.expected))

			decoder.addHybridSilk(out, test.silk48, test.streamChannelCount, test.outputChannelCount, 1)

			assert.Equal(t, test.expected, out)
		})
	}
}

func TestDecodeSilkFramesAddsHybridTransitionAudio(t *testing.T) {
	decoder := NewDecoder()
	decoder.previousMode = configurationModeHybrid

	bandwidth, isStereo, sampleCount, decodedChannelCount, err := decoder.decodeSilkFrames(
		Configuration(8),
		tableOfContentsHeader(byte(8<<3)|byte(frameCodeOneFrame)),
		[][]byte{nil},
		nil,
	)

	assert.NoError(t, err)
	assert.Equal(t, BandwidthWideband, bandwidth)
	assert.False(t, isStereo)
	assert.Equal(t, 160, sampleCount)
	assert.Equal(t, 1, decodedChannelCount)
	assert.Len(t, decoder.silkCeltAdditions, 1)
	assert.Len(t, decoder.silkCeltAdditions[0].audio, hybridFadeSampleCount)
}

func TestApplySilkRedundancyFades(t *testing.T) {
	decoder := NewDecoder()
	decoder.resampleBuffer = make([]float32, 600)
	for i := range decoder.resampleBuffer {
		decoder.resampleBuffer[i] = 0.25
	}

	leadingAudio := make([]float32, 2*hybridFadeSampleCount)
	trailingAudio := make([]float32, 2*hybridFadeSampleCount)
	for i := range leadingAudio {
		leadingAudio[i] = 0.5
		trailingAudio[i] = 0.75
	}
	decoder.silkCeltAdditions = append(decoder.silkCeltAdditions, silkCeltAddition{
		audio:        []float32{0.125},
		startSample:  0,
		channelCount: 1,
	})
	decoder.silkRedundancyFades = append(
		decoder.silkRedundancyFades,
		silkRedundancyFade{
			celtToSilk:       true,
			audio:            leadingAudio,
			startSample:      1,
			frameSampleCount: 2 * hybridFadeSampleCount,
			channelCount:     1,
		},
		silkRedundancyFade{
			audio:            trailingAudio,
			startSample:      2 * hybridFadeSampleCount,
			frameSampleCount: 2 * hybridFadeSampleCount,
			channelCount:     1,
		},
	)

	decoder.applySilkRedundancyFades(1)

	assert.Equal(t, float32(0.375), decoder.resampleBuffer[0])
	assert.Equal(t, float32(0.5), decoder.resampleBuffer[1])
	assert.NotEqual(t, float32(0.25), decoder.resampleBuffer[1+hybridFadeSampleCount])
	assert.NotEqual(t, float32(0.25), decoder.resampleBuffer[3*hybridFadeSampleCount+60])
	assert.Empty(t, decoder.silkCeltAdditions)
	assert.Empty(t, decoder.silkRedundancyFades)
}
