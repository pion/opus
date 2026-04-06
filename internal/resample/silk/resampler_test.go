// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	for _, test := range []struct {
		inputSampleRate  int
		outputSampleRate int
		fsInKHz          int
		fsOutKHz         int
		inputDelay       int
	}{
		{inputSampleRate: 8000, outputSampleRate: 8000, fsInKHz: 8, fsOutKHz: 8, inputDelay: 4},
		{inputSampleRate: 8000, outputSampleRate: 12000, fsInKHz: 8, fsOutKHz: 12, inputDelay: 0},
		{inputSampleRate: 8000, outputSampleRate: 16000, fsInKHz: 8, fsOutKHz: 16, inputDelay: 2},
		{inputSampleRate: 8000, outputSampleRate: 24000, fsInKHz: 8, fsOutKHz: 24, inputDelay: 0},
		{inputSampleRate: 8000, outputSampleRate: 48000, fsInKHz: 8, fsOutKHz: 48, inputDelay: 0},
		{inputSampleRate: 12000, outputSampleRate: 8000, fsInKHz: 12, fsOutKHz: 8, inputDelay: 0},
		{inputSampleRate: 12000, outputSampleRate: 12000, fsInKHz: 12, fsOutKHz: 12, inputDelay: 9},
		{inputSampleRate: 12000, outputSampleRate: 16000, fsInKHz: 12, fsOutKHz: 16, inputDelay: 4},
		{inputSampleRate: 12000, outputSampleRate: 24000, fsInKHz: 12, fsOutKHz: 24, inputDelay: 7},
		{inputSampleRate: 12000, outputSampleRate: 48000, fsInKHz: 12, fsOutKHz: 48, inputDelay: 4},
		{inputSampleRate: 16000, outputSampleRate: 8000, fsInKHz: 16, fsOutKHz: 8, inputDelay: 0},
		{inputSampleRate: 16000, outputSampleRate: 12000, fsInKHz: 16, fsOutKHz: 12, inputDelay: 3},
		{inputSampleRate: 16000, outputSampleRate: 16000, fsInKHz: 16, fsOutKHz: 16, inputDelay: 12},
		{inputSampleRate: 16000, outputSampleRate: 24000, fsInKHz: 16, fsOutKHz: 24, inputDelay: 7},
		{inputSampleRate: 16000, outputSampleRate: 48000, fsInKHz: 16, fsOutKHz: 48, inputDelay: 7},
	} {
		var resampler Resampler
		assert.NoError(t, resampler.Init(test.inputSampleRate, test.outputSampleRate))
		assert.Equal(t, test.fsInKHz, resampler.fsInKHz)
		assert.Equal(t, test.fsOutKHz, resampler.fsOutKHz)
		assert.Equal(t, test.inputDelay, resampler.inputDelay)
	}
}

func TestInitInvalidSampleRate(t *testing.T) {
	var resampler Resampler
	assert.Error(t, resampler.Init(24000, 48000))
	assert.Error(t, resampler.Init(16000, 44100))
}

func TestResampleInvalidLength(t *testing.T) {
	var resampler Resampler
	assert.NoError(t, resampler.Init(16000, 48000))
	assert.ErrorIs(t, resampler.Resample(make([]float32, 15), make([]float32, 45)), errInvalidInputLength)
}

func TestResampleNonIntegralOutputLength(t *testing.T) {
	var resampler Resampler
	assert.NoError(t, resampler.Init(16000, 12000))
	assert.ErrorIs(t, resampler.Resample(make([]float32, 17), make([]float32, 20)), errNonIntegralInputLength)
}

func TestResampleMatchesReferenceImpulse(t *testing.T) {
	for _, test := range []struct {
		inputSampleRate  int
		outputSampleRate int
		expected         []int16
	}{
		{
			inputSampleRate:  8000,
			outputSampleRate: 8000,
			expected: []int16{
				0, 0, 0, 0, 32767, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			},
		},
		{
			inputSampleRate:  8000,
			outputSampleRate: 12000,
			expected: []int16{
				0, 0, 2, 10, 326, 3476, 14055, 25740, 17153, -8161, -10975, 7757, 4868, -8036, 1033, 5439, -4615, -803, 4243, -2767,
				-1091, 3110, -1774, -960, 2248, -1195, -750, 1618, -830, -561,
				1163, -586, -411, 836, -418, -297, 600, -299, -215, 431,
			},
		},
		{
			inputSampleRate:  8000,
			outputSampleRate: 16000,
			expected: []int16{
				0, 0, 0, 0, 119, 1142, 5087, 13635, 23555, 25210, 11900, -7500, -13883, -2058, 9854, 5362, -6237, -5999, 3802, 5660,
				-2286, -5023, 1368, 4347, -817, -3719, 487, 3165, -291, -2688,
				173, 2280, -103, -1933, 62, 1639, -37, -1389, 22, 1177,
			},
		},
		{
			inputSampleRate:  8000,
			outputSampleRate: 24000,
			expected: []int16{
				0, 0, 0, 0, 2, 3, 10, 59, 326, 1231, 3476, 7748, 14055, 21013, 25740, 24965, 17153, 4305, -8161, -14177,
				-10975, -1372, 7757, 10189, 4868, -3446, -8036, -5667, 1033, 6093,
				5439, 213, -4615, -4873, -803, 3542, 4243, 1040, -2767, -3645,
			},
		},
		{
			inputSampleRate:  8000,
			outputSampleRate: 48000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 2, 3, 3, 6, 10, 23, 59, 146, 326, 659, 1231, 2138,
				3476, 5333, 7748, 10695, 14055, 17604, 21013, 23871, 25740, 26204,
				24965, 21910, 17153, 11079, 4305, -2386, -8161, -12269, -14177, -13688,
			},
		},
		{
			inputSampleRate:  12000,
			outputSampleRate: 8000,
			expected: []int16{
				0, 128, -192, 373, -863, 2751, 21132, -1704, 172, 378, -635, 816, -790, 775, -758, 729, -701, 665, -632, 593,
				-558, 519, -484, 447, -415, 381, -351, 321, -294, 267, -244, 221, -201, 181, -165, 148, -134, 120, -108, 96,
			},
		},
		{
			inputSampleRate:  12000,
			outputSampleRate: 12000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 32767, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			},
		},
		{
			inputSampleRate:  12000,
			outputSampleRate: 16000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 10, 469, 5333, 19351, 24965, 896, -13688, 5923, 4868, -8092, 4125, 1702,
				-4873, 4147, -1053, -1939, 3110, -2247, 322, 1351, -1907, 1303,
				-129, -857, 1163, -779, 64, 529, -709, 471, -36, -324,
			},
		},
		{
			inputSampleRate:  12000,
			outputSampleRate: 24000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 119, 1142, 5087, 13635, 23555, 25210,
				11900, -7500, -13883, -2058, 9854, 5362, -6237, -5999, 3802, 5660,
				-2286, -5023, 1368, 4347, -817, -3719, 487, 3165, -291, -2688,
			},
		},
		{
			inputSampleRate:  12000,
			outputSampleRate: 48000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 3, 4, 10, 37, 146, 469, 1231, 2748, 5333, 9161, 14055,
				19351, 23871, 26170, 24965, 19726, 11079, 896,
			},
		},
		{
			inputSampleRate:  16000,
			outputSampleRate: 8000,
			expected: []int16{
				0, 77, -154, 364, -924, 3313, 15615, -2597, 1023, -405, 88, 122, -195, 231, -254, 267, -273, 272, -267, 258,
				-248, 235, -221, 207, -193, 178, -164, 150, -138, 125, -114, 103, -93, 84, -75, 67, -60, 54, -48, 42,
			},
		},
		{
			inputSampleRate:  16000,
			outputSampleRate: 12000,
			expected: []int16{
				0, 0, 0, -38, 111, -201, 351, -735, 2201, 23761, -738, -570, 979, -1155, 1214, -1179, 1131, -1089, 1023, -968,
				916, -851, 796, -746, 686, -637, 591, -540, 498, -459, 416, -382, 349, -316, 288, -262, 235, -214, 193, -173,
			},
		},
		{
			inputSampleRate:  16000,
			outputSampleRate: 16000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32767, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			},
		},
		{
			inputSampleRate:  16000,
			outputSampleRate: 24000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 59, 1231, 7748, 21013, 24965, 4305,
				-14177, -1372, 10189, -3446, -5667, 6093, 213, -4873, 3542, 1040,
				-3645, 2198, 1047, -2647, 1449, 856, -1907, 994, 651, -1372,
			},
		},
		{
			inputSampleRate:  16000,
			outputSampleRate: 48000,
			expected: []int16{
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 2, 3, 10, 59, 326, 1231, 3476, 7748, 14055,
				21013, 25740, 24965, 17153, 4305, -8161,
			},
		},
	} {
		var resampler Resampler
		t.Run(fmt.Sprintf("%d_to_%d", test.inputSampleRate, test.outputSampleRate), func(t *testing.T) {
			assert.NoError(t, resampler.Init(test.inputSampleRate, test.outputSampleRate))

			in := make([]float32, test.inputSampleRate/50)
			in[0] = 1
			out := make([]float32, len(in)*test.outputSampleRate/test.inputSampleRate)

			assert.NoError(t, resampler.Resample(in, out))

			got := make([]int16, len(test.expected))
			for i := range got {
				got[i] = int16(math.Round(float64(out[i] * 32768)))
			}
			assert.Equal(t, test.expected, got)
		})
	}
}

func TestResampleChunking(t *testing.T) {
	for _, test := range []struct {
		inputSampleRate  int
		outputSampleRate int
	}{
		{inputSampleRate: 8000, outputSampleRate: 12000},
		{inputSampleRate: 8000, outputSampleRate: 16000},
		{inputSampleRate: 8000, outputSampleRate: 24000},
		{inputSampleRate: 8000, outputSampleRate: 48000},
		{inputSampleRate: 12000, outputSampleRate: 8000},
		{inputSampleRate: 12000, outputSampleRate: 16000},
		{inputSampleRate: 12000, outputSampleRate: 24000},
		{inputSampleRate: 12000, outputSampleRate: 48000},
		{inputSampleRate: 16000, outputSampleRate: 8000},
		{inputSampleRate: 16000, outputSampleRate: 12000},
		{inputSampleRate: 16000, outputSampleRate: 24000},
		{inputSampleRate: 16000, outputSampleRate: 48000},
	} {
		t.Run(fmt.Sprintf("%d_to_%d", test.inputSampleRate, test.outputSampleRate), func(t *testing.T) {
			whole := Resampler{}
			assert.NoError(t, whole.Init(test.inputSampleRate, test.outputSampleRate))
			chunked := Resampler{}
			assert.NoError(t, chunked.Init(test.inputSampleRate, test.outputSampleRate))

			in := make([]float32, test.inputSampleRate*60/1000)
			for i := range in {
				in[i] = float32((i%31)-15) / 32768
			}

			wholeOut := make([]float32, len(in)*test.outputSampleRate/test.inputSampleRate)
			assert.NoError(t, whole.Resample(in, wholeOut))

			chunkedOut := make([]float32, len(wholeOut))
			inChunk := test.inputSampleRate * 20 / 1000
			outChunk := test.outputSampleRate * 20 / 1000
			for i := range 3 {
				assert.NoError(t, chunked.Resample(
					in[i*inChunk:(i+1)*inChunk],
					chunkedOut[i*outChunk:(i+1)*outChunk],
				))
			}

			assert.Equal(t, len(wholeOut), len(chunkedOut))
			for i := range wholeOut {
				if wholeOut[i] != chunkedOut[i] {
					assert.FailNowf(
						t,
						"chunked output mismatch",
						"first mismatch at %d: whole=%v chunked=%v",
						i,
						wholeOut[i],
						chunkedOut[i],
					)
				}
			}
		})
	}
}

func TestResampleRoundTripDifference(t *testing.T) {
	for _, test := range []struct {
		inputSampleRate int
		midSampleRate   int
	}{
		{inputSampleRate: 8000, midSampleRate: 12000},
		{inputSampleRate: 8000, midSampleRate: 16000},
		{inputSampleRate: 12000, midSampleRate: 8000},
		{inputSampleRate: 12000, midSampleRate: 16000},
		{inputSampleRate: 16000, midSampleRate: 8000},
		{inputSampleRate: 16000, midSampleRate: 12000},
	} {
		t.Run(
			fmt.Sprintf("%d_to_%d_to_%d", test.inputSampleRate, test.midSampleRate, test.inputSampleRate),
			func(t *testing.T) {
				in := makeTestSignal(test.inputSampleRate)
				mid := resampleForTest(t, in, test.inputSampleRate, test.midSampleRate)
				out := resampleForTest(t, mid, test.midSampleRate, test.inputSampleRate)
				lag, maxAbs, rmse := compareSignalsWithBestLag(in, out, test.inputSampleRate/100)
				t.Logf("lag=%d max_abs=%f rmse=%f", lag, maxAbs, rmse)
				assert.LessOrEqual(t, maxAbs, 0.09)
				assert.LessOrEqual(t, rmse, 0.04)
			},
		)
	}
}

func makeTestSignal(sampleRate int) []float32 {
	out := make([]float32, sampleRate)
	for i := range out {
		ts := float64(i) / float64(sampleRate)
		out[i] = float32(
			0.2*math.Sin(2*math.Pi*440*ts) +
				0.1*math.Sin(2*math.Pi*1234*ts) +
				0.05*math.Sin(2*math.Pi*2100*ts),
		)
	}

	return out
}

func resampleForTest(t *testing.T, in []float32, inputSampleRate, outputSampleRate int) []float32 {
	t.Helper()

	var resampler Resampler
	assert.NoError(t, resampler.Init(inputSampleRate, outputSampleRate))
	out := make([]float32, len(in)*outputSampleRate/inputSampleRate)
	assert.NoError(t, resampler.Resample(in, out))

	return out
}

func compareSignalsWithBestLag(input, output []float32, maxLag int) (int, float64, float64) {
	bestLag := 0
	bestRMSE := math.Inf(1)
	bestMaxAbs := 0.0
	for lag := -maxLag; lag <= maxLag; lag++ {
		startA := 0
		startB := 0
		length := len(input)
		if lag < 0 {
			startA = -lag
			length += lag
		} else if lag > 0 {
			startB = lag
			length -= lag
		}
		maxAbs := 0.0
		sumErrSq := 0.0
		for i := 0; i < length; i++ {
			err := math.Abs(float64(input[startA+i] - output[startB+i]))
			maxAbs = math.Max(maxAbs, err)
			sumErrSq += err * err
		}
		rmse := math.Sqrt(sumErrSq / float64(length))
		if rmse < bestRMSE {
			bestLag = lag
			bestRMSE = rmse
			bestMaxAbs = maxAbs
		}
	}

	return bestLag, bestMaxAbs, bestRMSE
}
