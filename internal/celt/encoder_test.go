// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestEncodeBandThetaAllCases(t *testing.T) {
	enc := NewEncoder()
	enc.rangeEncoder.Init()
	encodeBandTheta(2, 4, 8, true, 1, &enc.rangeEncoder)
	encodeBandTheta(2, 4, 2, true, 1, &enc.rangeEncoder)
	encodeBandTheta(2, 4, 4, false, 1, &enc.rangeEncoder)
}
