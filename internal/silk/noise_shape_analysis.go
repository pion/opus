// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// Faithful port of silk_noise_shape_analysis_FLP and silk_process_gains_FLP
// (warping disabled, matching the low-complexity / non-delayed-decision path).

const (
	bgSNRDecrDB              = 2.0
	harmSNRIncrDB            = 2.0
	shapeWhiteNoiseFraction  = 3e-5
	bandwidthExpansion       = 0.94
	harmonicShapingConst     = 0.3
	highRateHarmonicShaping  = 0.2
	hpNoiseCoefConst         = 0.25
	harmHPNoiseCoefConst     = 0.35
	lowFreqShapingConst      = 4.0
	lowQualityLFShapingDecr  = 0.5
	subframeSmoothCoef       = 0.4
	energyVarThresholdQntOff = 0.6
	minQGainDB               = 2
	subFrameLengthMS         = 5

	lambdaOffset            = 1.2
	lambdaSpeechAct         = -0.2
	lambdaDelayedDecisions  = -0.05
	lambdaInputQuality      = -0.1
	lambdaCodingQuality     = -0.2
	lambdaQuantOffsetConst  = 0.8
	nStatesDelayedDecision  = 1 // non-delayed NSQ
	shapeLPCOrderLowComplex = 12
	laShapeMSLowComplex     = 3
)

// f2iQ rounds x to the nearest integer (silk_float2int).
func f2iQ(x float32) int32 {
	return int32(math.RoundToEven(float64(x)))
}

// limitCoefsFLP bandwidth-expands an AR filter until every coefficient's
// magnitude is within limit (silk_noise_shape_analysis's limit_coefs).
func limitCoefsFLP(coefs []float32, limit float32, order int) {
	for iter := range 10 {
		maxAbs := float32(-1)
		ind := 0
		for i := range order {
			if a := absFloat32(coefs[i]); a > maxAbs {
				maxAbs = a
				ind = i
			}
		}
		if maxAbs <= limit {
			return
		}
		chirp := 0.99 - (0.8+0.1*float32(iter))*(maxAbs-limit)/(maxAbs*float32(ind+1))
		bwexpanderFLP(coefs, order, chirp)
	}
}

func absFloat32(x float32) float32 {
	if x < 0 {
		return -x
	}

	return x
}

// shapeResult holds the noise-shaping outputs for one frame.
type shapeResult struct {
	gains         []float32
	arQ13         []int16
	tiltQ14       []int32
	lfShpQ14      []int32
	harmShapeQ14  []int32
	inputQuality  float32
	codingQuality float32
	snrAdjDB      float32
	quantOffset   frameQuantizationOffsetType // unvoiced choice; voiced set in processGains
}

// noiseShapeAnalysis computes the noise-shaping AR filters, initial gains,
// spectral tilt, low-frequency and harmonic shaping (silk_noise_shape_analysis_FLP).
// shapeBuf holds la_shape samples of history, the frame, then la_shape samples
// of look-ahead padding. pitchRes is the whitened LPC residual for the current
// frame (findPitchLags's res, offset past its ltpMemLength history) — the
// reference uses this, not the raw signal, for the unvoiced sparseness measure
// that picks quantOffsetType; reusing shapeBuf there was a bug.
//
//nolint:cyclop // faithful port of the noise-shape analysis stage.
func (e *Encoder) noiseShapeAnalysis(
	shapeBuf, pitchRes []float32,
	signalType frameSignalType,
	pitchL []int,
	predGain float32,
	snrDBQ7 int32,
	speechActQ8 int,
	qualityBands [vadNBands]int32,
	fsKHz, nbSubfr, subfrLength int,
) *shapeResult {
	order := shapeLPCOrderLowComplex
	laShape := laShapeMSLowComplex * fsKHz
	shapeWinLength := subFrameLengthMS*fsKHz + 2*laShape

	sr := &shapeResult{
		gains:        make([]float32, nbSubfr),
		arQ13:        make([]int16, nbSubfr*maxShapeLPCOrder),
		tiltQ14:      make([]int32, nbSubfr),
		lfShpQ14:     make([]int32, nbSubfr),
		harmShapeQ14: make([]int32, nbSubfr),
	}

	// Gain control.
	snrAdjDB := float32(snrDBQ7) * (1.0 / 128.0)
	sr.inputQuality = 0.5 * (float32(qualityBands[0]) + float32(qualityBands[1])) * (1.0 / 32768.0)
	sr.codingQuality = sigmoid(0.25 * (snrAdjDB - 20.0))
	speechAct := float32(speechActQ8) * (1.0 / 256.0)

	// useCBR == 0: reduce coding SNR during low speech activity.
	b := 1.0 - speechAct
	snrAdjDB -= bgSNRDecrDB * sr.codingQuality * (0.5 + 0.5*sr.inputQuality) * b * b
	if signalType == frameSignalTypeVoiced {
		snrAdjDB += harmSNRIncrDB * e.ltpCorr
	} else {
		snrAdjDB += (-0.4*float32(snrDBQ7)*(1.0/128.0) + 6.0) * (1.0 - sr.inputQuality)
	}
	sr.snrAdjDB = snrAdjDB

	// Quantizer offset for unvoiced from a sparseness measure.
	sr.quantOffset = frameQuantizationOffsetTypeLow
	if signalType == frameSignalTypeVoiced {
		sr.quantOffset = frameQuantizationOffsetTypeLow // may be overridden in processGains
	} else {
		nSamples := 2 * fsKHz
		nSegs := subFrameLengthMS * nbSubfr / 2
		var energyVariation, logEnergyPrev float32
		ptr := 0
		for k := range nSegs {
			nrg := float32(nSamples) + float32(energyFLP(pitchRes[ptr:], nSamples))
			logEnergy := silkLog2(float64(nrg))
			if k > 0 {
				energyVariation += absFloat32(logEnergy - logEnergyPrev)
			}
			logEnergyPrev = logEnergy
			ptr += nSamples
		}
		if energyVariation > energyVarThresholdQntOff*float32(nSegs-1) {
			sr.quantOffset = frameQuantizationOffsetTypeLow
		} else {
			sr.quantOffset = frameQuantizationOffsetTypeHigh
		}
	}

	// Bandwidth expansion for the shaping filter.
	strength := float32(findPitchWhiteNoiseFraction) * predGain
	bwExp := float32(bandwidthExpansion) / (1.0 + strength*strength)

	// Per-subframe shaping AR coefficients and gains.
	xWindowed := make([]float32, shapeWinLength)
	autoCorr := make([]float32, order+1)
	rc := make([]float32, order)
	flatPart := fsKHz * 3
	slopePart := (shapeWinLength - flatPart) / 2
	xPtr := 0
	for k := range nbSubfr { //nolint:varnamelen // k is the subframe index throughout.
		applySineWindowFLP(xWindowed, shapeBuf[xPtr:], 1, slopePart)
		copy(xWindowed[slopePart:slopePart+flatPart], shapeBuf[xPtr+slopePart:])
		applySineWindowFLP(xWindowed[slopePart+flatPart:], shapeBuf[xPtr+slopePart+flatPart:], 2, slopePart)
		xPtr += subfrLength

		autocorrelationFLP(autoCorr, xWindowed, shapeWinLength, order+1)
		autoCorr[0] += autoCorr[0]*shapeWhiteNoiseFraction + 1.0

		nrg := schurFLP(rc, autoCorr, order)
		arSub := make([]float32, order)
		k2aFLP(arSub, rc, order)
		sr.gains[k] = float32(math.Sqrt(float64(nrg)))
		bwexpanderFLP(arSub, order, bwExp)
		limitCoefsFLP(arSub, 3.999, order)
		for j := range order {
			//nolint:gosec // G115: bandwidth-expanded, limited shaping coefs fit int16.
			sr.arQ13[k*maxShapeLPCOrder+j] = int16(f2iQ(arSub[j] * 8192.0))
		}
	}

	// Gain tweaking: higher gains during low speech activity.
	gainMult := float32(math.Pow(2.0, float64(-0.16*snrAdjDB)))
	gainAdd := float32(math.Pow(2.0, 0.16*minQGainDB))
	for k := range nbSubfr {
		sr.gains[k] = sr.gains[k]*gainMult + gainAdd
	}

	// Low-frequency shaping and spectral tilt.
	lfStrength := lowFreqShapingConst * (1.0 + lowQualityLFShapingDecr*(float32(qualityBands[0])*(1.0/32768.0)-1.0))
	lfStrength *= speechAct
	var tilt float32
	lfMA := make([]float32, nbSubfr)
	lfAR := make([]float32, nbSubfr)
	if signalType == frameSignalTypeVoiced {
		for k := range nbSubfr {
			bb := 0.2/float32(fsKHz) + 3.0/float32(pitchL[k])
			lfMA[k] = -1.0 + bb
			lfAR[k] = 1.0 - bb - bb*lfStrength
		}
		tilt = -hpNoiseCoefConst - (1.0-hpNoiseCoefConst)*harmHPNoiseCoefConst*speechAct
	} else {
		bb := 1.3 / float32(fsKHz)
		lfMA[0] = -1.0 + bb
		lfAR[0] = 1.0 - bb - bb*lfStrength*0.6
		for k := 1; k < nbSubfr; k++ {
			lfMA[k] = lfMA[0]
			lfAR[k] = lfAR[0]
		}
		tilt = -hpNoiseCoefConst
	}

	// Harmonic shaping (voiced).
	var harmShapeGain float32
	if signalType == frameSignalTypeVoiced {
		harmShapeGain = harmonicShapingConst
		harmShapeGain += highRateHarmonicShaping * (1.0 - (1.0-sr.codingQuality)*sr.inputQuality)
		harmShapeGain *= float32(math.Sqrt(float64(e.ltpCorr)))
	}

	// Smooth over subframes.
	for k := range nbSubfr {
		e.harmShapeGainSmth += subframeSmoothCoef * (harmShapeGain - e.harmShapeGainSmth)
		e.tiltSmth += subframeSmoothCoef * (tilt - e.tiltSmth)
		sr.harmShapeQ14[k] = f2iQ(e.harmShapeGainSmth * 16384.0)
		sr.tiltQ14[k] = f2iQ(e.tiltSmth * 16384.0)
		sr.lfShpQ14[k] = (f2iQ(lfAR[k]*16384.0) << 16) | int32(uint16(int16(f2iQ(lfMA[k]*16384.0)))) //nolint:gosec
	}

	return sr
}

// processGains reduces gains for high LTP gain, soft-limits them against the
// residual energy, quantizes them, and computes Lambda (silk_process_gains_FLP).
func (e *Encoder) processGains(
	sr *shapeResult,
	resNrg []float32,
	signalType frameSignalType,
	ltpredCodGain float32,
	snrDBQ7 int32,
	speechActQ8 int,
	inputTiltQ15 int32,
	subfrLength, nbSubfr int,
	conditional bool,
) (gainsQ16Int []int32, gainIndices []int8, lambdaQ10 int32, quantOffset frameQuantizationOffsetType) {
	quantOffset = sr.quantOffset

	// Gain reduction when LTP coding gain is high.
	if signalType == frameSignalTypeVoiced {
		s := 1.0 - 0.5*sigmoid(0.25*(ltpredCodGain-12.0))
		for k := range nbSubfr {
			sr.gains[k] *= s
		}
	}

	// Soft limit on the ratio of residual energy to squared gains.
	invMaxSqrVal := float32(math.Pow(2.0, float64(0.33*(21.0-float32(snrDBQ7)*(1.0/128.0))))) / float32(subfrLength)
	gainsTargetQ16 := make([]int32, nbSubfr)
	for k := range nbSubfr {
		gain := sr.gains[k]
		gain = float32(math.Sqrt(float64(gain*gain + resNrg[k]*invMaxSqrVal)))
		sr.gains[k] = float32(math.Min(float64(gain), 32767.0))
		gainsTargetQ16[k] = int32(sr.gains[k] * 65536.0)
	}

	// Quantize.
	gainIndices, gainsFloat, gainsQ16Int := quantizeGains(gainsTargetQ16, &e.previousLogGain, nbSubfr, conditional)
	for k := range nbSubfr {
		sr.gains[k] = gainsFloat[k] * (1.0 / 65536.0)
	}

	// Quantizer offset for voiced.
	if signalType == frameSignalTypeVoiced {
		if ltpredCodGain+float32(inputTiltQ15)*(1.0/32768.0) > 1.0 {
			quantOffset = frameQuantizationOffsetTypeLow
		} else {
			quantOffset = frameQuantizationOffsetTypeHigh
		}
	}

	// Rate/distortion tradeoff.
	offsetIndex := int(quantOffset) - int(frameQuantizationOffsetTypeLow)
	signalIndex := int(signalType) - int(frameSignalTypeInactive)
	quantOffsetVal := float32(silkSignQuantOffsetQ10(signalIndex, offsetIndex)) * (1.0 / 1024.0)
	lambda := float32(lambdaOffset) +
		lambdaDelayedDecisions*nStatesDelayedDecision +
		lambdaSpeechAct*float32(speechActQ8)*(1.0/256.0) +
		lambdaInputQuality*sr.inputQuality +
		lambdaCodingQuality*sr.codingQuality +
		lambdaQuantOffsetConst*quantOffsetVal
	lambdaQ10 = f2iQ(lambda * 1024.0)

	return gainsQ16Int, gainIndices, lambdaQ10, quantOffset
}

// silkSignQuantOffsetQ10 returns the Q10 quantization offset for the signal and
// offset type (silk_Quantization_Offsets_Q10), indexed as [signalType>>1][offset].
func silkSignQuantOffsetQ10(signalIndex, offsetIndex int) int32 {
	sig := 0
	if signalIndex == 2 { // voiced
		sig = 1
	}

	return quantizationOffsetsQ10[sig][offsetIndex]
}
