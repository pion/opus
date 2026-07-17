// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// This file assembles a full SILK frame: analysis, quantization, the NSQ, and
// range coding of every field in decode order. It encodes mono, 20 ms,
// SILK-only frames with voiced/LTP prediction, faithful noise shaping and NLSF
// interpolation. The delayed-decision NSQ, stereo, and the rate-control loop
// are follow-up refinements.

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
func (e *Encoder) Encode(input []int16, bandwidth Bandwidth, targetBitrate int) []byte {
	if targetBitrate > 0 {
		e.targetBitrate = targetBitrate
	}
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
	saQ8, tiltQ15, quality := e.vad.getSpeechActivityQ8(input, frameLength, fsKHz)
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
	voiced, pitchL, lagIndex, contourIndex, res, predGain := e.findPitchLags(
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

	// Noise-shaping analysis: AR shaping filters, initial gains, spectral tilt,
	// low-frequency and harmonic shaping.
	snrDBQ7 := controlSNR(fsKHz, subfrCount, e.targetBitrate)
	laShape := laShapeMSLowComplex * fsKHz
	shapeBuf := make([]float32, laShape+frameLength+laShape)
	copy(shapeBuf, e.xBuf[ltpMemLength-laShape:ltpMemLength])
	for i := range frameLength {
		shapeBuf[laShape+i] = float32(input[i])
	}
	sr := e.noiseShapeAnalysis(shapeBuf, signalType, pitchL, predGain, snrDBQ7, saQ8, quality, fsKHz, subfrCount, subfrLength)

	// Prediction coefficients (find_pred_coefs). Build LPC_in_pre: the LTP
	// residual for voiced, or the gain-normalized input for unvoiced. Both drive
	// the short-term LPC and the residual energy.
	invGains := make([]float32, subfrCount)
	for k := range subfrCount {
		invGains[k] = 1.0 / sr.gains[k]
	}
	ltpCoefQ14 := make([]int16, ltpOrder*subfrCount)
	nsqPitchL := make([]int, subfrCount)
	lpcInPre := make([]float32, subfrCount*(order+subfrLength))
	xBase := ltpMemLength - order
	var periodicityIndex int
	var filterIndices []int8
	var predGainDB float32
	ltpScaleIndex := 0
	ltpScaleQ14 := int32(silkLTPScaleQ14)
	if voiced {
		xxLTP := make([]float32, subfrCount*ltpMatrixSize)
		xXLTP := make([]float32, subfrCount*ltpOrder)
		findLTPFLP(xxLTP, xXLTP, res, ltpMemLength, pitchL, subfrLength, subfrCount)
		ltpCoefQ14, filterIndices, periodicityIndex, predGainDB = e.quantLTPGains(xxLTP, xXLTP, subfrLength, subfrCount)
		copy(nsqPitchL, pitchL)
		ltpScaleIndex, ltpScaleQ14 = ltpScaleControl(predGainDB, snrDBQ7, e.packetLossPerc, 1, false)

		ltpCoefFloat := make([]float32, ltpOrder*subfrCount)
		for i := range ltpCoefFloat {
			ltpCoefFloat[i] = float32(ltpCoefQ14[i]) * (1.0 / 16384.0)
		}
		ltpAnalysisFilterFLP(lpcInPre, analysis, xBase, ltpCoefFloat, pitchL, invGains, subfrLength, subfrCount, order)
	} else {
		e.sumLogGainQ7 = 0
		for k := range subfrCount {
			dst := k * (order + subfrLength)
			src := xBase + k*subfrLength
			for i := range order + subfrLength {
				lpcInPre[dst+i] = analysis[src+i] * invGains[k]
			}
		}
	}

	// Short-term prediction: Burg over LPC_in_pre, search the NLSF interpolation
	// factor, then quantize and build both frame-half LPC sets.
	minInvGain := predCoefsMinInvGain(e.firstFrameAfterReset, predGainDB, sr.codingQuality)
	nlsfInterpQ2, nlsf := e.findLPCNLSF(lpcInPre, minInvGain, bandwidth, order, subfrCount, subfrLength)
	stabilizeNLSF(nlsf, order, bandwidth)
	index1, indices2, quantNLSF := quantizeNLSF(nlsf, bandwidth)
	predCoefQ12 := nlsfToLPCQ12(quantNLSF, bandwidth) // second frame half
	predCoefQ12Half0 := predCoefQ12
	if nlsfInterpQ2 < 4 {
		nlsf0 := make([]int16, order)
		interpolateNLSF(nlsf0, e.prevNLSFq, quantNLSF, nlsfInterpQ2, order)
		predCoefQ12Half0 = nlsfToLPCQ12(nlsf0, bandwidth) // interpolated first half
	}
	predCoef2 := make([]int16, 2*maxLPCOrder)
	copy(predCoef2, predCoefQ12Half0)
	copy(predCoef2[maxLPCOrder:], predCoefQ12)
	copy(e.prevNLSFq, quantNLSF)

	// Residual energy per subframe from the quantized LPC (gain soft-limit).
	predCoefFloat0 := make([]float32, order)
	predCoefFloat1 := make([]float32, order)
	for j := range order {
		predCoefFloat0[j] = float32(predCoefQ12Half0[j]) * (1.0 / 4096.0)
		predCoefFloat1[j] = float32(predCoefQ12[j]) * (1.0 / 4096.0)
	}
	resNrg := make([]float32, subfrCount)
	residualEnergyFLP(resNrg, lpcInPre, predCoefFloat0, predCoefFloat1, sr.gains, subfrLength, subfrCount, order)

	// Process gains: reduce for high LTP gain, soft-limit, quantize; Lambda + offset.
	gainsQ16Int, gainIndices, lambdaQ10, quantOffsetType := e.processGains(
		sr, resNrg, signalType, predGainDB, snrDBQ7, saQ8, tiltQ15, subfrLength, subfrCount, false)

	// Noise-shaping quantization.
	pulses := make([]int8, frameLength)
	seed := uint32(e.frameCounter & 3) //nolint:gosec // G115
	e.nsq.quantize(input, pulses, &nsqParams{
		predCoefQ12:      predCoef2,
		ltpCoefQ14:       ltpCoefQ14,
		arQ13:            sr.arQ13,
		harmShapeGainQ14: sr.harmShapeQ14,
		tiltQ14:          sr.tiltQ14,
		lfShpQ14:         sr.lfShpQ14,
		gainsQ16:         gainsQ16Int,
		pitchL:           nsqPitchL,
		lambdaQ10:        lambdaQ10,
		ltpScaleQ14:      ltpScaleQ14,
		seed:             int32(seed), //nolint:gosec // G115
		signalType:       signalType,
		quantOffsetType:  quantOffsetType,
		nlsfInterpCoefQ2: nlsfInterpQ2,
		ltpMemLength:     ltpMemLength,
		frameLength:      frameLength,
		subfrLength:      subfrLength,
		nbSubfr:          subfrCount,
		predictLPCOrder:  order,
		shapingLPCOrder:  shapeLPCOrderLowComplex,
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
	e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex, uint32(nlsfInterpQ2)) //nolint:gosec // G115
	if voiced {
		e.encodePitchLags(int(lagIndex)+peMinLagMS*fsKHz, uint32(contourIndex), bandwidth, nanoseconds20Ms, true) //nolint:gosec
		e.encodeLTPFilter(uint32(periodicityIndex), toUint32(filterIndices))                                      //nolint:gosec
		e.encodeLTPScaling(uint32(ltpScaleIndex))                                                                 //nolint:gosec // G115
	}
	e.rangeEncoder.EncodeSymbolWithICDF(icdfLinearCongruentialGeneratorSeed, seed)
	e.encodePulses(signalType, quantOffsetType, pulses, frameLength)

	// Carry state to the next frame.
	copy(e.xBuf, analysis[frameLength:frameLength+ltpMemLength])
	e.isPreviousFrameVoiced = voiced
	e.firstFrameAfterReset = false
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
