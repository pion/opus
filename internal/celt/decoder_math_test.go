// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:dupl,tagliatelle,varnamelen // Repeated stage tables intentionally preserve fixture terminology.
package celt

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecoderFirstSpectrumReference(t *testing.T) {
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus struct {
		Cases []struct{ Steps []struct{ Packet string } }
	}
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	packet, err := hex.DecodeString(corpus.Cases[0].Steps[0].Packet)
	require.NoError(t, err)
	data, err := os.ReadFile("../../testdata/short-plc/first-spectrum.json")
	require.NoError(t, err)
	var reference struct{ Normalized, Frequency, Energy, Synthesis, Postfilter []uint32 }
	require.NoError(t, json.Unmarshal(data, &reference))
	d := NewDecoder()
	require.NoError(t, d.DecodeToSampleRate(packet[1:], make([]float32, 160), false, 1, 960, 0, 21, 8000))
	referenceFrequency := make([]float32, len(reference.Frequency))
	for i, bits := range reference.Frequency {
		referenceFrequency[i] = math.Float32frombits(bits)
	}
	synthesisDecoder := NewDecoder()
	referenceSynthesis := synthesisDecoder.inverseTransformChannel(referenceFrequency, 0, &frameSideInfo{
		lm: 3, transient: true,
	})
	for _, stage := range []struct {
		name     string
		actual   []float32
		expected []uint32
	}{
		{"energy", d.previousLogE[0][:], reference.Energy},
		{"normalized", d.scratchBuffer().x[:800], reference.Normalized},
		{"frequency", d.scratchBuffer().channels[0].freq[:960], reference.Frequency},
		{"synthesis", referenceSynthesis, reference.Synthesis},
		{"postfilter", d.scratchBuffer().channels[0].time[:960], reference.Postfilter},
	} {
		t.Run(stage.name, func(t *testing.T) {
			require.Len(t, stage.expected, len(stage.actual))
			for i, sample := range stage.actual {
				require.Equal(t, stage.expected[i], math.Float32bits(sample), "index %d got %.9g want %.9g", i,
					sample, math.Float32frombits(stage.expected[i]))
			}
		})
	}
}

func TestDecoderStereoDownmixSpectrumReference(t *testing.T) {
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus struct {
		Cases []struct{ Steps []struct{ Packet string } }
	}
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	packet, err := hex.DecodeString(corpus.Cases[52].Steps[0].Packet)
	require.NoError(t, err)
	data, err := os.ReadFile("../../testdata/short-plc/downmix-spectrum.json")
	require.NoError(t, err)
	var reference struct {
		Normalized    []uint32
		Energy        []uint32
		MDCTFrequency []uint32 `json:"mdct_frequency"`
		Synthesis     []uint32
		Postfilter    []uint32
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	d := NewDecoder()
	require.NoError(t, d.DecodeToSampleRate(packet[1:], make([]float32, 480), true, 1, 960, 0, 21, 24000))
	referenceFrequency := make([]float32, len(reference.MDCTFrequency))
	for i, bits := range reference.MDCTFrequency {
		referenceFrequency[i] = math.Float32frombits(bits)
	}
	synthesisDecoder := NewDecoder()
	referenceSynthesis := synthesisDecoder.inverseTransformChannel(referenceFrequency, 0, &frameSideInfo{
		lm: 3, transient: true,
	})
	for _, stage := range []struct {
		name     string
		actual   []float32
		expected []uint32
	}{
		{"energy", d.previousLogE[0][:], reference.Energy},
		{"normalized", d.scratchBuffer().x[:800], reference.Normalized},
		{"mdct-frequency", d.scratchBuffer().channels[0].freq[:960], reference.MDCTFrequency},
		{"synthesis", referenceSynthesis, reference.Synthesis},
		{"postfilter", d.scratchBuffer().channels[0].time[:960], reference.Postfilter},
	} {
		t.Run(stage.name, func(t *testing.T) {
			require.Len(t, stage.expected, len(stage.actual))
			for i, sample := range stage.actual {
				require.Equal(t, stage.expected[i], math.Float32bits(sample), "index %d got %.9g want %.9g", i,
					sample, math.Float32frombits(stage.expected[i]))
			}
		})
	}
}

func TestDecoderRandomStereoHistoryReference(t *testing.T) {
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus struct {
		Cases []struct{ Steps []struct{ Packet string } }
	}
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	d := NewDecoder()
	for step := 0; step <= 18; step++ {
		packet, decodeErr := hex.DecodeString(corpus.Cases[298].Steps[step].Packet)
		require.NoError(t, decodeErr)
		require.NoError(t, d.DecodeToSampleRate(packet[1:], make([]float32, 960), true, 1, 960, 0, 21, 48000))
	}
	data, err := os.ReadFile("../../testdata/short-plc/random-stereo-spectrum.json")
	require.NoError(t, err)
	var reference struct {
		Normalized    []uint32
		Energy        []uint32
		MDCTFrequency []uint32 `json:"mdct_frequency"`
		Postfilter    []uint32
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	for _, stage := range []struct {
		name     string
		actual   []float32
		expected []uint32
	}{
		{"energy", d.previousLogE[0][:], reference.Energy},
		{"normalized", d.scratchBuffer().x[:800], reference.Normalized},
		{"mdct-frequency", d.scratchBuffer().channels[0].freq[:960], reference.MDCTFrequency},
		{"postfilter", d.scratchBuffer().channels[0].time[:960], reference.Postfilter},
	} {
		t.Run(stage.name, func(t *testing.T) {
			require.Len(t, stage.expected, len(stage.actual))
			for i, sample := range stage.actual {
				require.Equal(t, stage.expected[i], math.Float32bits(sample), "index %d got %.9g want %.9g", i,
					sample, math.Float32frombits(stage.expected[i]))
			}
		})
	}
}

func TestDecoderBandAmplitudeReference(t *testing.T) {
	data, err := os.ReadFile("../../testdata/short-plc/exp2.json")
	require.NoError(t, err)
	var bits []uint32
	require.NoError(t, json.Unmarshal(data, &bits))
	require.Len(t, bits, 1329)
	d := NewDecoder()
	info := frameSideInfo{channelCount: 1, startBand: 0, endBand: 1}
	for index, expected := range bits {
		x := float32(index-816) / 16
		d.previousLogE[0][0] = x - energyMeans[0]
		actual := d.log2Amp(&info)[0][0]
		require.Equal(t, expected, math.Float32bits(actual), "log amplitude %g", x)
	}
}

func TestDecoderTransformTablesReference(t *testing.T) {
	data, err := os.ReadFile("../../testdata/short-plc/mdct-tables.json")
	require.NoError(t, err)
	var fixture struct {
		Pin   string
		Plans []struct {
			Frame    int
			Trig     []uint32
			TwiddleR []uint32 `json:"twiddle_r"`
			TwiddleI []uint32 `json:"twiddle_i"`
			Bitrev   []int
			Factors  []int
		}
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.Equal(t, "22244de5a79bd1d6d623c32e72bf1954b56235be", fixture.Pin)
	for _, reference := range fixture.Plans {
		t.Run(fmt.Sprint(reference.Frame), func(t *testing.T) {
			plan := decoderTransformPlanForFrameSampleCount(reference.Frame)
			for i, value := range plan.trig {
				require.Equal(t, reference.Trig[i], math.Float32bits(value), "trig %d", i)
			}
			for i, value := range plan.twiddles {
				require.Equal(t, reference.TwiddleR[i], math.Float32bits(value.r), "twiddle real %d", i)
				require.Equal(t, reference.TwiddleI[i], math.Float32bits(value.i), "twiddle imaginary %d", i)
			}
			require.Equal(t, reference.Bitrev, plan.bitrev)
			actualFactors := make([]int, 0, 2*len(plan.factors))
			for _, factor := range plan.factors {
				actualFactors = append(actualFactors, factor.radix, factor.size)
			}
			require.Equal(t, reference.Factors, actualFactors)
		})
	}
}
