// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// This file assembles a full SILK frame: analysis, quantization, the NSQ, and
// range coding of every field in decode order. It encodes mono, 20 ms,
// SILK-only frames with voiced/LTP prediction, faithful noise shaping and NLSF
// interpolation. The delayed-decision NSQ, stereo, and the rate-control loop
// are follow-up refinements.

// silkVADThreshold is the current Pion speech_activity_Q8 cutoff. Its value
// 100 is a legacy encoder heuristic, not libopus's Q8 threshold 13. Pitch
// analysis may still promote a periodic unit to active below it.
const silkVADThreshold = 100

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

// Encode returns one mono 20, 40, or 60 ms SILK payload without the Opus TOC.
// Multi-unit packets share one range stream and preserve prediction state
// across units and calls. Invalid unit counts return nil before state changes.
func (e *Encoder) Encode(input []int16, bandwidth Bandwidth, targetBitrate int) []byte {
	unitSamples := silkUnitSamples(bandwidth)
	if len(input) < unitSamples || len(input)%unitSamples != 0 {
		return nil
	}
	frameCount := len(input) / unitSamples
	if frameCount > len(e.vadFlags) {
		return nil
	}
	if targetBitrate > 0 {
		e.targetBitrate = targetBitrate
	}
	e.vadFlags = [3]bool{}
	e.rangeEncoder.Init()
	e.encodeSILKPacketHeader(frameCount)
	for i := range frameCount {
		e.encodeSILKFrame(input[i*unitSamples:(i+1)*unitSamples], i, frameCount, bandwidth)
	}
	// The VAD flags are only final after every unit's analysis has run.
	// libopus writes them back into the reserved header interval with
	// ec_enc_patch_initial_bits (enc_API.c); the decoder reads the same
	// bits before the unit parameters.
	var vadFlags uint32
	for i, active := range e.vadFlags[:frameCount] {
		if active {
			vadFlags |= 1 << uint(frameCount-1-i) //nolint:gosec // G115: frameCount is 1..3.
		}
	}
	if !e.rangeEncoder.PatchInitialBits(vadFlags, uint(frameCount)) { //nolint:gosec // G115: frameCount is 1..3.
		return nil
	}

	return e.rangeEncoder.Done()
}

// encodeSILKPacketHeader reserves the SILK header interval that precedes the
// frames of a packet: one VAD bit per SILK frame (RFC 6716 Section 4.2.3)
// plus a single LBRR-present bit (Section 4.2.4). The true VAD flags are known
// only after every unit's analysis, so zero placeholders are patched before
// finalizing. The encoder has no low-bitrate redundancy, so that bit stays zero.
func (e *Encoder) encodeSILKPacketHeader(frameCount int) {
	for range frameCount {
		e.rangeEncoder.EncodeCumulative(0, 1, 2) // VAD flag interval, reserved
	}
	e.rangeEncoder.EncodeCumulative(0, 1, 2) // LBRR-present: no low-bitrate redundancy
}

// silkUnitSamples returns the number of PCM samples in one 20 ms SILK coding
// unit at the given bandwidth's internal rate.  A 20 ms unit holds 4 subframes
// of 5 ms each, so the count is 20 * fsKHz (e.g. 320 for 16 kHz WB).
func silkUnitSamples(bandwidth Bandwidth) int {
	return 20 * silkInternalRate(bandwidth)
}

// encodeSILKFrame encodes one 20 ms, four-subframe SILK coding unit. Unit zero
// uses independent gain and pitch coding; later units use conditional coding.
// Multi-unit packets share range and prediction state while each analysis
// stage consumes one unit, matching libopus's silk_encode_do_VAD_FIX and
// encode_frame_FIX.c flow (RFC 6716 Sections 4.2.7.4 and 4.2.7.6.1).
//
//nolint:gocyclo,cyclop // the frame encoder threads many stages in decode order.
func (e *Encoder) encodeSILKFrame(
	input []int16,
	unit int,
	frameCount int,
	bandwidth Bandwidth,
) {
	independent := unit == 0
	fsKHz := silkInternalRate(bandwidth)
	order := silkLPCOrder(bandwidth)
	subfrCount := subframeCount(nanoseconds20Ms)
	subfrLength := 5 * fsKHz
	frameLength := subfrCount * subfrLength
	ltpMemLength := 20 * fsKHz

	// Voice activity. Pitch analysis below may promote a periodic frame to
	// active, so the packet-header flag is recorded only after signal type is
	// finalized.
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
	isVoiced := signalType == frameSignalTypeVoiced
	e.vadFlags[unit] = active

	// Noise-shaping analysis: AR shaping filters, initial gains, spectral tilt,
	// low-frequency and harmonic shaping.
	snrDBQ7 := controlSNR(fsKHz, subfrCount, e.targetBitrate)
	laShape := laShapeMSLowComplex * fsKHz
	shapeBuf := make([]float32, laShape+frameLength+laShape)
	copy(shapeBuf, e.xBuf[ltpMemLength-laShape:ltpMemLength])
	for i := range frameLength {
		shapeBuf[laShape+i] = float32(input[i])
	}
	// pitchRes is the current frame's portion of findPitchLags's whitened
	// residual (res_pitch_frame in libopus) — noiseShapeAnalysis's unvoiced
	// sparseness measure needs this, not the raw signal in shapeBuf.
	pitchRes := res[ltpMemLength : ltpMemLength+frameLength]
	sr := e.noiseShapeAnalysis(
		shapeBuf, pitchRes, signalType, pitchL, predGain, snrDBQ7, saQ8, quality, fsKHz, subfrCount, subfrLength)

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
	ltpScaleQ14 := silkLTPScaleQ14
	if isVoiced {
		xxLTP := make([]float32, subfrCount*ltpMatrixSize)
		xXLTP := make([]float32, subfrCount*ltpOrder)
		findLTPFLP(xxLTP, xXLTP, res, ltpMemLength, pitchL, subfrLength, subfrCount)
		ltpCoefQ14, filterIndices, periodicityIndex, predGainDB = e.quantLTPGains(xxLTP, xXLTP, subfrLength, subfrCount)
		copy(nsqPitchL, pitchL)
		ltpScaleIndex, ltpScaleQ14 = ltpScaleForFrame(
			predGainDB, snrDBQ7, e.packetLossPerc, frameCount, independent,
		)

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
		sr, resNrg, signalType, predGainDB, snrDBQ7, saQ8, tiltQ15, subfrLength, subfrCount, !independent)

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

	// Emit every field in the order the decoder reads it. The per-frame VAD
	// and LBRR header bits were already emitted by encodeSILKPacketHeader;
	// the decoder then reads the frame type with the VAD table selected by
	// the header flag, so the unit's active flag stays consistent with the
	// table the decoder will use.
	e.emitFrameType(signalType, quantOffsetType, active)
	e.emitGainIndices(gainIndices, signalType, !independent)
	e.emitNLSFIndices(index1, indices2, bandwidth, isVoiced)
	e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex, uint32(nlsfInterpQ2)) //nolint:gosec // G115
	if isVoiced {
		primaryLag := int(lagIndex) + peMinLagMS*fsKHz
		contour := uint32(contourIndex)    //nolint:gosec // G115: contour index is non-negative.
		period := uint32(periodicityIndex) //nolint:gosec // G115: periodicity index is non-negative.
		scale := uint32(ltpScaleIndex)     //nolint:gosec // G115: scale index is 0..2.
		e.encodePitchLags(primaryLag, contour, bandwidth, nanoseconds20Ms, independent)
		e.encodeLTPFilter(period, toUint32(filterIndices))
		if independent {
			e.encodeLTPScaling(scale)
		}
	}
	e.rangeEncoder.EncodeSymbolWithICDF(icdfLinearCongruentialGeneratorSeed, seed)
	e.encodePulses(signalType, quantOffsetType, pulses, frameLength)

	// Carry state to the next frame.
	copy(e.xBuf, analysis[frameLength:frameLength+ltpMemLength])
	e.isPreviousFrameVoiced = isVoiced
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
