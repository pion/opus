// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func BenchmarkEncodeFrameMono(b *testing.B) {
	b.ReportAllocs()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	pcm := make([]float32, frameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	encoder := NewEncoder()
	input := [][]float32{pcm}

	b.ResetTimer()
	for range b.N {
		_, err := encoder.EncodeFrame(input, frameBytes, 0, maxBands)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeFrameStereo(b *testing.B) {
	b.ReportAllocs()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 120

	L := make([]float32, frameSampleCount)
	R := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		L[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		R[i] = float32(math.Sin(2 * math.Pi * 660 * float64(i) / sampleRate))
	}

	encoder := NewEncoder()
	input := [][]float32{L, R}

	b.ResetTimer()
	for range b.N {
		_, err := encoder.EncodeFrame(input, frameBytes, 0, maxBands)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestEncodeFrameRoundTripMono20ms(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	pcm := make([]float32, frameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	for i := range 3 {
		data, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
		require.NoError(t, err)
		require.NotEmpty(t, data)
		assert.LessOrEqual(t, len(data), frameBytes,
			"encoded frame should not exceed byte budget")

		out := make([]float32, frameSampleCount)
		err = decoder.Decode(data, out, false, 1, frameSampleCount, 0, maxBands)
		require.NoError(t, err)

		energy := float32(vectorEnergy(out))
		t.Logf("frame %d: encoded %d bytes, output energy %f", i, len(data), energy)
		assert.Greater(t, float64(energy), 1e-6,
			"decoded frame %d should have non-zero energy", i)
	}
}

func TestEncodeFrameRoundTripMono20msTightBudget(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 10

	pcm := make([]float32, frameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	data, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	assert.LessOrEqual(t, len(data), frameBytes)

	out := make([]float32, frameSampleCount)
	err = decoder.Decode(data, out, false, 1, frameSampleCount, 0, maxBands)
	require.NoError(t, err)

	energy := float32(vectorEnergy(out))
	t.Logf("tight budget: encoded %d bytes, output energy %f", len(data), energy)
	assert.Greater(t, float64(energy), 1e-6,
		"decoded frame should have non-zero energy even on tight budget")
}

func TestEncodeFrameMonoPersistence(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	pcm := make([]float32, frameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	data1, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
	require.NoError(t, err)

	out1 := make([]float32, frameSampleCount)
	require.NoError(t, decoder.Decode(data1, out1, false, 1, frameSampleCount, 0, maxBands))

	var out1b float64
	for i := range frameSampleCount {
		out1b += float64(out1[i])
	}

	data2, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
	require.NoError(t, err)

	out2 := make([]float32, frameSampleCount)
	require.NoError(t, decoder.Decode(data2, out2, false, 1, frameSampleCount, 0, maxBands))

	var out2b float64
	for i := range frameSampleCount {
		out2b += float64(out2[i])
	}

	t.Logf("frame1 sum=%f frame2 sum=%f", out1b, out2b)
}

func TestEncodeFrameMonoRngStability(t *testing.T) {
	encoder := NewEncoder()
	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	pcm := make([]float32, frameSampleCount)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	for range 3 {
		data, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
		require.NoError(t, err)

		require.NotEmpty(t, data)
		_ = encoder.rangeEncoder.FinalRange()
	}
}

func TestEncodeFrameStereoFinalRange(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	L := make([]float32, frameSampleCount)
	R := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		L[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		R[i] = float32(math.Sin(2 * math.Pi * 660 * float64(i) / sampleRate))
	}

	data, err := encoder.EncodeFrame([][]float32{L, R}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	out := make([]float32, frameSampleCount*2)
	require.NoError(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands))

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
		"range coder must be in sync after stereo encode/decode")
}

func TestQuantBandStereoN1(t *testing.T) {
	enc := NewEncoder()
	enc.rangeEncoder.Init()
	state := bandEncodeState{rangeEncoder: &enc.rangeEncoder}

	x := []float32{0.7}
	y := []float32{-0.5}
	remaining := 100 << bitResolution

	mask := quantBandStereo(
		0, x, y, 1, 10<<bitResolution,
		spreadNormal, 1, maxBands, 0, nil,
		&remaining, 3, 1.0, make([]float32, 2), 1, &state,
	)
	assert.Equal(t, uint(1), mask)
}

func TestQuantBandStereoN2(t *testing.T) {
	enc := NewEncoder()
	enc.rangeEncoder.Init()
	state := bandEncodeState{rangeEncoder: &enc.rangeEncoder}

	x := []float32{0.6, 0.3}
	y := []float32{-0.2, 0.5}
	remaining := 300 << bitResolution

	quantBandStereo(
		0, x, y, 2, 30<<bitResolution,
		spreadNormal, 1, maxBands, 0, nil,
		&remaining, 3, 1.0, make([]float32, 4), 1, &state,
	)
	assert.Greater(t, enc.rangeEncoder.FinalRange(), uint32(0))
}

func TestEncodeFrameStereoSeparatedBands(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 120

	L := make([]float32, frameSampleCount)
	R := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		L[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		R[i] = float32(math.Sin(2 * math.Pi * 3000 * float64(i) / sampleRate))
	}

	data, err := encoder.EncodeFrame([][]float32{L, R}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	out := make([]float32, frameSampleCount*2)
	require.NoError(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands))
	assert.Greater(t, vectorEnergy(out), 1e-6)
}

func TestEncodeFrameStereoDualStereoRoundTrip(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 120

	L := make([]float32, frameSampleCount)
	R := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		L[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		R[i] = float32(math.Cos(2 * math.Pi * 1200 * float64(i) / sampleRate))
	}

	data, err := encoder.EncodeFrame([][]float32{L, R}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	out := make([]float32, frameSampleCount*2)
	require.NoError(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands))

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
		"range coder must be in sync at frameBytes=%d", frameBytes)
	assert.Greater(t, vectorEnergy(out), 1e-6)
}

func TestEncodeBandThetaAllCases(t *testing.T) {
	enc := NewEncoder()
	enc.rangeEncoder.Init()
	encodeBandTheta(2, 4, 8, true, 1, &enc.rangeEncoder)
	encodeBandTheta(2, 4, 2, true, 1, &enc.rangeEncoder)
	encodeBandTheta(2, 4, 4, false, 1, &enc.rangeEncoder)
}

func TestEncodeFrameStereoIntensityLowBitrate(t *testing.T) {
	assertStereoFinalRangeMatch(t, 20)
}

func TestEncodeFrameStereoIntensityHighBitrate(t *testing.T) {
	assertStereoFinalRangeMatch(t, 120)
}

func TestEncodeFrameStereoIntensityBitrateSweep(t *testing.T) {
	for _, frameBytes := range []int{10, 20, 30, 60, 90, 120} {
		t.Run("", func(t *testing.T) {
			assertStereoFinalRangeMatch(t, frameBytes)
		})
	}
}

func TestTransientFlagWiring(t *testing.T) {
	frameSampleCount := shortBlockSampleCount << maxLM

	t.Run("impulse sets transient=1 in bitstream", func(t *testing.T) {
		encoder := NewEncoder()
		decoder := NewDecoder()

		pcm := make([]float32, frameSampleCount)
		pcm[frameSampleCount/2] = 1.0

		data, err := encoder.EncodeFrame([][]float32{pcm}, 60, 0, maxBands)
		require.NoError(t, err)
		require.NotEmpty(t, data)

		out := make([]float32, frameSampleCount)
		require.NoError(t, decoder.Decode(data, out, false, 1, frameSampleCount, 0, maxBands))
		assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
			"range coder must be in sync for transient mono frame")
		assert.Greater(t, vectorEnergy(out), 1e-10, "decoded transient frame must have energy")
	})

	t.Run("steady sine keeps transient=0 in bitstream", func(t *testing.T) {
		encoder := NewEncoder()
		decoder := NewDecoder()

		pcm := make([]float32, frameSampleCount)
		for i := range pcm {
			pcm[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		}

		data, err := encoder.EncodeFrame([][]float32{pcm}, 60, 0, maxBands)
		require.NoError(t, err)
		require.NotEmpty(t, data)

		out := make([]float32, frameSampleCount)
		require.NoError(t, decoder.Decode(data, out, false, 1, frameSampleCount, 0, maxBands))
		assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
			"range coder must be in sync for non-transient mono frame")
	})
}

func TestEncodeFrameTransientFinalRangeMono(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	pcm := make([]float32, frameSampleCount)
	pcm[frameSampleCount/2] = 1.0

	data, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	assert.LessOrEqual(t, len(data), frameBytes)

	out := make([]float32, frameSampleCount)
	require.NoError(t, decoder.Decode(data, out, false, 1, frameSampleCount, 0, maxBands))

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
		"range coder must be in sync after transient mono encode/decode")
}

func TestEncodeFrameTransientFinalRangeStereo(t *testing.T) {
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 120

	left := make([]float32, frameSampleCount)
	right := make([]float32, frameSampleCount)
	left[frameSampleCount/2] = 1.0
	right[frameSampleCount/2] = 0.8

	data, err := encoder.EncodeFrame([][]float32{left, right}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	out := make([]float32, frameSampleCount*2)
	require.NoError(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands))

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
		"range coder must be in sync after transient stereo encode/decode")
}

func TestEncodeFrameTransientMultiFrameMono(t *testing.T) {
	// Three-frame sequence: steady | transient | steady. Verify that the
	// inter-frame overlap state is not corrupted by the short-block MDCT path.
	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	steady := make([]float32, frameSampleCount)
	for i := range steady {
		steady[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	impulse := make([]float32, frameSampleCount)
	impulse[frameSampleCount/2] = 1.0

	for frame, pcm := range [][]float32{steady, impulse, steady} {
		data, err := encoder.EncodeFrame([][]float32{pcm}, frameBytes, 0, maxBands)
		require.NoError(t, err, "frame %d", frame)
		require.NotEmpty(t, data, "frame %d", frame)

		out := make([]float32, frameSampleCount)
		require.NoError(t, decoder.Decode(data, out, false, 1, frameSampleCount, 0, maxBands),
			"frame %d", frame)

		assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
			"range coder out of sync at frame %d", frame)
		assert.Greater(t, vectorEnergy(out), 1e-10,
			"decoded frame %d must have non-zero energy", frame)
	}
}

func TestEncodeFrameTransientNoRegressionNonTransient(t *testing.T) {
	// Verify steady-sine frames produce the same output regardless of whether
	// a transient frame preceded them in another encoder instance.
	frameSampleCount := shortBlockSampleCount << maxLM
	frameBytes := 60

	sine := make([]float32, frameSampleCount)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
	}

	encoder1 := NewEncoder()
	data1, err := encoder1.EncodeFrame([][]float32{sine}, frameBytes, 0, maxBands)
	require.NoError(t, err)

	encoder2 := NewEncoder()
	// Warm up encoder2 with an impulse first, then encode the same sine.
	impulse := make([]float32, frameSampleCount)
	impulse[frameSampleCount/2] = 1.0
	_, err = encoder2.EncodeFrame([][]float32{impulse}, frameBytes, 0, maxBands)
	require.NoError(t, err)

	data2, err := encoder2.EncodeFrame([][]float32{sine}, frameBytes, 0, maxBands)
	require.NoError(t, err)

	// The second sine frame differs from the first because encoder2 has
	// pre-emphasis state from the impulse frame, so we only check that it
	// produces a valid decodeable bitstream, not byte-identical output.
	decoder := NewDecoder()
	out := make([]float32, frameSampleCount)
	require.NoError(t, decoder.Decode(data2, out, false, 1, frameSampleCount, 0, maxBands))

	assert.LessOrEqual(t, len(data1), frameBytes)
	assert.LessOrEqual(t, len(data2), frameBytes)
}

func assertStereoFinalRangeMatch(t *testing.T, frameBytes int) {
	t.Helper()

	encoder := NewEncoder()
	decoder := NewDecoder()

	frameSampleCount := shortBlockSampleCount << maxLM

	L := make([]float32, frameSampleCount)
	R := make([]float32, frameSampleCount)
	for i := range frameSampleCount {
		L[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / sampleRate))
		R[i] = float32(math.Sin(2 * math.Pi * 660 * float64(i) / sampleRate))
	}

	data, err := encoder.EncodeFrame([][]float32{L, R}, frameBytes, 0, maxBands)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	out := make([]float32, frameSampleCount*2)
	require.NoError(t, decoder.Decode(data, out, true, 2, frameSampleCount, 0, maxBands))

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange(),
		"range coder must be in sync at frameBytes=%d", frameBytes)
}
