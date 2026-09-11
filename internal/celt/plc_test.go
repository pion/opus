// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gosec,varnamelen,tagliatelle // Reference fixtures preserve C widths, names, and stage order.
package celt

import (
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPLCReferenceState(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Mode, Rate, Channels int
			OutputChannels       int `json:"output_channels"`
			Steps                []struct {
				Packet  string
				Samples int
			}
		}
	}
	open := func(name string) *json.Decoder {
		f, err := os.Open("../../testdata/short-plc/" + name) //nolint:gosec // Constant test fixture names only.
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, f.Close()) })
		z, err := gzip.NewReader(f)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, z.Close()) })

		return json.NewDecoder(z)
	}
	require.NoError(t, open("corpus.json.gz").Decode(&fixture))
	require.Len(t, fixture.Cases, 1300)
	states := open("corpus-state.jsonl.gz")
	recordCount, lossCount := 0, 0
	for ci, c := range fixture.Cases {
		d := NewDecoder()
		end := maxBands
		for si, step := range c.Steps {
			var ref struct {
				Type, Loss, Skip, Pitch int
				Energy                  []float32
			}
			require.NoError(t, states.Decode(&ref))
			recordCount++
			if step.Packet == "" {
				lossCount++
			}
			if c.Mode != 0 {
				continue
			}
			var packet []byte
			stereo := c.Channels == 2
			if step.Packet != "" {
				p, err := hex.DecodeString(step.Packet)
				require.NoError(t, err)
				require.Zero(t, p[0]&3)
				end = [4]int{13, 17, 19, 21}[(p[0]>>3-16)>>2]
				stereo = p[0]&4 != 0
				packet = p[1:]
			}
			out := make([]float32, step.Samples*c.OutputChannels)
			require.NoError(t, d.DecodeToSampleRate(packet, out, stereo,
				c.OutputChannels, step.Samples*48000/c.Rate, 0, end, c.Rate))
			label := fmt.Sprintf("case %d step %d", ci, si)
			require.Equal(t, ref.Loss, d.lossDuration, label)
			require.Equal(t, ref.Skip != 0, d.plc.skip, label)
			require.Equal(t, ref.Type == 3, d.plc.periodic, label)
			require.Equal(t, ref.Pitch, d.plc.pitch, label)
			for h, history := range [4][2][maxBands]float32{d.previousLogE, d.previousLogE1, d.previousLogE2, d.plc.background} {
				for ch := range 2 {
					for band := range maxBands {
						require.InDelta(t, ref.Energy[h*2*maxBands+ch*maxBands+band], history[ch][band], 0.001, label)
					}
				}
			}
			for _, v := range out {
				require.False(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0), label)
			}
		}
	}
	require.Equal(t, 28_600, recordCount)
	require.Equal(t, 10_620, lossCount)
	var extra json.RawMessage
	require.ErrorIs(t, states.Decode(&extra), io.EOF)
}

func TestCELTPeriodicPLCStagesReference(t *testing.T) {
	var corpus struct {
		Cases []struct {
			Rate, Channels int
			OutputChannels int `json:"output_channels"`
			Steps          []struct {
				Packet  string
				Samples int
			}
		}
	}
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))

	data, err := os.ReadFile("../../testdata/short-plc/celt-stage.json")
	require.NoError(t, err)
	var reference []struct {
		Step, Pitch        int
		LPC, AC, Corrected []uint32
		RecomputedLPC      []uint32 `json:"recomputed_lpc"`
		History            []uint32
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Len(t, reference, 10)
	data, err = os.ReadFile("../../testdata/short-plc/celt-noise-stage.json")
	require.NoError(t, err)
	var noiseReference struct {
		Normalized, Frequency, Synthesis, Postfilter []uint32
	}
	require.NoError(t, json.Unmarshal(data, &noiseReference))

	scenario := corpus.Cases[0]
	decoder := NewDecoder()
	end := maxBands
	for step, frame := range scenario.Steps[:10] {
		var packet []byte
		if frame.Packet != "" {
			encoded, decodeErr := hex.DecodeString(frame.Packet)
			require.NoError(t, decodeErr)
			end = [4]int{13, 17, 19, 21}[(encoded[0]>>3-16)>>2]
			packet = encoded[1:]
		}
		output := make([]float32, frame.Samples*scenario.OutputChannels)
		if step == 9 {
			probe := decoder
			probe.scratch = &decoderScratch{}
			for channel := range probe.overlap {
				probe.overlap[channel] = append([]float32(nil), decoder.overlap[channel]...)
				probe.postfilterMem[channel] = append([]float32(nil), decoder.postfilterMem[channel]...)
			}
			info := frameSideInfo{
				lm: 3, startBand: 0, endBand: maxBands, channelCount: 1,
				outputChannelCount: 1, outputSampleRate: scenario.Rate,
			}
			for band := info.startBand; band < info.endBand; band++ {
				probe.previousLogE[0][band] = max(probe.plc.background[0][band], probe.previousLogE[0][band]-0.5)
			}
			normalized := probe.scratch.x[:maxFrameSampleCount]
			seed := probe.rng
			for band := info.startBand; band < info.endBand; band++ {
				start := int(bandEdges[band]) << info.lm
				stop := int(bandEdges[band+1]) << info.lm
				for i := start; i < stop; i++ {
					seed = lcgRand(seed)
					normalized[i] = float32(int32(seed) >> 20)
				}
				decoderRenormaliseVector(normalized[start:stop], stop-start, normScaling)
			}
			frequency := probe.scratch.channels[0].freq[:maxFrameSampleCount]
			denormaliseBands(&info, normalized, frequency, probe.log2Amp(&info)[0])
			limitOutputBandwidth(&info, frequency)
			synthesis := probe.inverseTransformChannel(frequency, 0, &info)
			for _, stage := range []struct {
				name     string
				actual   []float32
				expected []uint32
			}{
				{"normalized", normalized[:len(noiseReference.Normalized)], noiseReference.Normalized},
				{"frequency", frequency[:len(noiseReference.Frequency)], noiseReference.Frequency},
				{"synthesis", synthesis[:len(noiseReference.Synthesis)], noiseReference.Synthesis},
			} {
				for i, value := range stage.actual {
					require.Equal(t, stage.expected[i], math.Float32bits(value), "%s %d", stage.name, i)
				}
			}
		}
		require.NoError(t, decoder.DecodeToSampleRate(packet, output, false,
			scenario.OutputChannels, frame.Samples*48000/scenario.Rate, 0, end, scenario.Rate))
		want := reference[step]
		require.Equal(t, step, want.Step)
		require.Equal(t, want.Pitch, decoder.plc.pitch)
		require.Len(t, want.LPC, len(decoder.plc.lpc[0]))
		for i, value := range decoder.plc.lpc[0] {
			require.Equal(t, want.LPC[i], math.Float32bits(value), "step %d LPC %d", step, i)
		}
		if step == 3 {
			scratch := &decoder.scratchBuffer().plc
			copy(scratch.windowed[:], decoder.plc.history[0][plcHistorySize-combFilterMaxPeriod:plcHistorySize])
			for i := range shortBlockSampleCount {
				scratch.windowed[i] *= celtWindow120[i]
				scratch.windowed[combFilterMaxPeriod-1-i] *= celtWindow120[i]
			}
			ac := celtPLCAutocorr(scratch.windowed[:], plcLPCOrder, scratch.ac[:])
			for i, value := range ac {
				require.Equal(t, want.AC[i], math.Float32bits(value), "autocorrelation %d", i)
			}
			ac[0] *= 1.0001
			lagWindow := float32(0.008)
			lagWindow *= lagWindow
			for i := 1; i <= plcLPCOrder; i++ {
				correction := decoderRoundedProduct(ac[i], lagWindow)
				correction = decoderRoundedProduct(correction, float32(i))
				correction = decoderRoundedProduct(correction, float32(i))
				ac[i] -= correction
			}
			for i, value := range ac {
				require.Equal(t, want.Corrected[i], math.Float32bits(value), "corrected autocorrelation %d", i)
			}
			var lpc [plcLPCOrder]float32
			coefficients := decoderCELTLPC(ac, plcLPCOrder, lpc[:])
			for i, value := range coefficients {
				require.Equal(t, want.RecomputedLPC[i], math.Float32bits(value), "recomputed LPC %d", i)
			}
		}
		historyLength := plcHistorySize
		if step >= 4 {
			historyLength = len(decoder.plc.history[0])
		}
		require.GreaterOrEqual(t, len(want.History), historyLength)
		if step < 9 {
			for i, value := range decoder.plc.history[0][:historyLength] {
				require.Equal(t, want.History[i], math.Float32bits(value), "step %d history %d", step, i)
			}
		}
	}
	for i, value := range decoder.scratchBuffer().channels[0].time[:len(noiseReference.Postfilter)] {
		require.Equal(t, noiseReference.Postfilter[i], math.Float32bits(value), "postfilter %d", i)
	}
}

func TestCELTMixedRecoveryStagesReference(t *testing.T) {
	var corpus struct {
		Cases []struct {
			Rate, Channels int
			OutputChannels int `json:"output_channels"`
			Steps          []struct {
				Packet  string
				Samples int
			}
		}
	}
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	data, err := os.ReadFile("../../testdata/short-plc/celt-mixed-stage.json")
	require.NoError(t, err)
	var reference []struct {
		Step, Pitch  int
		LPC, History []uint32
	}
	require.NoError(t, json.Unmarshal(data, &reference))
	require.Len(t, reference, 5)
	data, err = os.ReadFile("../../testdata/short-plc/celt-plc-math.json")
	require.NoError(t, err)
	var mathReference struct {
		FIRInput     []uint32 `json:"fir_input"`
		Coefficients []uint32 `json:"coefficients"`
		FIROutput    []uint32 `json:"fir_output"`
		IIRInput     []uint32 `json:"iir_input"`
		IIRMemory    []uint32 `json:"iir_memory"`
		IIROutput    []uint32 `json:"iir_output"`
	}
	require.NoError(t, json.Unmarshal(data, &mathReference))

	scenario := corpus.Cases[3]
	decoder := NewDecoder()
	end := maxBands
	lastFrameSampleCount := maxFrameSampleCount
	for step, frame := range scenario.Steps[:16] {
		var packet []byte
		if frame.Packet != "" {
			encoded, decodeErr := hex.DecodeString(frame.Packet)
			require.NoError(t, decodeErr)
			end = [4]int{13, 17, 19, 21}[(encoded[0]>>3-16)>>2]
			packet = encoded[1:]
			lastFrameSampleCount = frame.Samples * sampleRate / scenario.Rate
		}
		output := make([]float32, frame.Samples*scenario.OutputChannels)
		if step == 15 {
			var scratch plcScratch
			channels := [][]float32{decoder.plc.history[0][:plcHistorySize]}
			celtPLCPitchDownsample(channels, scratch.low[:], plcHistorySize/2, 2, &scratch.pitch)
			pitch := plcPitchMax - celtPLCPitchSearch(scratch.low[plcPitchMax/2:], scratch.low[:],
				plcHistorySize-plcPitchMax, plcPitchMax-plcPitchMin, &scratch.pitch)
			length := min(2*pitch, combFilterMaxPeriod)
			excitation := scratch.excitation[:]
			copy(excitation, decoder.plc.history[0][plcHistorySize-combFilterMaxPeriod-plcLPCOrder:plcHistorySize])
			copy(scratch.windowed[:], excitation[plcLPCOrder:])
			for i := range shortBlockSampleCount {
				scratch.windowed[i] *= celtWindow120[i]
				scratch.windowed[combFilterMaxPeriod-1-i] *= celtWindow120[i]
			}
			ac := celtPLCAutocorr(scratch.windowed[:], plcLPCOrder, scratch.ac[:])
			ac[0] *= 1.0001
			lagWindow := float32(0.008)
			lagWindow *= lagWindow
			for i := 1; i <= plcLPCOrder; i++ {
				correction := decoderRoundedProduct(ac[i], lagWindow)
				correction = decoderRoundedProduct(correction, float32(i))
				correction = decoderRoundedProduct(correction, float32(i))
				ac[i] -= correction
			}
			var coefficientsMemory [plcLPCOrder]float32
			coefficients := decoderCELTLPC(ac, plcLPCOrder, coefficientsMemory[:])
			firStart := plcLPCOrder + combFilterMaxPeriod - length
			celtPLCFIR(excitation, firStart, coefficients, scratch.fir[:length])
			for _, stage := range []struct {
				name     string
				actual   []float32
				expected []uint32
			}{
				{"FIR input", excitation[firStart-plcLPCOrder : firStart+length], mathReference.FIRInput},
				{"coefficients", coefficients, mathReference.Coefficients},
				{"FIR output", scratch.fir[:length], mathReference.FIROutput},
			} {
				for i, value := range stage.actual {
					require.Equal(t, stage.expected[i], math.Float32bits(value), "%s %d", stage.name, i)
				}
			}
			copy(excitation[firStart:], scratch.fir[:length])
			e1, e2 := float32(1), float32(1)
			for i := 0; i < length/2; i++ {
				x := excitation[plcLPCOrder+combFilterMaxPeriod-length/2+i]
				y := excitation[plcLPCOrder+combFilterMaxPeriod-length+i]
				e1 += x * x
				e2 += y * y
			}
			decay := float32(math.Sqrt(float64(min(e1, e2) / e2)))
			memory := decoder.plc.history[0]
			n := lastFrameSampleCount
			copy(memory[:], memory[n:plcHistorySize])
			base := plcHistorySize - n
			attenuation := decay
			for i, j := 0, 0; i < n+shortBlockSampleCount; i, j = i+1, j+1 {
				if j >= pitch {
					j -= pitch
					attenuation *= decay
				}
				memory[base+i] = attenuation * excitation[plcLPCOrder+combFilterMaxPeriod-pitch+j]
			}
			iirMemory := make([]float32, plcLPCOrder)
			for i := range iirMemory {
				iirMemory[i] = memory[base-1-i]
			}
			for _, stage := range []struct {
				name     string
				actual   []float32
				expected []uint32
			}{
				{"IIR input", memory[base : base+n+shortBlockSampleCount], mathReference.IIRInput},
				{"IIR memory", iirMemory, mathReference.IIRMemory},
			} {
				for i, value := range stage.actual {
					require.Equal(t, stage.expected[i], math.Float32bits(value), "%s %d", stage.name, i)
				}
			}
			celtPLCIIR(memory[base:base+n+shortBlockSampleCount], memory[:base], coefficients, scratch.iir[:])
			for i, value := range memory[base : base+n+shortBlockSampleCount] {
				require.Equal(t, mathReference.IIROutput[i], math.Float32bits(value), "IIR output %d", i)
			}
		}
		frameSampleCount := frame.Samples * sampleRate / scenario.Rate
		if packet != nil {
			require.NoError(t, decoder.DecodeToSampleRate(packet, output, false,
				scenario.OutputChannels, frameSampleCount, 0, end, scenario.Rate))
		} else {
			for offset := 0; offset < frameSampleCount; offset += lastFrameSampleCount {
				chunk := min(lastFrameSampleCount, frameSampleCount-offset)
				outputStart := offset * scenario.Rate / sampleRate * scenario.OutputChannels
				outputEnd := (offset + chunk) * scenario.Rate / sampleRate * scenario.OutputChannels
				require.NoError(t, decoder.DecodeToSampleRate(nil, output[outputStart:outputEnd], false,
					scenario.OutputChannels, chunk, 0, end, scenario.Rate))
			}
		}
		if step < 11 {
			continue
		}
		want := reference[step-11]
		require.Equal(t, step, want.Step)
		require.Equal(t, want.Pitch, decoder.plc.pitch, "step %d pitch", step)
		for i, value := range decoder.plc.lpc[0] {
			require.Equal(t, want.LPC[i], math.Float32bits(value), "step %d LPC %d", step, i)
		}
		historyLength := plcHistorySize
		if frame.Packet == "" {
			historyLength = len(decoder.plc.history[0])
		}
		for i, value := range decoder.plc.history[0][:historyLength] {
			require.Equal(t, want.History[i], math.Float32bits(value), "step %d history %d", step, i)
		}
	}
}

func TestPLCNoiseDoesNotMirrorEnergy(t *testing.T) {
	d := NewDecoder()
	d.previousLogE[0][0], d.previousLogE[1][0] = 4, 3
	require.NoError(t, d.Decode(nil, make([]float32, 120), true, 1, 120, 0, maxBands))
	require.Equal(t, float32(2.5), d.previousLogE[0][0])
	require.Equal(t, float32(3), d.previousLogE[1][0])
}

func FuzzPLCSequence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 4, 4, 4, 4, 4, 0, 4, 0, 0, 4})
	f.Fuzz(func(t *testing.T, operations []byte) {
		d := NewDecoder()
		var out [1920]float32
		for _, op := range operations[:min(len(operations), 100)] {
			if op == 255 {
				d.Reset()

				continue
			}
			size := shortBlockSampleCount << int(op&3)
			var packet []byte
			if op&4 == 0 {
				packet = []byte{0, 0, 0, 0, 0, 0, 0, 0}
			}
			require.NoError(t, d.Decode(packet, out[:size*2], true, 2, size, 0, maxBands))
			for _, v := range out[:size*2] {
				require.False(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0))
			}
		}
	})
}
