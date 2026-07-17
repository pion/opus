// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// This file assembles a full SILK frame: analysis, quantization, the NSQ, and
// range coding of every field in decode order. This first version encodes
// mono, 20 ms, SILK-only frames on the unvoiced path (no long-term prediction)
// with simplified noise shaping. Voiced/LTP coding, the delayed-decision NSQ,
// stereo, NLSF interpolation, and rate control are follow-up refinements.

const (
	silkGainFloor    = 1.0
	silkGainCeil     = 32767.0
	silkVADThreshold = 100 // speech_activity_Q8 above which a frame is treated as active
	silkLambdaQ10    = 1024
	silkLTPScaleQ14  = 15565
	burgMinInvGain   = 1e-4 // 1 / MAX_PREDICTION_POWER_GAIN
)

// silkInternalRate returns the SILK internal sample rate in kHz.
func silkInternalRate(bandwidth Bandwidth) int {
	switch bandwidth {
	case BandwidthNarrowband:
		return 8
	case BandwidthMediumband:
		return 12
	default:
		return 16
	}
}

// silkLPCOrder returns the prediction LPC order for the bandwidth.
func silkLPCOrder(bandwidth Bandwidth) int {
	if bandwidth == BandwidthWideband {
		return 16
	}

	return 10
}

// nlsfToLPCQ12 reconstructs Q12 LPC coefficients from quantized NLSFs, reusing
// the decoder's NLSF->LPC path so the encoder predicts with the exact
// coefficients the decoder will use.
func nlsfToLPCQ12(nlsfQ15 []int16, bandwidth Bandwidth) []int16 {
	d := NewDecoder()
	a32Q17 := d.convertNormalizedLSFsToLPCCoefficients(nlsfQ15, bandwidth)
	d.limitLPCCoefficientsRange(a32Q17)
	d.limitLPCFilterPredictionGainInto(a32Q17, 0)

	out := make([]int16, len(d.aQ12Int[0]))
	copy(out, d.aQ12Int[0])

	return out
}

// Encode encodes one 20 ms mono SILK frame from internal-rate PCM and returns
// the range-coded SILK payload (the SILK header plus frame, without the Opus
// TOC byte).
func (e *Encoder) Encode(input []int16, bandwidth Bandwidth) []byte {
	e.rangeEncoder.Init()
	e.encodeSILKFrame(input, bandwidth)

	return e.rangeEncoder.Done()
}

// encodeSILKFrame encodes one 20 ms mono SILK frame to the range encoder.
// input is PCM at the internal rate for the bandwidth.
//
//nolint:gocyclo,cyclop // the frame encoder threads many stages in decode order.
func (e *Encoder) encodeSILKFrame(input []int16, bandwidth Bandwidth) {
	fsKHz := silkInternalRate(bandwidth)
	order := silkLPCOrder(bandwidth)
	subfrCount := subframeCount(nanoseconds20Ms)
	subfrLength := 5 * fsKHz
	frameLength := subfrCount * subfrLength
	ltpMemLength := 20 * fsKHz

	// Voice activity.
	saQ8, tiltQ15, _ := e.vad.getSpeechActivityQ8(input, frameLength, fsKHz)
	active := saQ8 > silkVADThreshold

	// Pitch analysis on the whitening residual (with LTP-memory history).
	if len(e.xBuf) != ltpMemLength {
		e.xBuf = make([]float32, ltpMemLength)
	}
	analysis := make([]float32, ltpMemLength+frameLength+ltpOrder)
	copy(analysis, e.xBuf)
	for i := range frameLength {
		analysis[ltpMemLength+i] = float32(input[i])
	}
	voiced, pitchL, lagIndex, contourIndex, res := e.findPitchLags(
		analysis[:ltpMemLength+frameLength], fsKHz, subfrCount, saQ8, tiltQ15)
	// Keep a few zero samples of headroom after the residual for find_LTP.
	res = append(res, make([]float32, ltpOrder)...)

	signalType := frameSignalTypeInactive
	switch {
	case voiced:
		signalType = frameSignalTypeVoiced
		active = true
	case active:
		signalType = frameSignalTypeUnvoiced
	}
	quantOffsetType := frameQuantizationOffsetTypeLow

	// Short-term prediction: Burg -> NLSF -> quantize -> quantized LPC (Q12).
	xf := make([]float32, frameLength)
	for i := range xf {
		xf[i] = float32(input[i])
	}
	aFloat := make([]float32, order)
	burgModifiedFLP(aFloat, xf, burgMinInvGain, frameLength, 1, order)
	nlsf := make([]int16, order)
	a2nlsfFLP(nlsf, aFloat, order)
	stabilizeNLSF(nlsf, order, bandwidth)
	index1, indices2, quantNLSF := quantizeNLSF(nlsf, bandwidth)
	predCoefQ12 := nlsfToLPCQ12(quantNLSF, bandwidth)
	predCoef2 := make([]int16, 2*maxLPCOrder)
	copy(predCoef2, predCoefQ12)
	copy(predCoef2[maxLPCOrder:], predCoefQ12)

	// Long-term prediction (voiced only).
	ltpCoefQ14 := make([]int16, ltpOrder*subfrCount)
	nsqPitchL := make([]int, subfrCount)
	var periodicityIndex int
	var filterIndices []int8
	var predGainDB float32
	if voiced {
		xxLTP := make([]float32, subfrCount*ltpMatrixSize)
		xXLTP := make([]float32, subfrCount*ltpOrder)
		findLTPFLP(xxLTP, xXLTP, res, ltpMemLength, pitchL, subfrLength, subfrCount)
		ltpCoefQ14, filterIndices, periodicityIndex, predGainDB = e.quantLTPGains(xxLTP, xXLTP, subfrLength, subfrCount)
		copy(nsqPitchL, pitchL)
	}

	// Gains from the LPC residual energy, reduced when the LTP gain is high.
	resFixed := make([]int16, frameLength)
	lpcAnalysisFilterFixed(resFixed, input, predCoefQ12, frameLength, order)
	gainScale := float64(1)
	if voiced {
		gainScale = 1.0 - 0.5*float64(sigmoid(0.25*(predGainDB-12.0)))
	}
	gainsTargetQ16 := make([]int32, subfrCount)
	for k := range subfrCount {
		var nrg float64
		for i := range subfrLength {
			v := float64(resFixed[k*subfrLength+i])
			nrg += v * v
		}
		gain := math.Sqrt(nrg) * gainScale
		gain = math.Max(silkGainFloor, math.Min(gain, silkGainCeil))
		gainsTargetQ16[k] = int32(gain * 65536)
	}
	gainIndices, _, gainsQ16Int := e.quantizeGains(gainsTargetQ16, subfrCount, false)

	// Noise-shaping quantization.
	pulses := make([]int8, frameLength)
	seed := uint32(e.frameCounter & 3) //nolint:gosec // G115
	e.nsq.quantize(input, pulses, &nsqParams{
		predCoefQ12:      predCoef2,
		ltpCoefQ14:       ltpCoefQ14,
		arQ13:            make([]int16, subfrCount*maxShapeLPCOrder),
		harmShapeGainQ14: make([]int32, subfrCount),
		tiltQ14:          make([]int32, subfrCount),
		lfShpQ14:         make([]int32, subfrCount),
		gainsQ16:         gainsQ16Int,
		pitchL:           nsqPitchL,
		lambdaQ10:        silkLambdaQ10,
		ltpScaleQ14:      silkLTPScaleQ14,
		seed:             int32(seed), //nolint:gosec // G115
		signalType:       signalType,
		quantOffsetType:  quantOffsetType,
		nlsfInterpCoefQ2: 4,
		ltpMemLength:     ltpMemLength,
		frameLength:      frameLength,
		subfrLength:      subfrLength,
		nbSubfr:          subfrCount,
		predictLPCOrder:  order,
		shapingLPCOrder:  order,
	})
	e.frameCounter++

	// Emit every field in the order the decoder reads it.
	vadBit := uint32(0)
	if active {
		vadBit = 1
	}
	e.rangeEncoder.EncodeSymbolLogP(1, vadBit)
	e.rangeEncoder.EncodeSymbolLogP(1, 0) // LBRR flag (no redundancy)

	e.emitFrameType(signalType, quantOffsetType, active)
	e.emitGainIndices(gainIndices, signalType, false)
	e.emitNLSFIndices(index1, indices2, bandwidth, voiced)
	e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex, 4) // no interpolation
	if voiced {
		e.encodePitchLags(int(lagIndex)+peMinLagMS*fsKHz, uint32(contourIndex), bandwidth, nanoseconds20Ms, true) //nolint:gosec
		e.encodeLTPFilter(uint32(periodicityIndex), toUint32(filterIndices))                                      //nolint:gosec
		e.encodeLTPScaling(0)                                                                                     // LTP_scale index 0 (15565)
	}
	e.rangeEncoder.EncodeSymbolWithICDF(icdfLinearCongruentialGeneratorSeed, seed)
	e.encodePulses(signalType, quantOffsetType, pulses, frameLength)

	// Carry state to the next frame.
	copy(e.xBuf, analysis[frameLength:frameLength+ltpMemLength])
	e.isPreviousFrameVoiced = voiced
}

// toUint32 converts codebook indices to the type the emitters expect.
func toUint32(indices []int8) []uint32 {
	out := make([]uint32, len(indices))
	for i, v := range indices {
		out[i] = uint32(v) //nolint:gosec // G115: indices are small non-negative.
	}

	return out
}

// emitFrameType codes the signal type and quantization offset (RFC 6716
// Section 4.2.7.3).
func (e *Encoder) emitFrameType(signalType frameSignalType, quantOffsetType frameQuantizationOffsetType, vad bool) {
	high := quantOffsetType == frameQuantizationOffsetTypeHigh
	if !vad {
		sym := uint32(0)
		if high {
			sym = 1
		}
		e.rangeEncoder.EncodeSymbolWithICDF(icdfFrameTypeVADInactive, sym)

		return
	}

	var sym uint32
	switch {
	case signalType == frameSignalTypeUnvoiced && high:
		sym = 1
	case signalType == frameSignalTypeVoiced && !high:
		sym = 2
	case signalType == frameSignalTypeVoiced && high:
		sym = 3
	}
	e.rangeEncoder.EncodeSymbolWithICDF(icdfFrameTypeVADActive, sym)
}
