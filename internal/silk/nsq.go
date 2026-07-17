// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// Noise-shaping quantizer (silk_NSQ_c, NSQ.c). It applies short- and long-term
// prediction plus noise shaping to the scaled input and quantizes the
// excitation to integer pulses. This is the non-delayed-decision variant;
// NSQ_del_dec is a later quality refinement.

const (
	maxFSKHz            = 16
	maxSubFrameLength   = 5 * maxFSKHz  // 80
	maxFrameLength      = 20 * maxFSKHz // 320
	nsqLPCBufLength     = maxLPCOrder   // 16
	harmShapeFIRTaps    = 3
	quantLevelAdjustQ10 = 80
)

// quantizationOffsetsQ10[signalType>>1][quantOffsetType] (silk_Quantization_Offsets_Q10).
//
//nolint:gochecknoglobals
var quantizationOffsetsQ10 = [2][2]int32{{100, 240}, {32, 100}}

// nsqState is the persistent noise-shaping quantizer state (silk_nsq_state).
type nsqState struct {
	xq            []int16 // quantized output with history, 2*maxFrameLength
	sLTPShpQ14    []int32 // 2*maxFrameLength
	sLPCQ14       []int32 // maxSubFrameLength + nsqLPCBufLength
	sAR2Q14       [maxShapeLPCOrder]int32
	sLFARShpQ14   int32
	sDiffShpQ14   int32
	lagPrev       int
	sLTPBufIdx    int
	sLTPShpBufIdx int
	randSeed      int32
	prevGainQ16   int32
	rewhiteFlag   int
}

func newNSQState() *nsqState {
	return &nsqState{
		xq:          make([]int16, 2*maxFrameLength),
		sLTPShpQ14:  make([]int32, 2*maxFrameLength),
		sLPCQ14:     make([]int32, maxSubFrameLength+nsqLPCBufLength),
		prevGainQ16: 65536,
	}
}

// nsqParams bundles the per-frame quantizer inputs from the encoder control.
type nsqParams struct {
	predCoefQ12      []int16 // 2*maxLPCOrder
	ltpCoefQ14       []int16 // ltpOrder*nbSubfr
	arQ13            []int16 // nbSubfr*maxShapeLPCOrder
	harmShapeGainQ14 []int32
	tiltQ14          []int32
	lfShpQ14         []int32
	gainsQ16         []int32
	pitchL           []int
	lambdaQ10        int32
	ltpScaleQ14      int32
	seed             int32
	signalType       frameSignalType
	quantOffsetType  frameQuantizationOffsetType
	nlsfInterpCoefQ2 int
	ltpMemLength     int
	frameLength      int
	subfrLength      int
	nbSubfr          int
	predictLPCOrder  int
	shapingLPCOrder  int
}

// quantize runs the NSQ over one frame, filling pulses with the quantized
// excitation indices.
func (nsq *nsqState) quantize(x16 []int16, pulses []int8, p *nsqParams) {
	nsq.randSeed = p.seed
	lag := nsq.lagPrev

	sigIndex := 0
	if p.signalType == frameSignalTypeVoiced {
		sigIndex = 1
	}
	offsetIndex := 0
	if p.quantOffsetType == frameQuantizationOffsetTypeHigh {
		offsetIndex = 1
	}
	offsetQ10 := quantizationOffsetsQ10[sigIndex][offsetIndex]

	lsfInterpFlag := 1
	if p.nlsfInterpCoefQ2 == 4 {
		lsfInterpFlag = 0
	}

	sLTPQ15 := make([]int32, p.ltpMemLength+p.frameLength)
	sLTP := make([]int16, p.ltpMemLength+p.frameLength)
	xScQ10 := make([]int32, p.subfrLength)

	nsq.sLTPShpBufIdx = p.ltpMemLength
	nsq.sLTPBufIdx = p.ltpMemLength
	pxqIndex := p.ltpMemLength

	for k := range p.nbSubfr {
		aQ12 := p.predCoefQ12[((k>>1)|(1-lsfInterpFlag))*maxLPCOrder:]
		bQ14 := p.ltpCoefQ14[k*ltpOrder:]
		arShpQ13 := p.arQ13[k*maxShapeLPCOrder:]

		harmShapeFIRPackedQ14 := p.harmShapeGainQ14[k] >> 2
		harmShapeFIRPackedQ14 |= (p.harmShapeGainQ14[k] >> 1) << 16

		nsq.rewhiteFlag = 0
		if p.signalType == frameSignalTypeVoiced {
			lag = p.pitchL[k]
			if k&(3-(lsfInterpFlag<<1)) == 0 {
				startIdx := p.ltpMemLength - lag - p.predictLPCOrder - ltpOrder/2
				lpcAnalysisFilterFixed(sLTP[startIdx:], nsq.xq[startIdx+k*p.subfrLength:],
					aQ12, p.ltpMemLength-startIdx, p.predictLPCOrder)
				nsq.rewhiteFlag = 1
				nsq.sLTPBufIdx = p.ltpMemLength
			}
		}

		nsq.scaleStates(x16[k*p.subfrLength:], xScQ10, sLTP, sLTPQ15, k, lag, p)
		nsq.noiseShapeQuantizer(xScQ10, pulses[k*p.subfrLength:], pxqIndex, sLTPQ15,
			aQ12, bQ14, arShpQ13, lag, harmShapeFIRPackedQ14, p.tiltQ14[k], p.lfShpQ14[k],
			p.gainsQ16[k], p.lambdaQ10, offsetQ10, p)

		pxqIndex += p.subfrLength
	}

	nsq.lagPrev = p.pitchL[p.nbSubfr-1]

	// Shift the quantized-speech and shaping buffers, keeping the LTP memory.
	copy(nsq.xq, nsq.xq[p.frameLength:p.frameLength+p.ltpMemLength])
	copy(nsq.sLTPShpQ14, nsq.sLTPShpQ14[p.frameLength:p.frameLength+p.ltpMemLength])
}

//nolint:gocyclo,cyclop // faithful port of the dense NSQ inner loop.
func (nsq *nsqState) noiseShapeQuantizer(
	xScQ10 []int32, pulses []int8, xqIndex int, sLTPQ15 []int32,
	aQ12, bQ14, arShpQ13 []int16, lag int,
	harmShapeFIRPackedQ14, tiltQ14, lfShpQ14, gainQ16, lambdaQ10, offsetQ10 int32,
	p *nsqParams,
) {
	shpLagPtr := nsq.sLTPShpBufIdx - lag + harmShapeFIRTaps/2
	predLagPtr := nsq.sLTPBufIdx - lag + ltpOrder/2
	gainQ10 := gainQ16 >> 6
	psLPCIndex := nsqLPCBufLength - 1

	for i := range p.subfrLength {
		nsq.randSeed = silkRand(nsq.randSeed)
		lpcPredQ10 := nsqShortPrediction(nsq.sLPCQ14, psLPCIndex, aQ12, p.predictLPCOrder)

		var ltpPredQ13 int32
		if p.signalType == frameSignalTypeVoiced {
			ltpPredQ13 = 2
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLagPtr], int32(bQ14[0]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLagPtr-1], int32(bQ14[1]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLagPtr-2], int32(bQ14[2]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLagPtr-3], int32(bQ14[3]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLagPtr-4], int32(bQ14[4]))
			predLagPtr++
		}

		nARQ12 := nsqNoiseShapeFeedbackLoop(nsq.sDiffShpQ14, nsq.sAR2Q14[:], arShpQ13, p.shapingLPCOrder)
		nARQ12 = smlawb(nARQ12, nsq.sLFARShpQ14, tiltQ14)

		nLFQ12 := smulwb(nsq.sLTPShpQ14[nsq.sLTPShpBufIdx-1], lfShpQ14)
		nLFQ12 = smlawt(nLFQ12, nsq.sLFARShpQ14, lfShpQ14)

		tmp1 := sub32Ovflw(lpcPredQ10<<2, nARQ12)
		tmp1 = sub32Ovflw(tmp1, nLFQ12)
		if lag > 0 {
			nLTPQ13 := smulwb(addSat32(nsq.sLTPShpQ14[shpLagPtr], nsq.sLTPShpQ14[shpLagPtr-2]), harmShapeFIRPackedQ14)
			nLTPQ13 = smlawt(nLTPQ13, nsq.sLTPShpQ14[shpLagPtr-1], harmShapeFIRPackedQ14)
			nLTPQ13 <<= 1
			shpLagPtr++
			tmp2 := ltpPredQ13 - nLTPQ13
			tmp1 = add32Ovflw(tmp2, lshiftOvflw(tmp1, 1))
			tmp1 = rshiftRound32(tmp1, 3)
		} else {
			tmp1 = rshiftRound32(tmp1, 2)
		}

		rQ10 := xScQ10[i] - tmp1
		if nsq.randSeed < 0 {
			rQ10 = -rQ10
		}
		rQ10 = clamp(-(31 << 10), rQ10, 30<<10)

		q1Q10 := rQ10 - offsetQ10
		q1Q0 := q1Q10 >> 10
		if lambdaQ10 > 2048 {
			rdoOffset := lambdaQ10/2 - 512
			switch {
			case q1Q10 > rdoOffset:
				q1Q0 = (q1Q10 - rdoOffset) >> 10
			case q1Q10 < -rdoOffset:
				q1Q0 = (q1Q10 + rdoOffset) >> 10
			case q1Q10 < 0:
				q1Q0 = -1
			default:
				q1Q0 = 0
			}
		}

		var q2Q10, rd1Q20, rd2Q20 int32
		switch {
		case q1Q0 > 0:
			q1Q10 = (q1Q0 << 10) - quantLevelAdjustQ10 + offsetQ10
			q2Q10 = q1Q10 + 1024
			rd1Q20 = smulbb(q1Q10, lambdaQ10)
			rd2Q20 = smulbb(q2Q10, lambdaQ10)
		case q1Q0 == 0:
			q1Q10 = offsetQ10
			q2Q10 = q1Q10 + 1024 - quantLevelAdjustQ10
			rd1Q20 = smulbb(q1Q10, lambdaQ10)
			rd2Q20 = smulbb(q2Q10, lambdaQ10)
		case q1Q0 == -1:
			q2Q10 = offsetQ10
			q1Q10 = q2Q10 - (1024 - quantLevelAdjustQ10)
			rd1Q20 = smulbb(-q1Q10, lambdaQ10)
			rd2Q20 = smulbb(q2Q10, lambdaQ10)
		default:
			q1Q10 = (q1Q0 << 10) + quantLevelAdjustQ10 + offsetQ10
			q2Q10 = q1Q10 + 1024
			rd1Q20 = smulbb(-q1Q10, lambdaQ10)
			rd2Q20 = smulbb(-q2Q10, lambdaQ10)
		}
		rrQ10 := rQ10 - q1Q10
		rd1Q20 = smlabb(rd1Q20, rrQ10, rrQ10)
		rrQ10 = rQ10 - q2Q10
		rd2Q20 = smlabb(rd2Q20, rrQ10, rrQ10)
		if rd2Q20 < rd1Q20 {
			q1Q10 = q2Q10
		}

		pulses[i] = int8(rshiftRound32(q1Q10, 10))

		excQ14 := q1Q10 << 4
		if nsq.randSeed < 0 {
			excQ14 = -excQ14
		}

		lpcExcQ14 := addLShift32(excQ14, ltpPredQ13, 1)
		xqQ14 := add32Ovflw(lpcExcQ14, lpcPredQ10<<4)

		nsq.xq[xqIndex+i] = int16(sat16(rshiftRound32(smulww(xqQ14, gainQ10), 8)))

		psLPCIndex++
		nsq.sLPCQ14[psLPCIndex] = xqQ14
		nsq.sDiffShpQ14 = sub32Ovflw(xqQ14, lshiftOvflw(xScQ10[i], 4))
		sLFARShpQ14 := sub32Ovflw(nsq.sDiffShpQ14, lshiftOvflw(nARQ12, 2))
		nsq.sLFARShpQ14 = sLFARShpQ14
		nsq.sLTPShpQ14[nsq.sLTPShpBufIdx] = sub32Ovflw(sLFARShpQ14, lshiftOvflw(nLFQ12, 2))
		sLTPQ15[nsq.sLTPBufIdx] = lpcExcQ14 << 1
		nsq.sLTPShpBufIdx++
		nsq.sLTPBufIdx++
		nsq.randSeed = add32Ovflw(nsq.randSeed, int32(pulses[i]))
	}

	copy(nsq.sLPCQ14, nsq.sLPCQ14[p.subfrLength:p.subfrLength+nsqLPCBufLength])
}

// scaleStates scales the input and LTP state by 1/gain and adjusts for gain
// changes (silk_nsq_scale_states).
func (nsq *nsqState) scaleStates(x16 []int16, xScQ10 []int32, sLTP []int16, sLTPQ15 []int32, subfr, lag int, p *nsqParams) {
	invGainQ31 := inverse32VarQ(max(p.gainsQ16[subfr], 1), 47)
	invGainQ26 := rshiftRound32(invGainQ31, 5)
	for i := range p.subfrLength {
		xScQ10[i] = smulww(int32(x16[i]), invGainQ26)
	}

	if nsq.rewhiteFlag != 0 {
		if subfr == 0 {
			invGainQ31 = smulwb(invGainQ31, p.ltpScaleQ14) << 2
		}
		for i := nsq.sLTPBufIdx - lag - ltpOrder/2; i < nsq.sLTPBufIdx; i++ {
			sLTPQ15[i] = smulwb(invGainQ31, int32(sLTP[i]))
		}
	}

	if p.gainsQ16[subfr] != nsq.prevGainQ16 {
		gainAdjQ16 := div32VarQ(nsq.prevGainQ16, p.gainsQ16[subfr], 16)
		for i := nsq.sLTPShpBufIdx - p.ltpMemLength; i < nsq.sLTPShpBufIdx; i++ {
			nsq.sLTPShpQ14[i] = smulww(gainAdjQ16, nsq.sLTPShpQ14[i])
		}
		if p.signalType == frameSignalTypeVoiced && nsq.rewhiteFlag == 0 {
			for i := nsq.sLTPBufIdx - lag - ltpOrder/2; i < nsq.sLTPBufIdx; i++ {
				sLTPQ15[i] = smulww(gainAdjQ16, sLTPQ15[i])
			}
		}
		nsq.sLFARShpQ14 = smulww(gainAdjQ16, nsq.sLFARShpQ14)
		nsq.sDiffShpQ14 = smulww(gainAdjQ16, nsq.sDiffShpQ14)
		for i := range nsqLPCBufLength {
			nsq.sLPCQ14[i] = smulww(gainAdjQ16, nsq.sLPCQ14[i])
		}
		for i := range maxShapeLPCOrder {
			nsq.sAR2Q14[i] = smulww(gainAdjQ16, nsq.sAR2Q14[i])
		}
		nsq.prevGainQ16 = p.gainsQ16[subfr]
	}
}

// nsqShortPrediction returns the short-term LPC prediction
// (silk_noise_shape_quantizer_short_prediction).
func nsqShortPrediction(buf []int32, bufIdx int, coef []int16, order int) int32 {
	out := int32(order >> 1)
	for i := range order {
		out = smlawb(out, buf[bufIdx-i], int32(coef[i]))
	}

	return out
}

// nsqNoiseShapeFeedbackLoop applies the noise-shaping AR filter, updating its
// state (silk_NSQ_noise_shape_feedback_loop).
func nsqNoiseShapeFeedbackLoop(data0 int32, data1 []int32, coef []int16, order int) int32 {
	tmp2 := data0
	tmp1 := data1[0]
	data1[0] = tmp2

	out := int32(order >> 1)
	out = smlawb(out, tmp2, int32(coef[0]))
	for j := 2; j < order; j += 2 {
		tmp2 = data1[j-1]
		data1[j-1] = tmp1
		out = smlawb(out, tmp1, int32(coef[j-1]))
		tmp1 = data1[j]
		data1[j] = tmp2
		out = smlawb(out, tmp2, int32(coef[j]))
	}
	data1[order-1] = tmp1
	out = smlawb(out, tmp1, int32(coef[order-1]))

	return out << 1
}

// lpcAnalysisFilterFixed is the fixed-point LPC residual filter used for
// re-whitening (silk_LPC_analysis_filter). b holds Q12 coefficients.
func lpcAnalysisFilterFixed(out, in []int16, b []int16, length, order int) {
	for ix := order; ix < length; ix++ {
		out32Q12 := smulbb(int32(in[ix-1]), int32(b[0]))
		for j := 1; j < order; j++ {
			out32Q12 = smlabbOvflw(out32Q12, int32(in[ix-1-j]), int32(b[j]))
		}
		out32Q12 = sub32Ovflw(int32(in[ix])<<12, out32Q12)
		out[ix] = int16(sat16(rshiftRound32(out32Q12, 12)))
	}
	for j := range order {
		out[j] = 0
	}
}
