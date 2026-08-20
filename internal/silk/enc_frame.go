// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// This file assembles a full SILK frame: analysis, quantization, the NSQ, and
// range coding of every field in decode order. It encodes mono, 20 ms,
// SILK-only frames with voiced/LTP prediction, faithful noise shaping and NLSF
// interpolation. The delayed-decision NSQ, stereo, and the rate-control loop
// are follow-up refinements.

const (
	// silkVADThreshold is the speech_activity_Q8 at or below which a SILK
	// coding unit is treated as VAD-inactive. It mirrors libopus's
	// SPEECH_ACTIVITY_DTX_THRES (Q8, fixed/tuning_parameters.h): the SILK
	// VAD flag and the frame type's active/inactive table both follow this
	// threshold (silk_encode_do_VAD_FIX, silk_encode_indices).
	silkVADThreshold = 100
	silkLTPScaleQ14  = 15565
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

// Encode encodes one mono SILK packet of 20, 40, or 60 ms from internal-rate
// PCM and returns the range-coded SILK payload (the SILK header plus the SILK
// frames, without the Opus TOC byte). Durations longer than 20 ms hold
// multiple 20 ms SILK coding units in one shared range stream, mirroring
// libopus's silk_Encode for a payload larger than one 20 ms coding unit
// (silk/enc_API.c): the per-packet VAD/LBRR header is emitted once for the
// whole packet, and every unit is coded with its own analysis and parameters
// while prediction state (pitch lag, NLSF interpolation, LCG seed) continues
// across units.
//
// Encode is stateful per stream: it keeps the SILK prediction state between
// calls (one encoder per stream, like the decoder) and re-initializes the
// range coder only for the new packet. Within a packet the range coder stays
// open across every 20 ms coding unit — libopus runs one range stream for the
// whole SILK packet (ec_enc_init once, ec_enc_done once) and only the
// per-unit analysis/parameters are recomputed — so a multi-unit packet is one
// continuous bitstream, not a concatenation of per-unit streams. Resetting
// the prediction state per packet (Reset) is what a fresh stream does; the
// range coder must not be re-initialized between units or the decoder's
// single continuous stream desynchronizes after the first unit.
func (e *Encoder) Encode(input []int16, bandwidth Bandwidth, targetBitrate int) []byte {
	frameCount := silkFrameCount(frameDurationNanoseconds(len(input), bandwidth))
	if targetBitrate > 0 {
		e.targetBitrate = targetBitrate
	}
	e.vadFlags = [3]bool{}
	e.rangeEncoder.Init()
	e.encodeSILKPacketHeader(frameCount)
	unitSamples := silkUnitSamples(bandwidth)
	for i := range frameCount {
		e.encodeSILKFrame(input[i*unitSamples:(i+1)*unitSamples], i, bandwidth, i == 0)
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
	e.rangeEncoder.PatchInitialBits(vadFlags, uint(frameCount)) //nolint:gosec // G115: frameCount is 1..3.

	return e.rangeEncoder.Done()
}

// encodeSILKPacketHeader reserves the SILK header interval that precedes the
// frames of a packet: one VAD bit per SILK frame (RFC 6716 Section 4.2.3)
// plus a single LBRR-present bit (Section 4.2.4). Each VAD bit is reserved as
// the active branch of a probability-1/2 symbol (the inactive branch is the
// patchable value); the true VAD flags are known only after every unit's
// analysis, so Encode patches them back in before finalizing. The encoder has
// no low-bitrate redundancy, so the LBRR-present bit stays zero.
func (e *Encoder) encodeSILKPacketHeader(frameCount int) {
	for range frameCount {
		e.rangeEncoder.EncodeCumulative(1, 2, 2) // VAD flag interval, reserved
	}
	e.rangeEncoder.EncodeCumulative(0, 1, 2) // LBRR-present: no low-bitrate redundancy
}

// silkUnitSamples returns the number of PCM samples in one 20 ms SILK coding
// unit at the given bandwidth's internal rate.  A 20 ms unit holds 4 subframes
// of 5 ms each, so the count is 20 * fsKHz (e.g. 320 for 16 kHz WB).
func silkUnitSamples(bandwidth Bandwidth) int {
	return 20 * silkInternalRate(bandwidth)
}

// frameDurationNanoseconds derives the packet duration from the PCM length so
// Encode can reject lengths that are not a whole number of 20 ms coding units.
func frameDurationNanoseconds(sampleCount int, bandwidth Bandwidth) int {
	unit := silkUnitSamples(bandwidth)
	if unit == 0 || sampleCount%unit != 0 {
		return 0
	}

	return (sampleCount / unit) * nanoseconds20Ms
}

// encodeSILKFrame encodes exactly one 20 ms SILK coding unit to the range
// encoder. input is 4*fsKHz PCM samples (5 ms * 4 subframes) at the internal
// rate for the bandwidth; unit is the unit's index within the current packet
// (0-based), used to record that unit's VAD flag; isFirstSilkFrameInOpusFrame
// selects independent gain and absolute pitch-lag coding for the first unit
// of the packet, as the decoder expects (RFC 6716 Sections 4.2.7.4,
// 4.2.7.6.1).
//
// Every analysis stage is sized for one 20 ms unit, mirroring libopus 1.3.1,
// where the SILK state's frame_length is always 20*fs_kHz (silk_setup_fs,
// control_codec.c) and a 40/60 ms API frame loops per unit: VAD runs on the
// unit (silk_encode_do_VAD_FIX passes inputBuf to silk_VAD_GetSA_Q8), the
// VAD band spans are derived from decimated frame_length/8 (VAD.c), and
// control_SNR, find_pitch_lags, find_LPC, and the NSQ all consume one
// frame_length of 4 subframes (encode_frame_FIX.c). A 60 ms packet is
// therefore three such units in one shared range stream, not one 12-subframe
// unit, and no stage below may assume the input spans more than 20 ms.
//
//nolint:gocyclo,cyclop // the frame encoder threads many stages in decode order.
func (e *Encoder) encodeSILKFrame(input []int16, unit int, bandwidth Bandwidth, isFirstSilkFrameInOpusFrame bool) {
	fsKHz := silkInternalRate(bandwidth)
	order := silkLPCOrder(bandwidth)
	subfrCount := subframeCount(nanoseconds20Ms)
	subfrLength := 5 * fsKHz
	frameLength := subfrCount * subfrLength
	if len(input) != frameLength {
		return
	}
	ltpMemLength := 20 * fsKHz

	// Voice activity. The unit's VAD flag is the threshold decision (the same
	// rule that sets the frame type's active/inactive table); the header
	// interval is patched from it in Encode.
	saQ8, tiltQ15, quality := e.vad.getSpeechActivityQ8(input, frameLength, fsKHz)
	active := saQ8 > silkVADThreshold
	e.vadFlags[unit] = active

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
		sr, resNrg, signalType, predGainDB, snrDBQ7, saQ8, tiltQ15, subfrLength, subfrCount, !isFirstSilkFrameInOpusFrame)

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
	e.emitGainIndices(gainIndices, signalType, !isFirstSilkFrameInOpusFrame)
	e.emitNLSFIndices(index1, indices2, bandwidth, voiced)
	e.rangeEncoder.EncodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex, uint32(nlsfInterpQ2)) //nolint:gosec // G115
	if voiced {
		primaryLag := int(lagIndex) + peMinLagMS*fsKHz
		contour := uint32(contourIndex)    //nolint:gosec // G115: contour index is non-negative.
		period := uint32(periodicityIndex) //nolint:gosec // G115: periodicity index is non-negative.
		scale := uint32(ltpScaleIndex)     //nolint:gosec // G115: scale index is 0..2.
		e.encodePitchLags(primaryLag, contour, bandwidth, nanoseconds20Ms, isFirstSilkFrameInOpusFrame)
		e.encodeLTPFilter(period, toUint32(filterIndices))
		if isFirstSilkFrameInOpusFrame {
			e.encodeLTPScaling(scale)
		}
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
