// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	duration20Ms = "20ms"
	duration40Ms = "40ms"
	duration60Ms = "60ms"

	// Guards for TestEncodeSILKTracksTargetRate. The ratio and the SNR floor
	// are deliberately loose: they are there to catch a quantizer that has
	// stopped predicting, not to pin encoder quality, which
	// TestEncoderQuality tracks against a baseline.
	silkRateTestBitrate     = 24000
	silkRateTestComplexity  = 5
	silkRateTestSeconds     = 1
	silkRateTestMaxOverrun  = 2.0  // emitted rate, as a multiple of the target
	silkRateTestPeakCeiling = 0.99 // decoded peak, 1.0 being full scale
	silkRateTestMinSNRDB    = 6.0
)

// TestDecodeLibopus161FirstFrameVector pins the public packet boundary against
// libopus 1.6.1 at exact source commit
// 22244de5a79bd1d6d623c32e72bf1954b56235be. The input is one 20 ms, 16 kHz,
// mono periodic frame encoded in SILK-only wideband mode.
func TestDecodeLibopus161FirstFrameVector(t *testing.T) {
	packet, err := hex.DecodeString(
		"4881A7B7022A8729FA0637420C88CB4371F6B65F9E802E8FD52CF5FAD21127A8103DC8044DD3CF02C9" +
			"B24EBD74E782311962A0696744E9332E2BE8EE390D92F050558E46807036514940E156E1AA74A7E5DF83EC349870EC" +
			"9D87638B19D111841A02973DC0",
	)
	require.NoError(t, err)
	require.Equal(t, byte(0x48), packet[0], "20 ms mono wideband SILK TOC")

	dec, err := NewDecoderWithOutput(16000, 1)
	require.NoError(t, err)
	out := make([]float32, 320)
	got, err := dec.DecodeToFloat32(packet, out)
	require.NoError(t, err)
	require.Equal(t, 320, got)
	assert.Equal(t, uint32(0x49cf8000), dec.rangeFinal)
}

// TestEncodeSILKRoundTrip encodes a SILK frame with the public encoder and
// decodes the resulting Opus packet with the public decoder, for every
// SILK-supported bandwidth, checking the packet is well-formed and decodes to
// non-silent audio.
func TestEncodeSILKRoundTrip(t *testing.T) {
	for _, bandwidth := range []Bandwidth{BandwidthNarrowband, BandwidthMediumband, BandwidthWideband} {
		enc, err := NewEncoder()
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)

		sampleCount := bandwidth.SampleRate() / 50 // 20 ms
		pcm := make([]int16, sampleCount)
		for i := range pcm {
			pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
		}

		packet := make([]byte, maxOpusFrameSize)
		n, err := enc.EncodeSILK(pcm, bandwidth, packet)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)
		require.Greaterf(t, n, 1, "bandwidth %d", bandwidth)
		packet = packet[:n]

		dec, err := NewDecoderWithOutput(bandwidth.SampleRate(), 1)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)

		out := make([]float32, sampleCount)
		got, err := dec.DecodeToFloat32(packet, out)
		require.NoErrorf(t, err, "bandwidth %d", bandwidth)
		require.Positivef(t, got, "bandwidth %d", bandwidth)

		var energy float64
		for _, v := range out[:got] {
			energy += float64(v) * float64(v)
		}
		assert.Positivef(t, energy, "bandwidth %d: decoded output is silent", bandwidth)
	}
}

// TestEncodeSILKMultiUnitRoundTrip checks the TOC, frame code, decoded sample
// count, and energy for 20, 40, and 60 ms at every SILK bandwidth. The 60 ms
// case also covers a representative 24 kb/s, complexity-5 VoIP configuration.
func TestEncodeSILKMultiUnitRoundTrip(t *testing.T) {
	durations := []struct {
		name     string
		units    int // 20 ms coding units
		wbConfig int // expected TOC config (RFC 6716 Table 2)
		nbConfig int
		mbConfig int
	}{
		{duration20Ms, 1, silkOnlyWideband20msConfig, silkOnlyNarrowband20msConfig, silkOnlyMediumband20msConfig},
		{duration40Ms, 2, silkOnlyWideband40msConfig, silkOnlyNarrowband40msConfig, silkOnlyMediumband40msConfig},
		{duration60Ms, 3, silkOnlyWideband60msConfig, silkOnlyNarrowband60msConfig, silkOnlyMediumband60msConfig},
	}
	for _, duration := range durations {
		t.Run(duration.name, func(t *testing.T) {
			t.Run("default", func(t *testing.T) {
				runMultiUnitRoundTrip(t, duration.name, duration.units, duration.wbConfig, duration.nbConfig, duration.mbConfig)
			})
			if duration.units == 3 {
				t.Run("wideband60", func(t *testing.T) {
					runMultiUnitRoundTripWideband60(t, duration.name)
				})
			}
		})
	}
}

// runMultiUnitRoundTrip encodes one frame of the given duration at each
// SILK-supported bandwidth with a default encoder, decodes it back, and
// checks the TOC config, frame code, sample count, and non-silent energy.
func runMultiUnitRoundTrip(t *testing.T, name string, units int, wbConfig, nbConfig, mbConfig int) {
	t.Helper()
	configs := []int{wbConfig, nbConfig, mbConfig}
	for bandwidthIndex, bandwidth := range []Bandwidth{BandwidthWideband, BandwidthNarrowband, BandwidthMediumband} {
		wantConfig := configs[bandwidthIndex]
		enc, err := NewEncoder()
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)

		sampleCount := bandwidth.SampleRate() / 50 * units
		pcm := make([]int16, sampleCount)
		for i := range pcm {
			pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
		}

		packet := make([]byte, maxOpusFrameSize)
		n, err := enc.EncodeSILK(pcm, bandwidth, packet)
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)
		require.Greaterf(t, n, 1, "%s bw %d", name, bandwidth)
		packet = packet[:n]

		// TOC: mono (s=0), one frame in the packet (c=0), and the
		// SILK-only config for this bandwidth and duration.
		toc := int(packet[0])
		assert.Equalf(t, wantConfig<<3, toc,
			"%s bw %d: TOC %#x, want config %d (0x%02x)", name, bandwidth, toc, wantConfig, wantConfig<<3)
		assert.Equalf(t, 0, toc&1, "%s bw %d: frame code must be 0 (single frame)", name, bandwidth)

		dec, err := NewDecoderWithOutput(bandwidth.SampleRate(), 1)
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)

		out := make([]float32, sampleCount)
		got, err := dec.DecodeToFloat32(packet, out)
		require.NoErrorf(t, err, "%s bw %d", name, bandwidth)
		assert.Equalf(t, sampleCount, got, "%s bw %d: decoded %d samples, want %d", name, bandwidth, got, sampleCount)

		var energy float64
		for _, v := range out[:got] {
			energy += float64(v) * float64(v)
		}
		assert.Positivef(t, energy, "%s bw %d: decoded output is silent", name, bandwidth)
	}
}

// runMultiUnitRoundTripWideband60 runs the 60 ms wideband round trip with a
// representative VoIP configuration: 24 kb/s at complexity 5.
func runMultiUnitRoundTripWideband60(t *testing.T, name string) {
	t.Helper()
	enc, err := NewEncoder(
		WithApplication(ApplicationVoIP),
		WithBitrate(24000),
		WithComplexity(5),
	)
	require.NoErrorf(t, err, "%s", name)

	unit := BandwidthWideband.SampleRate() / 50 // 320 samples per 20 ms unit
	pcm := make([]int16, 3*unit)                // 960 samples, one 60 ms frame
	for i := range pcm {
		pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
	}

	packet := make([]byte, maxOpusFrameSize)
	n, err := enc.EncodeSILK(pcm, BandwidthWideband, packet)
	require.NoErrorf(t, err, "%s", name)
	require.Greaterf(t, n, 1, "%s", name)
	packet = packet[:n]

	// TOC: config 11 (SILK-only, WB, 60 ms, RFC 6716 Table 2), s=0 (mono),
	// frame code 0 (one frame in the packet).
	assert.Equalf(t, silkOnlyWideband60msConfig<<3, int(packet[0]),
		"%s: TOC %#x, want 0x%02x (SILK-only WB 60 ms, mono, frame code 0)",
		name, int(packet[0]), silkOnlyWideband60msConfig<<3)

	dec, err := NewDecoderWithOutput(BandwidthWideband.SampleRate(), 1)
	require.NoErrorf(t, err, "%s", name)

	out := make([]float32, 3*unit)
	got, err := dec.DecodeToFloat32(packet, out)
	require.NoErrorf(t, err, "%s", name)
	assert.Equalf(t, 3*unit, got, "%s: decoded %d samples, want %d (960)", name, got, 3*unit)

	var energy float64
	for _, v := range out[:got] {
		energy += float64(v) * float64(v)
	}
	assert.Positivef(t, energy, "%s: decoded output is silent", name)
}

// TestEncodeSILKStateContinuity feeds the same encoder consecutive 20, 40,
// and 60 ms frames and decodes them with one stateful decoder. Prediction
// state (NLSF interpolation, pitch lag, LCG seed) must stay consistent
// across units of a 60 ms packet and across packets; a mismatch between the
// encoder's per-unit emission and the decoder's per-unit consumption would
// desynchronize the range stream and fail closed here.
func TestEncodeSILKStateContinuity(t *testing.T) {
	enc, err := NewEncoder(WithComplexity(8))
	require.NoError(t, err)

	dec, err := NewDecoderWithOutput(BandwidthWideband.SampleRate(), 1)
	require.NoError(t, err)

	unitSamples := BandwidthWideband.SampleRate() / 50 // 320
	for _, units := range []int{1, 1, 2, 3, 3, 1, 2, 3} {
		pcm := make([]int16, unitSamples*units)
		for i := range pcm {
			pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
		}
		packet := make([]byte, maxOpusFrameSize)
		n, err := enc.EncodeSILK(pcm, BandwidthWideband, packet)
		require.NoError(t, err)

		out := make([]float32, len(pcm))
		got, err := dec.DecodeToFloat32(packet[:n], out)
		require.NoError(t, err)
		assert.Equal(t, len(pcm), got)

		var energy float64
		for _, v := range out[:got] {
			energy += float64(v) * float64(v)
		}
		assert.Positive(t, energy)
	}
}

// TestEncodeSILKInvalidSizes rejects lengths that are not a whole number of
// 20 ms coding units at the bandwidth's internal rate, plus the case of more
// than three units (120 ms), which the SILK packet duration cap forbids.
func TestEncodeSILKInvalidSizes(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	unit := BandwidthWideband.SampleRate() / 50 // 320
	for _, samples := range []int{0, unit - 1, unit + 1, 2*unit - 1, 3*unit + 1, 4 * unit} {
		_, err := enc.EncodeSILK(make([]int16, samples), BandwidthWideband, make([]byte, maxOpusFrameSize))
		assert.Errorf(t, err, "%d samples", samples)
	}

	// Exact multiples of a coding unit up to three units are accepted.
	for _, samples := range []int{unit, 2 * unit, 3 * unit} {
		_, err := enc.EncodeSILK(make([]int16, samples), BandwidthWideband, make([]byte, maxOpusFrameSize))
		assert.NoErrorf(t, err, "%d samples", samples)
	}
}

// TestEncodeSILKComplexityGatesInterpolation checks that raising complexity
// above silkComplexityInterpolationThreshold reaches SetUseInterpolatedNLSFs
// on the underlying SILK encoder (end to end: encode two frames and confirm
// both still decode cleanly, exercising the interpolation branch inside
// internal/silk once firstFrameAfterReset clears on the second frame).
func TestEncodeSILKComplexityGatesInterpolation(t *testing.T) {
	enc, err := NewEncoder(WithComplexity(8))
	require.NoError(t, err)

	pcm := make([]int16, BandwidthWideband.SampleRate()/50) // 20 ms
	for i := range pcm {
		pcm[i] = int16(5000*math.Sin(2*math.Pi*float64(i)/48) + 1200*math.Sin(2*math.Pi*float64(i)/11))
	}

	packet := make([]byte, maxOpusFrameSize)
	for range 2 {
		n, encErr := enc.EncodeSILK(pcm, BandwidthWideband, packet)
		require.NoError(t, encErr)

		dec, decErr := NewDecoderWithOutput(BandwidthWideband.SampleRate(), 1)
		require.NoError(t, decErr)
		out := make([]float32, len(pcm))
		_, decErr = dec.DecodeToFloat32(packet[:n], out)
		require.NoError(t, decErr)
	}
}

func TestEncodeSILKInvalidBandwidth(t *testing.T) {
	enc, err := NewEncoder()
	require.NoError(t, err)

	for _, bandwidth := range []Bandwidth{BandwidthSuperwideband, BandwidthFullband, BandwidthAuto} {
		_, err = enc.EncodeSILK(make([]int16, 320), bandwidth, make([]byte, maxOpusFrameSize))
		assert.ErrorIsf(t, err, errInvalidBandwidth, "bandwidth %d", bandwidth)
	}
}

// TestEncodeSILKOutBufferTooSmallDoesNotAdvanceState checks that a rejected
// packet is transactional: retrying with enough output space must produce the
// exact packet an encoder that never saw the rejected call would produce.
// Cover both fresh and established prediction state at every supported SILK
// packet duration.
func TestEncodeSILKOutBufferTooSmallDoesNotAdvanceState(t *testing.T) {
	durations := []struct {
		name  string
		units int
	}{
		{duration20Ms, 1},
		{duration40Ms, 2},
		{duration60Ms, 3},
	}

	makePCM := func(samples, offset int) []int16 {
		pcm := make([]int16, samples)
		for i := range pcm {
			phase := float64(i + offset)
			pcm[i] = int16(5000*math.Sin(2*math.Pi*phase/48) + 1200*math.Sin(2*math.Pi*phase/11))
		}

		return pcm
	}

	unit := BandwidthWideband.SampleRate() / 50
	for _, duration := range durations {
		for _, warmed := range []bool{false, true} {
			state := "fresh"
			if warmed {
				state = "warmed"
			}

			t.Run(duration.name+"/"+state, func(t *testing.T) {
				retryEncoder, err := NewEncoder(WithComplexity(8))
				require.NoError(t, err)
				referenceEncoder, err := NewEncoder(WithComplexity(8))
				require.NoError(t, err)

				if warmed {
					warmup := makePCM(unit, 0)
					for _, encoder := range []*Encoder{retryEncoder, referenceEncoder} {
						_, err = encoder.EncodeSILK(warmup, BandwidthWideband, make([]byte, maxOpusFrameSize))
						require.NoError(t, err)
					}
				}

				pcm := makePCM(duration.units*unit, 37)
				shortOut := []byte{0xa5}
				n, err := retryEncoder.EncodeSILK(pcm, BandwidthWideband, shortOut)
				assert.Zero(t, n)
				assert.ErrorIs(t, err, errOutBufferTooSmall)
				assert.Equal(t, []byte{0xa5}, shortOut, "rejected call must not modify output")

				retryPacket := make([]byte, maxOpusFrameSize)
				retryN, err := retryEncoder.EncodeSILK(pcm, BandwidthWideband, retryPacket)
				require.NoError(t, err)

				referencePacket := make([]byte, maxOpusFrameSize)
				referenceN, err := referenceEncoder.EncodeSILK(pcm, BandwidthWideband, referencePacket)
				require.NoError(t, err)

				require.Equal(t, referenceN, retryN)
				assert.True(t, bytes.Equal(referencePacket[:referenceN], retryPacket[:retryN]),
					"retry packet differs from untouched reference")
			})
		}
	}
}

// TestSILKDCBlockRemovesConstantOffset mirrors celt's TestDCBlockRemovesConstantOffset:
// after 1 s of constant input the filter should settle to near zero.
func TestSILKDCBlockRemovesConstantOffset(t *testing.T) {
	const sampleRate = 16000
	pcm := make([]int16, sampleRate)
	for i := range pcm {
		pcm[i] = 10000
	}
	var mem float32
	out := applySILKDCBlock(pcm, sampleRate, &mem)

	var sum float64
	for i := len(out) - 1600; i < len(out); i++ {
		sum += float64(out[i])
	}
	assert.InDelta(t, 0.0, sum/1600, 100.0,
		"DC filter should attenuate constant offset (mean=%f)", sum/1600)
}

// TestSILKDCBlockPreservesInput checks the original pcm slice is untouched
// (applySILKDCBlock returns a new slice, unlike celt's in-place applyDCBlock).
func TestSILKDCBlockPreservesInput(t *testing.T) {
	pcm := []int16{5000, 5000, 5000, 5000}
	original := append([]int16(nil), pcm...)
	var mem float32
	applySILKDCBlock(pcm, 16000, &mem)
	assert.Equal(t, original, pcm)
}

// TestSILKDCBlockSaturates checks the clamp branches: a step between the
// int16 extremes while mem sits at the opposite extreme pushes y outside
// [-32768, 32767], which must saturate instead of wrapping.
func TestSILKDCBlockSaturates(t *testing.T) {
	memHigh := float32(-32768)
	outHigh := applySILKDCBlock([]int16{32767}, 16000, &memHigh)
	assert.Equal(t, int16(32767), outHigh[0])

	memLow := float32(32767)
	outLow := applySILKDCBlock([]int16{-32768}, 16000, &memLow)
	assert.Equal(t, int16(-32768), outLow[0])
}

// TestSILKDCBlockMultiFrameState checks the filter state persists correctly
// across calls: two half-frame runs must match one full run.
func TestSILKDCBlockMultiFrameState(t *testing.T) {
	const sampleRate = 16000
	pcm := make([]int16, 320)
	for i := range pcm {
		pcm[i] = int16(3000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}

	var memFull float32
	fullOut := applySILKDCBlock(pcm, sampleRate, &memFull)

	var memSplit float32
	out1 := applySILKDCBlock(pcm[:160], sampleRate, &memSplit)
	out2 := applySILKDCBlock(pcm[160:], sampleRate, &memSplit)
	splitOut := append(append([]int16(nil), out1...), out2...)

	assert.Equal(t, fullOut, splitOut)
}

// silkRateMeasurement is what runSILKRateProbe reports for one bandwidth.
type silkRateMeasurement struct {
	bitrate float64 // emitted bits per second
	peak    float64 // largest absolute decoded sample, 1.0 being full scale
	snrDB   float64 // round-trip signal to noise ratio
}

// silkSpeechLike generates count samples of voiced-speech-like audio at the
// given rate: a 180 Hz five-harmonic series under a slow amplitude envelope,
// so the LPC analysis, the pitch predictor and the gain control all have
// something to work with.
func silkSpeechLike(count, sampleRate int) []int16 {
	pcm := make([]int16, count)
	for i := range pcm {
		sec := float64(i) / float64(sampleRate)
		var sum float64
		for harmonic := 1; harmonic <= 5; harmonic++ {
			sum += math.Sin(2*math.Pi*180*float64(harmonic)*sec) / float64(harmonic)
		}
		env := 0.6 + 0.4*math.Sin(2*math.Pi*3*sec)
		pcm[i] = int16(env * 9000 * sum / harmonicsPeakAmplitude)
	}

	return pcm
}

// runSILKRateProbe encodes silkRateTestSeconds of speech-like audio one 20 ms
// coding unit at a time and decodes it with a single stateful decoder,
// reporting the emitted bitrate, the decoded peak, and the round-trip SNR.
func runSILKRateProbe(t *testing.T, bandwidth Bandwidth, bitrate int) silkRateMeasurement {
	t.Helper()

	encoder, err := NewEncoder(WithBitrate(bitrate), WithComplexity(silkRateTestComplexity))
	require.NoErrorf(t, err, "bandwidth %d", bandwidth)

	decoder, err := NewDecoderWithOutput(bandwidth.SampleRate(), 1)
	require.NoErrorf(t, err, "bandwidth %d", bandwidth)

	sampleRate := bandwidth.SampleRate()
	unit := sampleRate / 50 // one 20 ms coding unit
	pcm := silkSpeechLike(sampleRate*silkRateTestSeconds, sampleRate)

	packet := make([]byte, maxOpusFrameSize)
	decoded := make([]float32, 0, len(pcm))
	payloadBytes := 0
	for offset := 0; offset+unit <= len(pcm); offset += unit {
		size, encErr := encoder.EncodeSILK(pcm[offset:offset+unit], bandwidth, packet)
		require.NoErrorf(t, encErr, "bandwidth %d, sample %d", bandwidth, offset)
		payloadBytes += size

		out := make([]float32, unit)
		got, decErr := decoder.DecodeToFloat32(packet[:size], out)
		require.NoErrorf(t, decErr, "bandwidth %d, sample %d", bandwidth, offset)
		decoded = append(decoded, out[:got]...)
	}

	original := make([]float32, len(pcm))
	for i, sample := range pcm {
		original[i] = float32(sample) / 32768
	}

	peak := 0.0
	for _, sample := range decoded {
		peak = max(peak, math.Abs(float64(sample)))
	}

	return silkRateMeasurement{
		bitrate: float64(payloadBytes*8) / silkRateTestSeconds,
		peak:    peak,
		snrDB:   computeSNR(original, decoded),
	}
}

// TestEncodeSILKTracksTargetRate encodes speech-like audio at a 24 kb/s target
// for every SILK bandwidth and checks three things the round-trip tests above
// cannot: the emitted rate stays near the requested target, the decode stays
// below full scale, and the round-trip error stays well under the signal.
// A noise shaping quantizer predicting through a broken filter still produces
// non-silent output, so it passes every energy check while spending several
// times the target and clipping the decode.
func TestEncodeSILKTracksTargetRate(t *testing.T) {
	for _, bandwidth := range []Bandwidth{BandwidthNarrowband, BandwidthMediumband, BandwidthWideband} {
		got := runSILKRateProbe(t, bandwidth, silkRateTestBitrate)
		t.Logf("bandwidth %d: %.2f kb/s for a %d kb/s target, peak %.3f of full scale, SNR %.1f dB",
			bandwidth, got.bitrate/1000, silkRateTestBitrate/1000, got.peak, got.snrDB)

		assert.LessOrEqualf(t, got.bitrate, silkRateTestMaxOverrun*silkRateTestBitrate,
			"bandwidth %d: emitted %.2f kb/s for a %d kb/s target",
			bandwidth, got.bitrate/1000, silkRateTestBitrate/1000)
		assert.Lessf(t, got.peak, silkRateTestPeakCeiling,
			"bandwidth %d: decoded peak %.3f is at full scale", bandwidth, got.peak)
		assert.GreaterOrEqualf(t, got.snrDB, silkRateTestMinSNRDB,
			"bandwidth %d: round-trip SNR %.1f dB", bandwidth, got.snrDB)
	}
}
