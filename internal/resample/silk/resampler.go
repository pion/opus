// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package silkresample ports the RFC 6716 SILK resampler.
package silkresample

import (
	"errors"
	"math"
)

const (
	maxFIROrder    = 36
	maxIIROrder    = 6
	maxBatchSizeMS = 10

	downOrderFIR0 = 18
	downOrderFIR1 = 24
	downOrderFIR2 = 36
	orderFIR12    = 8
)

const (
	resamplerCopy = iota
	resamplerUp2HQ
	resamplerIIRFIR
	resamplerDownFIR
)

var (
	errInvalidInputSampleRate  = errors.New("input sample rate must be 8000, 12000, or 16000")
	errInvalidOutputSampleRate = errors.New("output sample rate must be 8000, 12000, 16000, 24000, or 48000")
	errInvalidInputLength      = errors.New("input length must be at least 1 ms")
	errOutBufferTooSmall       = errors.New("out buffer too small")
)

/*
 * Matrix of resampling methods used:
 *                                 Fs_out (kHz)
 *                        8      12     16     24     48
 *
 *               8        C      UF     U      UF     UF
 *              12        AF     C      UF     U      UF
 * Fs_in (kHz)  16        D      AF     C      UF     UF
 *
 * C   -> Copy (no resampling)
 * D   -> Allpass-based 2x downsampling
 * U   -> Allpass-based 2x upsampling
 * UF  -> Allpass-based 2x upsampling followed by FIR interpolation
 * AF  -> AR2 filter followed by FIR interpolation.
 */
var delayMatrixDec = [3][5]int{ //nolint:gochecknoglobals
	{4, 0, 2, 0, 0},
	{0, 9, 4, 7, 4},
	{0, 3, 12, 7, 7},
}

// Resampler converts one SILK decoder channel from 8/12/16 kHz to
// 8/12/16/24/48 kHz.
type Resampler struct {
	sIIR              [maxIIROrder]int32
	sFIR              [maxFIROrder]int32
	sFIRIIR           [orderFIR12]int16
	delayBuf          [48]int16
	resamplerFunction int
	batchSize         int
	invRatioQ16       int32
	firOrder          int
	firFracs          int
	fsInKHz           int
	fsOutKHz          int
	inputDelay        int
	coefs             []int16
}

// Init initializes the resampler state for one decoder channel.
//
//nolint:cyclop
func (r *Resampler) Init(inputSampleRate, outputSampleRate int) error {
	*r = Resampler{}

	inputRateID, err := inputRateID(inputSampleRate)
	if err != nil {
		return err
	}
	outputRateID, err := outputRateID(outputSampleRate)
	if err != nil {
		return err
	}

	r.inputDelay = delayMatrixDec[inputRateID][outputRateID]
	r.fsInKHz = inputSampleRate / 1000
	r.fsOutKHz = outputSampleRate / 1000
	r.batchSize = r.fsInKHz * maxBatchSizeMS

	up2x := 0
	switch {
	case outputSampleRate > inputSampleRate:
		if outputSampleRate == inputSampleRate*2 {
			r.resamplerFunction = resamplerUp2HQ
		} else {
			r.resamplerFunction = resamplerIIRFIR
			up2x = 1
		}
	case outputSampleRate < inputSampleRate:
		r.resamplerFunction = resamplerDownFIR
		switch {
		case outputSampleRate*4 == inputSampleRate*3:
			r.firFracs = 3
			r.firOrder = downOrderFIR0
			r.coefs = resampler34Coefs[:]
		case outputSampleRate*3 == inputSampleRate*2:
			r.firFracs = 2
			r.firOrder = downOrderFIR0
			r.coefs = resampler23Coefs[:]
		case outputSampleRate*2 == inputSampleRate:
			r.firFracs = 1
			r.firOrder = downOrderFIR1
			r.coefs = resampler12Coefs[:]
		case outputSampleRate*3 == inputSampleRate:
			r.firFracs = 1
			r.firOrder = downOrderFIR2
			r.coefs = resampler13Coefs[:]
		case outputSampleRate*4 == inputSampleRate:
			r.firFracs = 1
			r.firOrder = downOrderFIR2
			r.coefs = resampler14Coefs[:]
		case outputSampleRate*6 == inputSampleRate:
			r.firFracs = 1
			r.firOrder = downOrderFIR2
			r.coefs = resampler16Coefs[:]
		default:
			return errInvalidOutputSampleRate
		}
	default:
		r.resamplerFunction = resamplerCopy
	}

	r.invRatioQ16 = int32((int64(inputSampleRate)<<(14+up2x))/int64(outputSampleRate)) << 2 // #nosec G115
	for silkSMULWW(r.invRatioQ16, int32(outputSampleRate)) < int32(inputSampleRate<<up2x) { // #nosec G115
		r.invRatioQ16++
	}

	return nil
}

// Resample converts one non-interleaved channel to the configured output rate.
//
//nolint:cyclop
func (r *Resampler) Resample(in, out []float32) error {
	if r.fsInKHz == 0 {
		return errInvalidInputSampleRate
	}
	if len(in) < r.fsInKHz {
		return errInvalidInputLength
	}

	outLen := len(in) * r.fsOutKHz
	if outLen%r.fsInKHz != 0 {
		return errInvalidInputLength
	}
	outLen /= r.fsInKHz
	if len(out) < outLen {
		return errOutBufferTooSmall
	}
	in16 := make([]int16, len(in))
	for i := range in {
		in16[i] = float32ToInt16(in[i])
	}

	out16 := make([]int16, outLen+r.fsOutKHz)
	nSamples := r.fsInKHz - r.inputDelay
	remainingIn := in16[nSamples : len(in16)-r.inputDelay]
	copy(r.delayBuf[r.inputDelay:], in16[:nSamples])

	switch r.resamplerFunction {
	case resamplerUp2HQ:
		r.resamplerPrivateUp2HQ(out16, r.delayBuf[:r.fsInKHz])
		r.resamplerPrivateUp2HQ(out16[r.fsOutKHz:], remainingIn)
	case resamplerIIRFIR:
		r.resamplerPrivateIIRFIR(out16, r.delayBuf[:r.fsInKHz])
		r.resamplerPrivateIIRFIR(out16[r.fsOutKHz:], remainingIn)
	case resamplerDownFIR:
		r.resamplerPrivateDownFIR(out16, r.delayBuf[:r.fsInKHz])
		r.resamplerPrivateDownFIR(out16[r.fsOutKHz:], remainingIn)
	default:
		copy(out16, r.delayBuf[:r.fsInKHz])
		copy(out16[r.fsOutKHz:], remainingIn)
	}

	copy(r.delayBuf[:], in16[len(in16)-r.inputDelay:])
	for i := range outLen {
		out[i] = float32(out16[i]) / 32768.0
	}

	return nil
}

func inputRateID(sampleRate int) (int, error) {
	switch sampleRate {
	case 8000:
		return 0, nil
	case 12000:
		return 1, nil
	case 16000:
		return 2, nil
	default:
		return 0, errInvalidInputSampleRate
	}
}

func outputRateID(sampleRate int) (int, error) {
	switch sampleRate {
	case 8000:
		return 0, nil
	case 12000:
		return 1, nil
	case 16000:
		return 2, nil
	case 24000:
		return 3, nil
	case 48000:
		return 4, nil
	default:
		return 0, errInvalidOutputSampleRate
	}
}

func float32ToInt16(sample float32) int16 {
	sample32 := math.Round(float64(sample * 32768))
	sample32 = math.Max(sample32, -32768)
	sample32 = math.Min(sample32, 32767)

	return int16(sample32)
}
