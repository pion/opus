// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

const (
	vadNBands                   = 4
	vadInternalSubframesLog2    = 2
	vadInternalSubframes        = 1 << vadInternalSubframesLog2
	vadNoiseLevelSmoothCoefQ16  = 1024
	vadNoiseLevelsBias          = 50
	vadNegativeOffsetQ5         = 128
	vadSNRFactorQ16             = 45000
	vadSNRSmoothCoefQ18         = 4096
	anaFilterBankA20            = 5394 << 1
	anaFilterBankA21            = -24290
	variableHPMinCutoffHz       = 60
	variableHPMaxCutoffHz       = 100
	variableHPMinCutoffQ16      = variableHPMinCutoffHz << 16
	variableHPMaxDeltaFreqQ7    = 51   // round(0.4 * 128)
	variableHPSmoothCoef1Q16    = 6554 // round(0.1 * 65536)
	variableHPSmoothInitDefault = 0
)

// tiltWeights weights each band's SNR in the frequency-tilt measure.
//
//nolint:gochecknoglobals
var tiltWeights = [vadNBands]int32{30000, 6000, -12000, -12000}

// vadState holds the per-channel voice-activity detector state
// (silk_VAD_state).
type vadState struct {
	anaState       [2]int32
	anaState1      [2]int32
	anaState2      [2]int32
	xnrgSubfr      [vadNBands]int32
	nrgRatioSmthQ8 [vadNBands]int32
	hpState        int16
	nl             [vadNBands]int32
	invNL          [vadNBands]int32
	noiseLevelBias [vadNBands]int32
	counter        int32
}

// newVADState initializes the VAD with approximate pink-noise levels.
func newVADState() vadState {
	var v vadState
	for b := range vadNBands {
		v.noiseLevelBias[b] = max(vadNoiseLevelsBias/int32(b+1), 1)
	}
	for b := range vadNBands {
		v.nl[b] = 100 * v.noiseLevelBias[b]
		v.invNL[b] = math.MaxInt32 / v.nl[b]
	}
	v.counter = 15
	for b := range vadNBands {
		v.nrgRatioSmthQ8[b] = 100 * 256
	}

	return v
}

// getSpeechActivityQ8 returns the Q8 speech activity for one frame and updates
// the VAD state (silk_VAD_GetSA_Q8). It also returns the frequency tilt and
// per-band input-quality measures used by later analysis stages.
func (v *vadState) getSpeechActivityQ8(pcm []int16, frameLength, fsKHz int) (saQ8 int, tiltQ15 int32, qualityQ15 [vadNBands]int32) {
	decimatedFramelength1 := frameLength >> 1
	decimatedFramelength2 := frameLength >> 2
	decimatedFramelength := frameLength >> 3

	var xOffset [vadNBands]int
	xOffset[1] = decimatedFramelength + decimatedFramelength2
	xOffset[2] = xOffset[1] + decimatedFramelength
	xOffset[3] = xOffset[2] + decimatedFramelength2
	x := make([]int16, xOffset[3]+decimatedFramelength1)

	anaFiltBank1(pcm, &v.anaState, x, x[xOffset[3]:], frameLength)
	anaFiltBank1(x, &v.anaState1, x, x[xOffset[2]:], decimatedFramelength1)
	anaFiltBank1(x, &v.anaState2, x, x[xOffset[1]:], decimatedFramelength2)

	// High-pass differentiator on the lowest band.
	x[decimatedFramelength-1] >>= 1
	hpStateTmp := x[decimatedFramelength-1]
	for i := decimatedFramelength - 1; i > 0; i-- {
		x[i-1] >>= 1
		x[i] -= x[i-1]
	}
	x[0] -= v.hpState
	v.hpState = hpStateTmp

	// Energy per band.
	var xnrg [vadNBands]int32
	for b := range vadNBands {
		bandLength := frameLength >> min(vadNBands-b, vadNBands-1)
		subframeLength := bandLength >> vadInternalSubframesLog2
		offset := 0
		xnrg[b] = v.xnrgSubfr[b]
		var sumSquared int32
		for s := range vadInternalSubframes {
			sumSquared = 0
			for i := range subframeLength {
				xTmp := int32(x[xOffset[b]+i+offset]) >> 3
				sumSquared = smlabb(sumSquared, xTmp, xTmp)
			}
			if s < vadInternalSubframes-1 {
				xnrg[b] = addPosSat32(xnrg[b], sumSquared)
			} else {
				xnrg[b] = addPosSat32(xnrg[b], sumSquared>>1)
			}
			offset += subframeLength
		}
		v.xnrgSubfr[b] = sumSquared
	}

	v.getNoiseLevels(&xnrg)

	// Signal-plus-noise to noise ratio.
	var sumSquared, inputTilt int32
	var nrgToNoiseRatioQ8 [vadNBands]int32
	for b := range vadNBands {
		speechNrg := xnrg[b] - v.nl[b]
		if speechNrg <= 0 {
			nrgToNoiseRatioQ8[b] = 256

			continue
		}
		if uint32(xnrg[b])&0xFF800000 == 0 { //nolint:gosec // G115: xnrg is non-negative here.
			nrgToNoiseRatioQ8[b] = (xnrg[b] << 8) / (v.nl[b] + 1)
		} else {
			nrgToNoiseRatioQ8[b] = xnrg[b] / ((v.nl[b] >> 8) + 1)
		}
		snrQ7 := lin2log(nrgToNoiseRatioQ8[b]) - 8*128
		sumSquared = smlabb(sumSquared, snrQ7, snrQ7)
		if speechNrg < 1<<20 {
			snrQ7 = smulwb(sqrtApprox(speechNrg)<<6, snrQ7)
		}
		inputTilt = smlawb(inputTilt, tiltWeights[b], snrQ7)
	}

	sumSquared /= vadNBands
	pSNRdBQ7 := int32(int16(3 * sqrtApprox(sumSquared)))
	saQ15 := sigmQ15(smulwb(vadSNRFactorQ16, pSNRdBQ7) - vadNegativeOffsetQ5)
	tiltQ15 = (sigmQ15(inputTilt) - 16384) << 1

	// Scale by power level.
	var powerNrg int32
	for b := range vadNBands {
		powerNrg += int32(b+1) * ((xnrg[b] - v.nl[b]) >> 4)
	}
	if frameLength == 20*fsKHz {
		powerNrg >>= 1
	}
	switch {
	case powerNrg <= 0:
		saQ15 >>= 1
	case powerNrg < 16384:
		powerNrg = sqrtApprox(powerNrg << 16)
		saQ15 = smulwb(32768+powerNrg, saQ15)
	}
	saQ8 = min(int(saQ15>>7), math.MaxUint8)

	// Smoothed energy-to-noise ratio and per-band quality.
	smoothCoefQ16 := smulwb(vadSNRSmoothCoefQ18, smulwb(saQ15, saQ15))
	if frameLength == 10*fsKHz {
		smoothCoefQ16 >>= 1
	}
	for b := range vadNBands {
		v.nrgRatioSmthQ8[b] = smlawb(v.nrgRatioSmthQ8[b], nrgToNoiseRatioQ8[b]-v.nrgRatioSmthQ8[b], smoothCoefQ16)
		snrQ7 := 3 * (lin2log(v.nrgRatioSmthQ8[b]) - 8*128)
		qualityQ15[b] = sigmQ15((snrQ7 - 16*128) >> 4)
	}

	return saQ8, tiltQ15, qualityQ15
}

// getNoiseLevels updates the smoothed per-band noise estimate
// (silk_VAD_GetNoiseLevels).
func (v *vadState) getNoiseLevels(pX *[vadNBands]int32) {
	minCoef := int32(0)
	if v.counter < 1000 {
		minCoef = math.MaxInt16 / ((v.counter >> 4) + 1)
		v.counter++
	}
	for k := range vadNBands {
		nl := v.nl[k]
		nrg := addPosSat32(pX[k], v.noiseLevelBias[k])
		invNrg := math.MaxInt32 / nrg

		var coef int32
		switch {
		case nrg > nl<<3:
			coef = vadNoiseLevelSmoothCoefQ16 >> 3
		case nrg < nl:
			coef = vadNoiseLevelSmoothCoefQ16
		default:
			coef = smulwb(smulww(invNrg, nl), vadNoiseLevelSmoothCoefQ16<<1)
		}
		coef = max(coef, minCoef)

		v.invNL[k] = smlawb(v.invNL[k], invNrg-v.invNL[k], coef)
		nl = min(math.MaxInt32/v.invNL[k], 0x00FFFFFF)
		v.nl[k] = nl
	}
}

// anaFiltBank1 splits a signal into low and high bands (silk_ana_filt_bank_1).
func anaFiltBank1(in []int16, state *[2]int32, outL, outH []int16, n int) {
	for k := range n >> 1 {
		in32 := int32(in[2*k]) << 10
		y := in32 - state[0]
		xVal := smlawb(y, y, anaFilterBankA21)
		out1 := state[0] + xVal
		state[0] = in32 + xVal

		in32 = int32(in[2*k+1]) << 10
		y = in32 - state[1]
		xVal = smulwb(y, anaFilterBankA20)
		out2 := state[1] + xVal
		state[1] = in32 + xVal

		outL[k] = int16(sat16(rshiftRound32(out2+out1, 11)))
		outH[k] = int16(sat16(rshiftRound32(out2-out1, 11)))
	}
}

// hpVariableCutoff returns the updated high-pass smoother state, adapting the
// cutoff to the previous frame's pitch (silk_HP_variable_cutoff). It only
// changes when the previous frame was voiced.
func hpVariableCutoff(
	smth1Q15 int32,
	prevSignalType frameSignalType,
	prevLag, fsKHz int,
	qualityBand0Q15 int32,
	speechActivityQ8 int,
) int32 {
	if prevSignalType != frameSignalTypeVoiced {
		return smth1Q15
	}

	pitchFreqHzQ16 := (int32(fsKHz*1000) << 16) / int32(prevLag)
	pitchFreqLogQ7 := lin2log(pitchFreqHzQ16) - (16 << 7)

	pitchFreqLogQ7 = smlawb(pitchFreqLogQ7,
		smulwb((-qualityBand0Q15)<<2, qualityBand0Q15),
		pitchFreqLogQ7-(lin2log(variableHPMinCutoffQ16)-(16<<7)))

	deltaFreqQ7 := pitchFreqLogQ7 - (smth1Q15 >> 8)
	if deltaFreqQ7 < 0 {
		deltaFreqQ7 *= 3
	}
	deltaFreqQ7 = clamp(-variableHPMaxDeltaFreqQ7, deltaFreqQ7, variableHPMaxDeltaFreqQ7)

	smth1Q15 = smlawb(smth1Q15, smulbb(int32(speechActivityQ8), deltaFreqQ7), variableHPSmoothCoef1Q16)

	return clamp(lin2log(variableHPMinCutoffHz)<<8, smth1Q15, lin2log(variableHPMaxCutoffHz)<<8)
}
