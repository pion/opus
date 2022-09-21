package silk

import (
	"github.com/pion/opus/internal/rangecoding"
)

// Decoder maintains the state needed to decode a stream
// of Silk frames
type Decoder struct {
	rangeDecoder rangecoding.Decoder

	// Have we decoded a frame yet?
	haveDecoded bool

	// Is the previous frame a voiced frame?
	isPreviousFrameVoiced bool

	previousLogGain int32

	// The decoder saves the final d_LPC values, i.e., lpc[i] such that
	// (j + n - d_LPC) <= i < (j + n), to feed into the LPC synthesis of the
	// next subframe.  This requires storage for up to 16 values of lpc[i]
	// (for WB frames).
	//
	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.2
	finalLPCValues []float32

	// n0Q15 are the LSF coefficients decoded for the prior frame
	// see normalizeLSFInterpolation
	n0Q15 []int16
}

// NewDecoder creates a new Silk Decoder
func NewDecoder() Decoder {
	return Decoder{
		finalLPCValues: make([]float32, 16),
	}
}

// The LP layer begins with two to eight header bits These consist of one
// Voice Activity Detection (VAD) bit per frame (up to 3), followed by a
// single flag indicating the presence of LBRR frames.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.3
func (d *Decoder) decodeHeaderBits() (voiceActivityDetected, lowBitRateRedundancy bool) {
	voiceActivityDetected = d.rangeDecoder.DecodeSymbolLogP(1) == 1
	lowBitRateRedundancy = d.rangeDecoder.DecodeSymbolLogP(1) == 1
	return
}

// Each SILK frame contains a single "frame type" symbol that jointly
// codes the signal type and quantization offset type of the
// corresponding frame.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.3
func (d *Decoder) determineFrameType(voiceActivityDetected bool) (signalType frameSignalType, quantizationOffsetType frameQuantizationOffsetType) {
	var frameTypeSymbol uint32
	if voiceActivityDetected {
		frameTypeSymbol = d.rangeDecoder.DecodeSymbolWithICDF(icdfFrameTypeVADActive)
	} else {
		frameTypeSymbol = d.rangeDecoder.DecodeSymbolWithICDF(icdfFrameTypeVADInactive)
	}

	// +------------+-------------+--------------------------+
	// | Frame Type | Signal Type | Quantization Offset Type |
	// +------------+-------------+--------------------------+
	// | 0          | Inactive    |                      Low |
	// |            |             |                          |
	// | 1          | Inactive    |                     High |
	// |            |             |                          |
	// | 2          | Unvoiced    |                      Low |
	// |            |             |                          |
	// | 3          | Unvoiced    |                     High |
	// |            |             |                          |
	// | 4          | Voiced      |                      Low |
	// |            |             |                          |
	// | 5          | Voiced      |                     High |
	// +------------+-------------+--------------------------+
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.3

	switch {
	case !voiceActivityDetected && frameTypeSymbol == 0:
		signalType = frameSignalTypeInactive
		quantizationOffsetType = frameQuantizationOffsetTypeLow
	case !voiceActivityDetected:
		signalType = frameSignalTypeInactive
		quantizationOffsetType = frameQuantizationOffsetTypeHigh
	case frameTypeSymbol == 0:
		signalType = frameSignalTypeUnvoiced
		quantizationOffsetType = frameQuantizationOffsetTypeLow
	case frameTypeSymbol == 1:
		signalType = frameSignalTypeUnvoiced
		quantizationOffsetType = frameQuantizationOffsetTypeHigh
	case frameTypeSymbol == 2:
		signalType = frameSignalTypeVoiced
		quantizationOffsetType = frameQuantizationOffsetTypeLow
	case frameTypeSymbol == 3:
		signalType = frameSignalTypeVoiced
		quantizationOffsetType = frameQuantizationOffsetTypeHigh
	}

	return
}

// A separate quantization gain is coded for each 5 ms subframe
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
func (d *Decoder) decodeSubframeQuantizations(signalType frameSignalType) (gainQ16 []float32) {
	var logGain, deltaGainIndex, gainIndex int32
	gainQ16 = make([]float32, 4)

	for subframeIndex := 0; subframeIndex < subframeCount; subframeIndex++ {

		//The subframe gains are either coded independently, or relative to the
		// gain from the most recent coded subframe in the same channel.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
		if subframeIndex == 0 {
			// In an independently coded subframe gain, the 3 most significant bits
			// of the quantization gain are decoded using a PDF selected from
			// Table 11 based on the decoded signal type
			switch signalType {
			case frameSignalTypeInactive:
				gainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBInactive))
			case frameSignalTypeVoiced:
				gainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBVoiced))
			case frameSignalTypeUnvoiced:
				gainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBUnvoiced))
			}

			// The 3 least significant bits are decoded using a uniform PDF:
			// These 6 bits are combined to form a value, gain_index, between 0 and 63.
			gainIndex = (gainIndex << 3) | int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainLSB))

			// When the gain for the previous subframe is available, then the
			// current gain is limited as follows:
			//     log_gain = max(gain_index, previous_log_gain - 16)
			if d.haveDecoded {
				logGain = maxInt32(gainIndex, d.previousLogGain-16)
			} else {
				logGain = gainIndex
			}
		} else {
			// For subframes that do not have an independent gain (including the
			// first subframe of frames not listed as using independent coding
			// above), the quantization gain is coded relative to the gain from the
			// previous subframe
			deltaGainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfDeltaQuantizationGain))

			// The following formula translates this index into a quantization gain
			// for the current subframe using the gain from the previous subframe:
			//      log_gain = clamp(0, max(2*delta_gain_index - 16, previous_log_gain + delta_gain_index - 4), 63)
			logGain = int32(clamp(0, maxInt32(2*int32(deltaGainIndex)-16, int32(d.previousLogGain+deltaGainIndex)-4), 63))
		}

		d.previousLogGain = logGain

		// silk_gains_dequant() (gain_quant.c) dequantizes log_gain for the k'th
		// subframe and converts it into a linear Q16 scale factor via
		//
		//       gain_Q16[k] = silk_log2lin((0x1D1C71*log_gain>>16) + 2090)
		//
		inLogQ7 := (0x1D1C71 * int32(logGain) >> 16) + 2090
		i := inLogQ7 >> 7
		f := inLogQ7 & 127

		// The function silk_log2lin() (log2lin.c) computes an approximation of
		// 2**(inLog_Q7/128.0), where inLog_Q7 is its Q7 input.  Let i =
		// inLog_Q7>>7 be the integer part of inLogQ7 and f = inLog_Q7&127 be
		// the fractional part.  Then,
		//
		//             (1<<i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7)
		//
		// yields the approximate exponential.  The final Q16 gain values lies
		// between 81920 and 1686110208, inclusive (representing scale factors
		// of 1.25 to 25728, respectively).

		gainQ16[subframeIndex] = float32((1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7))
	}

	return
}

// A set of normalized Line Spectral Frequency (LSF) coefficients follow
// the quantization gains in the bitstream and represent the Linear
// Predictive Coding (LPC) coefficients for the current SILK frame.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.1
func (d *Decoder) normalizeLineSpectralFrequencyStageOne(voiceActivityDetected bool, bandwidth Bandwidth) (I1 uint32) {
	// The first VQ stage uses a 32-element codebook, coded with one of the
	// PDFs in Table 14, depending on the audio bandwidth and the signal
	// type of the current SILK frame.  This yields a single index, I1, for
	// the entire frame, which
	//
	// 1.  Indexes an element in a coarse codebook,
	// 2.  Selects the PDFs for the second stage of the VQ, and
	// 3.  Selects the prediction weights used to remove intra-frame
	//     redundancy from the second stage.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.1
	switch {
	case !voiceActivityDetected && (bandwidth == BandwidthNarrowband || bandwidth == BandwidthMediumband):
		I1 = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandUnvoiced)
	case voiceActivityDetected && (bandwidth == BandwidthNarrowband || bandwidth == BandwidthMediumband):
		I1 = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandVoiced)
	case !voiceActivityDetected && (bandwidth == BandwidthWideband):
		I1 = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexWidebandUnvoiced)
	case voiceActivityDetected && (bandwidth == BandwidthWideband):
		I1 = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexWidebandVoiced)
	}

	return
}

// A set of normalized Line Spectral Frequency (LSF) coefficients follow
// the quantization gains in the bitstream and represent the Linear
// Predictive Coding (LPC) coefficients for the current SILK frame.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
func (d *Decoder) normalizeLineSpectralFrequencyStageTwo(bandwidth Bandwidth, I1 uint32) (dLPC int, resQ10 []int16) {
	// Decoding the second stage residual proceeds as follows.  For each
	// coefficient, the decoder reads a symbol using the PDF corresponding
	// to I1 from either Table 17 or Table 18,
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	var codebook [][]uint
	if bandwidth == BandwidthWideband {
		codebook = codebookNormalizedLSFStageTwoIndexWideband
	} else {
		codebook = codebookNormalizedLSFStageTwoIndexNarrowbandOrMediumband
	}

	I2 := make([]int8, len(codebook[0]))
	for i := 0; i < len(I2); i++ {
		// the decoder reads a symbol using the PDF corresponding
		// to I1 from either Table 17 or Table 18 and subtracts 4 from the
		// result to give an index in the range -4 to 4, inclusive.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
		I2[i] = int8(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndex[codebook[I1][i]])) - 4

		// If the index is either -4 or 4, it reads a second symbol using the PDF in
		// Table 19, and adds the value of this second symbol to the index,
		// using the same sign.  This gives the index, I2[k], a total range of
		// -10 to 10, inclusive.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
		if I2[i] == -4 {
			I2[i] -= int8(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndexExtension))
		} else if I2[i] == 4 {
			I2[i] += int8(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndexExtension))
		}
	}

	// The decoded indices from both stages are translated back into
	// normalized LSF coefficients. The stage-2 indices represent residuals
	// after both the first stage of the VQ and a separate backwards-prediction
	// step. The backwards prediction process in the encoder subtracts a prediction
	// from each residual formed by a multiple of the coefficient that follows it.
	// The decoder must undo this process.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2

	// qstep is the Q16 quantization step size, which is 11796 for NB and MB and 9830
	// for WB (representing step sizes of approximately 0.18 and 0.15, respectively).
	var qstep int
	if bandwidth == BandwidthWideband {
		qstep = 9830
	} else {
		qstep = 11796
	}

	// stage-2 residual
	resQ10 = make([]int16, len(I2))

	// Let d_LPC be the order of the codebook, i.e., 10 for NB and MB, and 16 for WB
	dLPC = len(I2)

	// for 0 <= k < d_LPC-1
	for k := dLPC - 2; k >= 0; k-- {
		// The stage-2 residual for each coefficient is computed via
		//
		//     res_Q10[k] = (k+1 < d_LPC ? (res_Q10[k+1]*pred_Q8[k])>>8 : 0) + ((((I2[k]<<10) - sign(I2[k])*102)*qstep)>>16) ,
		//

		// The following computes
		//
		// (k+1 < d_LPC ? (res_Q10[k+1]*pred_Q8[k])>>8 : 0)
		//
		firstOperand := int(0)
		if k+1 < dLPC {
			// Each coefficient selects its prediction weight from one of the two lists based on the stage-1 index, I1.
			// let pred_Q8[k] be the weight for the k'th coefficient selected by this process for 0 <= k < d_LPC-1
			predQ8 := int(0)
			if bandwidth == BandwidthWideband {
				predQ8 = int(predictionWeightForWidebandNormalizedLSF[predictionWeightSelectionForWidebandNormalizedLSF[I1][k]][k])
			} else {
				predQ8 = int(predictionWeightForNarrowbandAndMediumbandNormalizedLSF[predictionWeightSelectionForNarrowbandAndMediumbandNormalizedLSF[I1][k]][k])
			}

			firstOperand = (int(resQ10[k+1]) * predQ8) >> 8
		}

		// The following computes
		//
		// (((I2[k]<<10) - sign(I2[k])*102)*qstep)>>16
		//
		secondOperand := (((int(I2[k]) << 10) - sign(int(I2[k]))*102) * qstep) >> 16

		resQ10[k] = int16(firstOperand + secondOperand)
	}

	return
}

// Once the stage-1 index I1 and the stage-2 residual res_Q10[] have
// been decoded, the final normalized LSF coefficients can be
// reconstructed.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
func (d *Decoder) normalizeLineSpectralFrequencyCoefficients(dLPC int, bandwidth Bandwidth, resQ10 []int16, I1 uint32) (nlsfQ15 []int16) {
	nlsfQ15 = make([]int16, dLPC)
	w2Q18 := make([]uint, dLPC)
	wQ9 := make([]int16, dLPC)

	cb1Q8 := codebookNormalizedLSFStageOneNarrowbandOrMediumband
	if bandwidth == BandwidthWideband {
		cb1Q8 = codebookNormalizedLSFStageOneWideband
	}

	// Let cb1_Q8[k] be the k'th entry of the stage-1 codebook vector from Table 23 or Table 24.
	// Then, for 0 <= k < d_LPC, the following expression computes the
	// square of the weight as a Q18 value:
	//
	//          w2_Q18[k] = (1024/(cb1_Q8[k] - cb1_Q8[k-1])
	//                       + 1024/(cb1_Q8[k+1] - cb1_Q8[k])) << 16
	//
	// where cb1_Q8[-1] = 0 and cb1_Q8[d_LPC] = 256, and the division is
	// integer division.  This is reduced to an unsquared, Q9 value using
	// the following square-root approximation:
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
	for k := 0; k < dLPC; k++ {
		kMinusOne, kPlusOne := uint(0), uint(256)
		if k != 0 {
			kMinusOne = cb1Q8[I1][k-1]
		}

		if k+1 != dLPC {
			kPlusOne = cb1Q8[I1][k+1]
		}

		w2Q18[k] = (1024/(cb1Q8[I1][k]-kMinusOne) +
			1024/(kPlusOne-cb1Q8[I1][k])) << 16

		// This is reduced to an unsquared, Q9 value using
		// the following square-root approximation:
		//
		//     i = ilog(w2_Q18[k])
		//     f = (w2_Q18[k]>>(i-8)) & 127
		//     y = ((i&1) ? 32768 : 46214) >> ((32-i)>>1)
		//     w_Q9[k] = y + ((213*f*y)>>16)
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
		i := ilog(int(w2Q18[k]))
		f := int((w2Q18[k] >> (i - 8)) & 127)

		y := 46214
		if (i & 1) != 0 {
			y = 32768
		}

		y = y >> ((32 - i) >> 1)
		wQ9[k] = int16(y + ((213 * f * y) >> 16))

		// Given the stage-1 codebook entry cb1_Q8[], the stage-2 residual
		// res_Q10[], and their corresponding weights, w_Q9[], the reconstructed
		// normalized LSF coefficients are
		//
		//    NLSF_Q15[k] = clamp(0,
		//               (cb1_Q8[k]<<7) + (res_Q10[k]<<14)/w_Q9[k], 32767)
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
		nlsfQ15[k] = int16(clamp(0,
			int32((int(cb1Q8[I1][k])<<7)+(int(resQ10[k])<<14)/int(wQ9[k])), 32767))
	}

	return
}

// The normalized LSF stabilization procedure ensures that
// consecutive values of the normalized LSF coefficients, NLSF_Q15[],
// are spaced some minimum distance apart (predetermined to be the 0.01
// percentile of a large training set).
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
func (d *Decoder) normalizeLSFStabilization(nlsfQ15 []int16) {
	// TODO
}

// For 20 ms SILK frames, the first half of the frame (i.e., the first
// two subframes) may use normalized LSF coefficients that are
// interpolated between the decoded LSFs for the most recent coded frame
// (in the same channel) and the current frame
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
func (d *Decoder) normalizeLSFInterpolation(n2Q15 []int16) (n1Q15 []int16, wQ2 int16) {
	// Let n2_Q15[k] be the normalized LSF coefficients decoded by the
	// procedure in Section 4.2.7.5, n0_Q15[k] be the LSF coefficients
	// decoded for the prior frame, and w_Q2 be the interpolation factor.
	// Then, the normalized LSF coefficients used for the first half of a
	// 20 ms frame, n1_Q15[k], are
	//
	//      n1_Q15[k] = n0_Q15[k] + (w_Q2*(n2_Q15[k] - n0_Q15[k]) >> 2)
	wQ2 = int16(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex))
	if wQ2 == 4 || !d.haveDecoded {
		return n2Q15, wQ2
	}

	n1Q15 = make([]int16, len(n2Q15))
	for k := range n1Q15 {
		n1Q15[k] = d.n0Q15[k] + (wQ2 * (n2Q15[k] - d.n0Q15[k]) >> 2)
	}

	return
}

func (d *Decoder) convertNormalizedLSFsToLPCCoefficients(n1Q15 []int16, bandwidth Bandwidth) (a32Q17 []int32) {
	cQ17 := make([]int32, len(n1Q15))
	cosQ12 := q12CosineTableForLSFConverion

	ordering := lsfOrderingForPolynomialEvaluationNarrowbandAndMediumband
	if bandwidth == BandwidthWideband {
		ordering = lsfOrderingForPolynomialEvaluationWideband
	}

	// The top 7 bits of each normalized LSF coefficient index a value in
	// the table, and the next 8 bits interpolate between it and the next
	// value.  Let i = (n[k] >> 8) be the integer index and f = (n[k] & 255)
	// be the fractional part of a given coefficient.  Then, the re-ordered,
	// approximated cosine, c_Q17[ordering[k]], is
	//
	//     c_Q17[ordering[k]] = (cos_Q12[i]*256
	//                           + (cos_Q12[i+1]-cos_Q12[i])*f + 4) >> 3
	//
	// where ordering[k] is the k'th entry of the column of Table 27
	// corresponding to the current audio bandwidth and cos_Q12[i] is the
	// i'th entry of Table 28.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6
	for k := range n1Q15 {
		i := int32(n1Q15[k] >> 8)
		f := int32(n1Q15[k] & 255)

		cQ17[ordering[k]] = (cosQ12[i]*256 +
			(cosQ12[i+1]-cosQ12[i])*f + 4) >> 3
	}

	pQ16 := make([]int32, (len(n1Q15)/2)+1)
	qQ16 := make([]int32, (len(n1Q15)/2)+1)

	// Given the list of cosine values compute the coefficients of P and Q,
	// described here via a simple recurrence.  Let p_Q16[k][j] and q_Q16[k][j]
	// be the coefficients of the products of the first (k+1) root pairs for P and
	// Q, with j indexing the coefficient number.  Only the first (k+2) coefficients
	// are needed, as the products are symmetric.  Let
	//
	//      p_Q16[0][0] = q_Q16[0][0] = 1<<16
	//      p_Q16[0][1] = -c_Q17[0]
	//      q_Q16[0][1] = -c_Q17[1]
	//      d2 = d_LPC/2
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6

	pQ16[0] = 1 << 16
	qQ16[0] = 1 << 16
	pQ16[1] = -cQ17[0]
	qQ16[1] = -cQ17[1]
	dLPC := len(n1Q15)
	d2 := dLPC / 2

	// As boundary conditions, assume p_Q16[k][j] = q_Q16[k][j] = 0 for all j < 0.
	// Also, assume (because of the symmetry)
	//
	//      p_Q16[k][k+2] = p_Q16[k][k]
	//      q_Q16[k][k+2] = q_Q16[k][k]
	//
	// Then, for 0 < k < d2 and 0 <= j <= k+1,
	//
	//      p_Q16[k][j] = p_Q16[k-1][j] + p_Q16[k-1][j-2]
	//                    - ((c_Q17[2*k]*p_Q16[k-1][j-1] + 32768)>>16)
	//
	//      q_Q16[k][j] = q_Q16[k-1][j] + q_Q16[k-1][j-2]
	//                    - ((c_Q17[2*k+1]*q_Q16[k-1][j-1] + 32768)>>16)
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6

	for k := 1; k < d2; k++ {
		pQ16[k+1] = pQ16[k-1]*2 - int32(((int64(cQ17[2*k])*int64(pQ16[k]))+32768)>>16)
		qQ16[k+1] = qQ16[k-1]*2 - int32(((int64(cQ17[(2*k)+1])*int64(qQ16[k]))+32768)>>16)

		for j := k; j > 1; j-- {
			pQ16[j] += pQ16[j-2] - int32(((int64(cQ17[2*k])*int64(pQ16[j-1]))+32768)>>16)
			qQ16[j] += qQ16[j-2] - int32(((int64(cQ17[(2*k)+1])*int64(qQ16[j-1]))+32768)>>16)
		}

		pQ16[1] -= cQ17[2*k]
		qQ16[1] -= cQ17[2*k+1]
	}

	// silk_NLSF2A() uses the values from the last row of this recurrence to
	// reconstruct a 32-bit version of the LPC filter (without the leading
	// 1.0 coefficient), a32_Q17[k], 0 <= k < d2:
	//
	//      a32_Q17[k]         = -(q_Q16[d2-1][k+1] - q_Q16[d2-1][k])
	//                           - (p_Q16[d2-1][k+1] + p_Q16[d2-1][k]))
	//
	//      a32_Q17[d_LPC-k-1] =  (q_Q16[d2-1][k+1] - q_Q16[d2-1][k])
	//                           - (p_Q16[d2-1][k+1] + p_Q16[d2-1][k]))
	//
	// The sum and difference of two terms from each of the p_Q16 and q_Q16
	// coefficient lists reflect the (1 + z**-1) and (1 - z**-1) factors of
	// P and Q, respectively.  The promotion of the expression from Q16 to
	// Q17 implicitly scales the result by 1/2.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6

	a32Q17 = make([]int32, len(n1Q15))
	for k := 0; k < d2; k++ {
		a32Q17[k] = -(qQ16[k+1] - qQ16[k]) - (pQ16[k+1] + pQ16[k])
		a32Q17[dLPC-k-1] = (qQ16[k+1] - qQ16[k]) - (pQ16[k+1] + pQ16[k])
	}
	return
}

// As described in Section 4.2.7.8.6, SILK uses a Linear Congruential
// Generator (LCG) to inject pseudorandom noise into the quantized
// excitation.  To ensure synchronization of this process between the
// encoder and decoder, each SILK frame stores a 2-bit seed after the
// LTP parameters (if any).
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.7
func (d *Decoder) decodeLinearCongruentialGeneratorSeed() uint32 {
	return d.rangeDecoder.DecodeSymbolWithICDF(icdfLinearCongruentialGeneratorSeed)
}

// SILK fixes the dimension of the codebook to N = 16.  The excitation
// is made up of a number of "shell blocks", each 16 samples in size.
// Table 44 lists the number of shell blocks required for a SILK frame
// for each possible audio bandwidth and frame size.
//
// +-----------------+------------+------------------------+
// | Audio Bandwidth | Frame Size | Number of Shell Blocks |
// +-----------------+------------+------------------------+
// | NB              | 10 ms      |                      5 |
// |                 |            |                        |
// | MB              | 10 ms      |                      8 |
// |                 |            |                        |
// | WB              | 10 ms      |                     10 |
// |                 |            |                        |
// | NB              | 20 ms      |                     10 |
// |                 |            |                        |
// | MB              | 20 ms      |                     15 |
// |                 |            |                        |
// | WB              | 20 ms      |                     20 |
// +-----------------+------------+------------------------+
//
//	Table 44: Number of Shell Blocks Per SILK Frame
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8
func (d *Decoder) decodeShellblocks(nanoseconds int, bandwidth Bandwidth) (shellblocks int) {
	switch {
	case bandwidth == BandwidthNarrowband && nanoseconds == nanoseconds10Ms:
		shellblocks = 5
	case bandwidth == BandwidthMediumband && nanoseconds == nanoseconds10Ms:
		shellblocks = 8
	case bandwidth == BandwidthWideband && nanoseconds == nanoseconds10Ms:
		fallthrough
	case bandwidth == BandwidthNarrowband && nanoseconds == nanoseconds20Ms:
		shellblocks = 10
	case bandwidth == BandwidthMediumband && nanoseconds == nanoseconds20Ms:
		shellblocks = 15
	case bandwidth == BandwidthWideband && nanoseconds == nanoseconds20Ms:
		shellblocks = 20
	}
	return
}

// The first symbol in the excitation is a "rate level", which is an
// index from 0 to 8, inclusive, coded using the PDF in Table 45
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.1
func (d *Decoder) decodeRatelevel(voiceActivityDetected bool) uint32 {
	if voiceActivityDetected {
		return d.rangeDecoder.DecodeSymbolWithICDF(icdfRateLevelVoiced)
	}

	return d.rangeDecoder.DecodeSymbolWithICDF(icdfRateLevelUnvoiced)
}

// The total number of pulses in each of the shell blocks follows the
// rate level.  The pulse counts for all of the shell blocks are coded
// consecutively, before the content of any of the blocks.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.2
func (d *Decoder) decodePulseAndLSBCounts(shellblocks int, rateLevel uint32) (pulsecounts []uint8, lsbcounts []uint8) {
	pulsecounts = make([]uint8, shellblocks)
	lsbcounts = make([]uint8, shellblocks)
	for i := 0; i < shellblocks; i++ {
		pulsecounts[i] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdfPulseCount[rateLevel]))

		// The special value 17 indicates that this block
		// has one or more additional LSBs to decode for each coefficient.
		if pulsecounts[i] == 17 {
			// If the decoder encounters this value, it decodes another value for the
			// actual pulse count of the block, but uses the PDF corresponding to
			// the special rate level 9 instead of the normal rate level.
			// This Process repeats until the decoder reads a value less than 17, and it
			// Then sets the number of extra LSBs used to the number of 17's decoded
			// For that block.
			lsbcount := uint8(0)
			for ; pulsecounts[i] == 17 && lsbcount < 10; lsbcount++ {
				pulsecounts[i] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdfPulseCount[9]))
			}
			lsbcounts[i] = lsbcount

			// If it reads the value 17 ten times, then the next
			// Iteration uses the special rate level 10 instead of 9.  The
			// Probability of decoding a 17 when using the PDF for rate level 10 is
			// Zero, ensuring that the number of LSBs for a block will not exceed
			// 10.  The cumulative distribution for rate level 10 is just a shifted
			// Version of that for 9 and thus does not require any additional
			// Storage.
			if lsbcount == 10 {
				pulsecounts[i] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdfPulseCount[10]))
			}
		}
	}

	return
}

// The locations of the pulses in each shell block follow the pulse
// counts. As with the pulse counts, these locations are coded for all the shell blocks
// before any of the remaining information for each block.  Unlike many
// other codecs, SILK places no restriction on the distribution of
// pulses within a shell block.  All of the pulses may be placed in a
// single location, or each one in a unique location, or anything in
// between.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.3
func (d *Decoder) decodePulseLocation(pulsecounts []uint8) (eRaw []int32) {
	eRaw = make([]int32, len(pulsecounts)*pulsecountLargestPartitionSize)
	for i := range pulsecounts {
		// This process skips partitions without any pulses, i.e., where
		// the initial pulse count from Section 4.2.7.8.2 was zero, or where the
		// split in the prior level indicated that all of the pulses fell on the
		// other side.  These partitions have nothing to code, so they require
		// no PDF.
		if pulsecounts[i] == 0 {
			continue
		}

		eRawIndex := pulsecountLargestPartitionSize * i
		samplePartition16 := make([]uint8, 2)
		samplePartition8 := make([]uint8, 2)
		samplePartition4 := make([]uint8, 2)
		samplePartition2 := make([]uint8, 2)

		// The location of pulses is coded by recursively partitioning each
		// block into halves, and coding how many pulses fall on the left side
		// of the split.  All remaining pulses must fall on the right side of
		// the split.
		d.partitionPulseCount(icdfPulseCountSplit16SamplePartitions, pulsecounts[i], samplePartition16)
		for j := 0; j < 2; j++ {
			d.partitionPulseCount(icdfPulseCountSplit8SamplePartitions, samplePartition16[j], samplePartition8)
			for k := 0; k < 2; k++ {
				d.partitionPulseCount(icdfPulseCountSplit4SamplePartitions, samplePartition8[k], samplePartition4)
				for l := 0; l < 2; l++ {
					d.partitionPulseCount(icdfPulseCountSplit2SamplePartitions, samplePartition4[l], samplePartition2)
					eRaw[eRawIndex] = int32(samplePartition2[0])
					eRawIndex++

					eRaw[eRawIndex] = int32(samplePartition2[1])
					eRawIndex++
				}
			}
		}
	}

	return
}

// After the decoder reads the pulse locations for all blocks, it reads
// the LSBs (if any) for each block in turn.  Inside each block, it
// reads all the LSBs for each coefficient in turn, even those where no
// pulses were allocated, before proceeding to the next one.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.4
func (d *Decoder) decodeExcitationLSB(eRaw []int32, lsbcounts []uint8) {
	for i := 0; i < len(eRaw); i++ {
		for bit := uint8(0); bit < lsbcounts[i/pulsecountLargestPartitionSize]; bit++ {
			eRaw[i] = (eRaw[i] << 1) | int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfExcitationLSB))
		}
	}
}

// After decoding the pulse locations and the LSBs, the decoder knows
// the magnitude of each coefficient in the excitation.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.5
func (d *Decoder) decodeExcitationSign(eRaw []int32, signalType frameSignalType, quantizationOffsetType frameQuantizationOffsetType, pulsecounts []uint8) {
	for i := 0; i < len(eRaw); i++ {
		// It then decodes a sign for all coefficients
		// with a non-zero magnitude
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.5
		if eRaw[i] == 0 {
			continue
		}

		var icdf []uint
		pulsecount := pulsecounts[i/pulsecountLargestPartitionSize]

		// using one of the PDFs from Table 52.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.5
		switch signalType {
		case frameSignalTypeInactive:
			switch quantizationOffsetType {
			case frameQuantizationOffsetTypeLow:
				switch pulsecount {
				case 0:
					icdf = icdfExcitationSignInactiveSignalLowQuantization0Pulse
				case 1:
					icdf = icdfExcitationSignInactiveSignalLowQuantization1Pulse
				case 2:
					icdf = icdfExcitationSignInactiveSignalLowQuantization2Pulse
				case 3:
					icdf = icdfExcitationSignInactiveSignalLowQuantization3Pulse
				case 4:
					icdf = icdfExcitationSignInactiveSignalLowQuantization4Pulse
				case 5:
					icdf = icdfExcitationSignInactiveSignalLowQuantization5Pulse
				default:
					icdf = icdfExcitationSignInactiveSignalLowQuantization6PlusPulse
				}
			case frameQuantizationOffsetTypeHigh:
				switch pulsecount {
				case 0:
					icdf = icdfExcitationSignInactiveSignalHighQuantization0Pulse
				case 1:
					icdf = icdfExcitationSignInactiveSignalHighQuantization1Pulse
				case 2:
					icdf = icdfExcitationSignInactiveSignalHighQuantization2Pulse
				case 3:
					icdf = icdfExcitationSignInactiveSignalHighQuantization3Pulse
				case 4:
					icdf = icdfExcitationSignInactiveSignalHighQuantization4Pulse
				case 5:
					icdf = icdfExcitationSignInactiveSignalHighQuantization5Pulse
				default:
					icdf = icdfExcitationSignInactiveSignalHighQuantization6PlusPulse
				}

			}
		case frameSignalTypeUnvoiced:
			switch quantizationOffsetType {
			case frameQuantizationOffsetTypeLow:
				switch pulsecount {
				case 0:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization0Pulse
				case 1:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization1Pulse
				case 2:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization2Pulse
				case 3:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization3Pulse
				case 4:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization4Pulse
				case 5:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization5Pulse
				default:
					icdf = icdfExcitationSignUnvoicedSignalLowQuantization6PlusPulse
				}
			case frameQuantizationOffsetTypeHigh:
				switch pulsecount {
				case 0:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization0Pulse
				case 1:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization1Pulse
				case 2:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization2Pulse
				case 3:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization3Pulse
				case 4:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization4Pulse
				case 5:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization5Pulse
				default:
					icdf = icdfExcitationSignUnvoicedSignalHighQuantization6PlusPulse
				}

			}

		case frameSignalTypeVoiced:
			switch quantizationOffsetType {
			case frameQuantizationOffsetTypeLow:
				switch pulsecount {
				case 0:
					icdf = icdfExcitationSignVoicedSignalLowQuantization0Pulse
				case 1:
					icdf = icdfExcitationSignVoicedSignalLowQuantization1Pulse
				case 2:
					icdf = icdfExcitationSignVoicedSignalLowQuantization2Pulse
				case 3:
					icdf = icdfExcitationSignVoicedSignalLowQuantization3Pulse
				case 4:
					icdf = icdfExcitationSignVoicedSignalLowQuantization4Pulse
				case 5:
					icdf = icdfExcitationSignVoicedSignalLowQuantization5Pulse
				default:
					icdf = icdfExcitationSignVoicedSignalLowQuantization6PlusPulse
				}
			case frameQuantizationOffsetTypeHigh:
				switch pulsecount {
				case 0:
					icdf = icdfExcitationSignVoicedSignalHighQuantization0Pulse
				case 1:
					icdf = icdfExcitationSignVoicedSignalHighQuantization1Pulse
				case 2:
					icdf = icdfExcitationSignVoicedSignalHighQuantization2Pulse
				case 3:
					icdf = icdfExcitationSignVoicedSignalHighQuantization3Pulse
				case 4:
					icdf = icdfExcitationSignVoicedSignalHighQuantization4Pulse
				case 5:
					icdf = icdfExcitationSignVoicedSignalHighQuantization5Pulse
				default:
					icdf = icdfExcitationSignVoicedSignalHighQuantization6PlusPulse
				}
			}
		}

		// If the value decoded is 0, then the coefficient magnitude is negated.
		// Otherwise, it remains positive.
		if d.rangeDecoder.DecodeSymbolWithICDF(icdf) == 0 {
			eRaw[i] *= -1
		}
	}

}

// SILK codes the excitation using a modified version of the Pyramid
// Vector Quantizer (PVQ) codebook [PVQ].  The PVQ codebook is designed
// for Laplace-distributed values and consists of all sums of K signed,
// unit pulses in a vector of dimension N, where two pulses at the same
// position are required to have the same sign.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8
func (d *Decoder) decodeExcitation(signalType frameSignalType, quantizationOffsetType frameQuantizationOffsetType, seed uint32, pulsecounts, lsbcounts []uint8) (eQ23 []int32) {
	// After the signs have been read, there is enough information to
	// reconstruct the complete excitation signal.  This requires adding a
	// constant quantization offset to each non-zero sample and then
	// pseudorandomly inverting and offsetting every sample.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.6

	// The constant quantization offset varies depending on the signal type and
	// quantization offset type

	// +-------------+--------------------------+--------------------------+
	// | Signal Type | Quantization Offset Type |      Quantization Offset |
	// |             |                          |                    (Q23) |
	// +-------------+--------------------------+--------------------------+
	// | Inactive    | Low                      |                       25 |
	// |             |                          |                          |
	// | Inactive    | High                     |                       60 |
	// |             |                          |                          |
	// | Unvoiced    | Low                      |                       25 |
	// |             |                          |                          |
	// | Unvoiced    | High                     |                       60 |
	// |             |                          |                          |
	// | Voiced      | Low                      |                        8 |
	// |             |                          |                          |
	// | Voiced      | High                     |                       25 |
	// +-------------+--------------------------+--------------------------+
	// Table 53: Excitation Quantization Offsets
	var offsetQ23 int32
	switch {
	case signalType == frameSignalTypeInactive && quantizationOffsetType == frameQuantizationOffsetTypeLow:
		offsetQ23 = 25
	case signalType == frameSignalTypeInactive && quantizationOffsetType == frameQuantizationOffsetTypeHigh:
		offsetQ23 = 60
	case signalType == frameSignalTypeUnvoiced && quantizationOffsetType == frameQuantizationOffsetTypeLow:
		offsetQ23 = 25
	case signalType == frameSignalTypeUnvoiced && quantizationOffsetType == frameQuantizationOffsetTypeHigh:
		offsetQ23 = 25
	case signalType == frameSignalTypeVoiced && quantizationOffsetType == frameQuantizationOffsetTypeLow:
		offsetQ23 = 8
	case signalType == frameSignalTypeVoiced && quantizationOffsetType == frameQuantizationOffsetTypeHigh:
		offsetQ23 = 25
	}

	// Let e_raw[i] be the raw excitation value at position i,
	// with a magnitude composed of the pulses at that location (see Section 4.2.7.8.3)
	eRaw := d.decodePulseLocation(pulsecounts)

	// combined with any additional LSBs (see Section 4.2.7.8.4),
	d.decodeExcitationLSB(eRaw, lsbcounts)

	// and with the corresponding sign decoded in Section 4.2.7.8.5.
	d.decodeExcitationSign(eRaw, signalType, quantizationOffsetType, pulsecounts)

	eQ23 = make([]int32, len(eRaw))
	for i := 0; i < len(eRaw); i++ {
		// Additionally, let seed be the current pseudorandom seed, which is initialized to the
		// value decoded from Section 4.2.7.7 for the first sample in the current SILK frame, and
		// updated for each subsequent sample according to the procedure below.
		// Finally, let offset_Q23 be the quantization offset from Table 53.
		// Then the following procedure produces the final reconstructed
		// excitation value, e_Q23[i]:

		//      e_Q23[i] = (e_raw[i] << 8) - sign(e_raw[i])*20 + offset_Q23;
		//          seed = (196314165*seed + 907633515) & 0xFFFFFFFF;
		//      e_Q23[i] = (seed & 0x80000000) ? -e_Q23[i] : e_Q23[i];
		//          seed = (seed + e_raw[i]) & 0xFFFFFFFF;

		// When e_raw[i] is zero, sign() returns 0 by the definition in
		// Section 1.1.4, so the factor of 20 does not get added.  The final
		// e_Q23[i] value may require more than 16 bits per sample, but it will
		// not require more than 23, including the sign.

		eQ23[i] = (eRaw[i] << 8) - int32(sign(int(eRaw[i])))*20 + offsetQ23
		seed = (196314165*seed + 907633515) & 0xFFFFFFFF
		if seed&0x80000000 != 0 {
			eQ23[i] *= -1
		}
		seed = (seed + uint32(eRaw[i])) & 0xFFFFFFFF
	}

	return
}

// The PDF to use is chosen by the size of the current partition (16, 8, 4, or 2) and the
// number of pulses in the partition (1 to 16, inclusive)
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.3
func (d *Decoder) partitionPulseCount(icdf [][]uint, block uint8, halves []uint8) {
	// This process skips partitions without any pulses, i.e., where
	// the initial pulse count from Section 4.2.7.8.2 was zero, or where the
	// split in the prior level indicated that all of the pulses fell on the
	// other side.  These partitions have nothing to code, so they require
	// no PDF.
	if block == 0 {
		halves[0] = 0
		halves[1] = 0
	} else {
		halves[0] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdf[block-1]))
		halves[1] = block - halves[0]
	}
}

// The a32_Q17[] coefficients are too large to fit in a 16-bit value,
// which significantly increases the cost of applying this filter in
// fixed-point decoders.  Reducing them to Q12 precision doesn't incur
// any significant quality loss, but still does not guarantee they will
// fit.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.7
func (d *Decoder) limitLPCCoefficientsRange(a32Q17 []int32) {
	bandwidthExpansionRound := 0
	for ; bandwidthExpansionRound < 10; bandwidthExpansionRound++ {

		// For each round, the process first finds the index k such that
		// abs(a32_Q17[k]) is largest, breaking ties by choosing the lowest
		// value of k.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.7
		maxabsQ17K := uint(0)
		maxabsQ17 := uint(0)

		for k, val := range a32Q17 {
			abs := int32(sign(int(val))) * val
			if maxabsQ17 < uint(abs) {
				maxabsQ17K = uint(k)
				maxabsQ17 = uint(abs)
			}
		}

		// Then, it computes the corresponding Q12 precision value,
		// maxabs_Q12, subject to an upper bound to avoid overflow in subsequent
		// computations:
		//
		//    maxabs_Q12 = min((maxabs_Q17 + 16) >> 5, 163838)
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.7

		maxabsQ12 := minUint((maxabsQ17+16)>>5, 163838)

		// If this is larger than 32767, the procedure derives the chirp factor,
		// sc_Q16[0], to use in the bandwidth expansion as
		//
		//                       (maxabs_Q12 - 32767) << 14
		//   sc_Q16[0] = 65470 - --------------------------
		//                       (maxabs_Q12 * (k+1)) >> 2
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.7
		if maxabsQ12 > 32767 {
			scQ16 := make([]uint, len(a32Q17))

			scQ16[0] = uint(65470)
			scQ16[0] -= ((maxabsQ12 - 32767) << 14) / ((maxabsQ12 * (maxabsQ17K + 1)) >> 2)

			// silk_bwexpander_32() (bwexpander_32.c) performs the bandwidth
			// expansion (again, only when maxabs_Q12 is greater than 32767) using
			// the following recurrence:
			//
			//            a32_Q17[k] = (a32_Q17[k]*sc_Q16[k]) >> 16
			//
			//           sc_Q16[k+1] = (sc_Q16[0]*sc_Q16[k] + 32768) >> 16
			//
			// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.7
			for k := 0; k < len(a32Q17); k++ {
				a32Q17[k] = (a32Q17[k] * int32(scQ16[k])) >> 16
				if len(scQ16) <= k {
					scQ16[k+1] = (scQ16[0]*scQ16[k] + 32768) >> 16
				}
			}
		} else {
			break
		}
	}

	// After 10 rounds of bandwidth expansion are performed, they are simply
	// saturated to 16 bits:
	//
	//     a32_Q17[k] = clamp(-32768, (a32_Q17[k] + 16) >> 5, 32767) << 5
	//
	// Because this performs the actual saturation in the Q12 domain, but
	// saturation is not performed if maxabs_Q12 drops to 32767 or less
	// prior to the 10th round.
	if bandwidthExpansionRound == 9 {
		for k := 0; k < len(a32Q17); k++ {
			a32Q17[k] = clamp(-32768, (a32Q17[k]+16)>>5, 32767) << 5
		}
	}
}

// The prediction gain of an LPC synthesis filter is the square root of
// the output energy when the filter is excited by a unit-energy
// impulse.  Even if the Q12 coefficients would fit, the resulting
// filter may still have a significant gain (especially for voiced
// sounds), making the filter unstable. silk_NLSF2A() applies up to 16
// additional rounds of bandwidth expansion to limit the prediction
// gain.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.8
func (d *Decoder) limitLPCFilterPredictionGain(a32Q17 []int32) (aQ12 []float32) {
	aQ12 = make([]float32, len(a32Q17))

	// However, silk_LPC_inverse_pred_gain_QA() approximates this using
	// fixed-point arithmetic to guarantee reproducible results across
	// platforms and implementations.  Since small changes in the
	// coefficients can make a stable filter unstable, it takes the real Q12
	// coefficients that will be used during reconstruction as input.  Thus,
	// let
	//
	//     a32_Q12[n] = (a32_Q17[n] + 16) >> 5
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.8
	for n := range a32Q17 {
		aQ12[n] = float32((a32Q17[n] + 16) >> 5)
	}

	return
}

// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.1
func (d *Decoder) decodePitchLags(signalType frameSignalType, bandwidth Bandwidth) (lag uint32, pitchLags []int) {
	if signalType != frameSignalTypeVoiced {
		return
	}

	var (
		lagMin uint32
		lagMax uint32
	)

	// The primary lag index is coded either relative to the primary lag of
	// the prior frame in the same channel or as an absolute index.
	// Absolute coding is used if and only if
	//
	// *  This is the first SILK frame of its type (LBRR or regular) for
	//    this channel in the current Opus frame,
	//
	// *  The previous SILK frame of the same type (LBRR or regular) for
	//    this channel in the same Opus frame was not coded, or
	//
	// *  That previous SILK frame was coded, but was not voiced (see
	//    Section 4.2.7.3).

	lagAbsolute := true
	if lagAbsolute {
		// With absolute coding, the primary pitch lag may range from 2 ms
		// (inclusive) up to 18 ms (exclusive), corresponding to pitches from
		// 500 Hz down to 55.6 Hz, respectively.  It is comprised of a high part
		// and a low part, where the decoder first reads the high part using the
		// 32-entry codebook in Table 29 and then the low part using the
		// codebook corresponding to the current audio bandwidth from Table 30.
		//
		//  +------------+------------------------+-------+----------+----------+
		//  | Audio      | PDF                    | Scale | Minimum  | Maximum  |
		//  | Bandwidth  |                        |       | Lag      | Lag      |
		//  +------------+------------------------+-------+----------+----------+
		//  | NB         | {64, 64, 64, 64}/256   | 4     | 16       | 144      |
		//  |            |                        |       |          |          |
		//  | MB         | {43, 42, 43, 43, 42,   | 6     | 24       | 216      |
		//  |            | 43}/256                |       |          |          |
		//  |            |                        |       |          |          |
		//  | WB         | {32, 32, 32, 32, 32,   | 8     | 32       | 288      |
		//  |            | 32, 32, 32}/256        |       |          |          |
		//  +------------+------------------------+-------+----------+----------+

		// Table 30: PDF for Low Part of Primary Pitch Lag
		var (
			lowPartICDF []uint
			lagScale    uint32
		)
		switch bandwidth {
		case BandwidthNarrowband:
			lowPartICDF = icdfPrimaryPitchLagLowPartNarrowband
			lagScale = 4
			lagMin = 16
			lagMax = 144
		case BandwidthMediumband:
			lowPartICDF = icdfPrimaryPitchLagLowPartMediumband
			lagScale = 6
			lagMin = 24
			lagMax = 216
		case BandwidthWideband:
			lowPartICDF = icdfPrimaryPitchLagLowPartWideband
			lagScale = 8
			lagMin = 32
			lagMax = 288
		}

		lagHigh := d.rangeDecoder.DecodeSymbolWithICDF(icdfPrimaryPitchLagHighPart)
		lagLow := d.rangeDecoder.DecodeSymbolWithICDF(lowPartICDF)

		// The final primary pitch lag is then
		//
		//              lag = lag_high*lag_scale + lag_low + lag_min
		//
		// where lag_high is the high part, lag_low is the low part, and
		// lag_scale and lag_min are the values from the "Scale" and "Minimum
		// Lag" columns of Table 30, respectively.
		lag = lagHigh*lagScale + lagLow + lagMin
	} else {
		// TODO
	}

	// After the primary pitch lag, a "pitch contour", stored as a single
	// entry from one of four small VQ codebooks, gives lag offsets for each
	// subframe in the current SILK frame.  The codebook index is decoded
	// using one of the PDFs in Table 32 depending on the current frame size
	// and audio bandwidth.  Tables 33 through 36 give the corresponding
	// offsets to apply to the primary pitch lag for each subframe given the
	// decoded codebook index.
	//
	// +-----------+--------+----------+-----------------------------------+
	// | Audio     | SILK   | Codebook | PDF                               |
	// | Bandwidth | Frame  |     Size |                                   |
	// |           | Size   |          |                                   |
	// +-----------+--------+----------+-----------------------------------+
	// | NB        | 10 ms  |        3 | {143, 50, 63}/256                 |
	// |           |        |          |                                   |
	// | NB        | 20 ms  |       11 | {68, 12, 21, 17, 19, 22, 30, 24,  |
	// |           |        |          | 17, 16, 10}/256                   |
	// |           |        |          |                                   |
	// | MB or WB  | 10 ms  |       12 | {91, 46, 39, 19, 14, 12, 8, 7, 6, |
	// |           |        |          | 5, 5, 4}/256                      |
	// |           |        |          |                                   |
	// | MB or WB  | 20 ms  |       34 | {33, 22, 18, 16, 15, 14, 14, 13,  |
	// |           |        |          | 13, 10, 9, 9, 8, 6, 6, 6, 5, 4,   |
	// |           |        |          | 4, 4, 3, 3, 3, 2, 2, 2, 2, 2, 2,  |
	// |           |        |          | 2, 1, 1, 1, 1}/256                |
	// +-----------+--------+----------+-----------------------------------+
	//
	// Table 32: PDFs for Subframe Pitch Contour

	// The final pitch lag for each subframe is assembled in
	// silk_decode_pitch() (decode_pitch.c).  Let lag be the primary pitch
	// lag for the current SILK frame, contour_index be index of the VQ
	// codebook, and lag_cb[contour_index][k] be the corresponding entry of
	// the codebook from the appropriate table given above for the k'th
	// subframe.

	var (
		lagCb   [][]int8
		lagIcdf []uint
	)

	switch bandwidth {
	case BandwidthNarrowband:
		lagCb = codebookSubframePitchCounterNarrowband20Ms
		lagIcdf = icdfSubframePitchContourNarrowband20Ms
	case BandwidthMediumband, BandwidthWideband:
		lagCb = codebookSubframePitchCounterMediumbandOrWideband20Ms
		lagIcdf = icdfSubframePitchContourMediumbandOrWideband20Ms
	}

	contourIndex := d.rangeDecoder.DecodeSymbolWithICDF(lagIcdf)

	// Then the final pitch lag for that subframe is
	//
	//     pitch_lags[k] = clamp(lag_min, lag + lag_cb[contour_index][k],
	//                           lag_max)
	pitchLags = make([]int, subframeCount)
	for i := 0; i < subframeCount; i++ {
		pitchLags[i] = int(clamp(
			int32(lagMin),
			int32(lag+uint32(lagCb[contourIndex][i])),
			int32(lagMax)),
		)
	}

	return
}

// This allows the encoder to trade off the prediction gain between
// packets against the recovery time after packet loss.
//
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.3
func (d *Decoder) decodeLTPScalingParamater(signalType frameSignalType) (LTPscaleQ14 float32) {
	// An LTP scaling parameter appears after the LTP filter coefficients if
	// and only if
	//
	// o  This is a voiced frame (see Section 4.2.7.3), and
	// o  Either
	//    *  This SILK frame corresponds to the first time interval of the
	//       current Opus frame for its type (LBRR or regular), or
	//
	//    *  This is an LBRR frame where the LBRR flags (see Section 4.2.4)
	//       indicate the previous LBRR frame in the same channel is not
	//       coded.

	// Frames that do not code the scaling parameter
	//    use the default factor of 15565 (approximately 0.95).
	if signalType != frameSignalTypeVoiced {
		return 15565.0
	}

	// The three possible values represent Q14 scale factors of
	// 15565, 12288, and 8192, respectively (corresponding to approximately
	// 0.95, 0.75, and 0.5)
	scaleFactorIndex := d.rangeDecoder.DecodeSymbolWithICDF(icdfLTPScalingParameter)
	switch scaleFactorIndex {
	case 0:
		return 15565.0
	case 1:
		return 12288.0
	case 2:
		return 8192.0
	}

	return 0
}

// SILK uses a separate 5-tap pitch filter for each subframe, selected
// from one of three codebooks.
//
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.2
func (d *Decoder) decodeLTPFilterCoefficients(signalType frameSignalType) (bQ7 [][]int8) {
	if signalType != frameSignalTypeVoiced {
		return
	}

	bQ7 = [][]int8{
		make([]int8, 5),
		make([]int8, 5),
		make([]int8, 5),
		make([]int8, 5),
	}

	// This is signaled with an explicitly-coded "periodicity index".  This
	// immediately follows the subframe pitch lags, and is coded using the
	// 3-entry PDF from Table 37.
	periodicityIndex := d.rangeDecoder.DecodeSymbolWithICDF(icdfPeriodicityIndex)

	// The indices of the filters for each subframe follow.  They are all
	// coded using the PDF from Table 38 corresponding to the periodicity
	// index.  Tables 39 through 41 contain the corresponding filter taps as
	// signed Q7 integers.
	for i := 0; i < subframeCount; i++ {
		var filterIndiceIcdf []uint
		switch periodicityIndex {
		case 0:
			filterIndiceIcdf = icdfLTPFilterIndex0
		case 1:
			filterIndiceIcdf = icdfLTPFilterIndex1
		case 2:
			filterIndiceIcdf = icdfLTPFilterIndex2
		}

		filterIndex := d.rangeDecoder.DecodeSymbolWithICDF(filterIndiceIcdf)
		var LTPFilterCodebook [][]int8

		switch periodicityIndex {
		case 0:
			LTPFilterCodebook = codebookLTPFilterPeriodicityIndex0
		case 1:
			LTPFilterCodebook = codebookLTPFilterPeriodicityIndex1
		case 2:
			LTPFilterCodebook = codebookLTPFilterPeriodicityIndex2

		}

		copy(bQ7[i], LTPFilterCodebook[filterIndex])
	}
	return
}

// let n be the number of samples in a subframe (40 for NB, 60 for
// MB, and 80 for WB)
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
func (d *Decoder) samplesInSubframe(bandwidth Bandwidth) int {
	switch bandwidth {
	case BandwidthNarrowband:
		return 40
	case BandwidthMediumband:
		return 60
	case BandwidthWideband:
		return 80
	}

	return 0
}

// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.1
func (d *Decoder) ltpSynthesis(
	out []float32,
	signalType frameSignalType,
	bQ7 [][]int8,
	pitchLags []int,
	eQ23 []int32,
	n, j, s, dLPC int,
	LTPScaleQ14 float32,
	bandwidth Bandwidth,
	wQ2 int16,
	aQ12, gainQ16, lpc []float32,
) (res []float32) {
	// For unvoiced frames (see Section 4.2.7.3), the LPC residual for i
	// such that j <= i < (j + n) is simply a normalized copy of the
	// excitation signal, i.e.,
	//
	//               e_Q23[i]
	//     res[i] = ---------
	//               2.0**23

	res = make([]float32, len(eQ23))
	if signalType != frameSignalTypeVoiced {
		for i := j; i < (j + n); i++ {
			res[i] = float32(eQ23[i]) / 8388608
		}
		return
	}

	// Voiced SILK frames, on the other hand, pass the excitation through an
	// LTP filter using the parameters decoded in Section 4.2.7.6 to produce
	// an LPC residual.
	for i := range res {
		res[i] = float32(eQ23[i]) / 8388608.0
	}

	// Voiced SILK frames, on the other hand, pass the excitation through an
	// LTP filter using the parameters decoded in Section 4.2.7.6 to produce
	// an LPC residual.

	// If this is the third or fourth subframe of a 20 ms SILK frame and the LSF
	// interpolation factor, w_Q2 (see Section 4.2.7.5.5), is less than 4,
	// then let out_end be set to (j - (s-2)*n) and let LTP_scale_Q14 be set
	// to 16384.  Otherwise, set out_end to (j - s*n) and set LTP_scale_Q14
	// to the Q14 LTP scaling value from Section 4.2.7.6.3.
	var out_end int
	if s > 2 || wQ2 < 4 {
		out_end = (j - (s-2)*n)
		LTPScaleQ14 = 16386.0
	} else {
		out_end = (j - s*n)
	}

	// out[i] and lpc[i] are initially cleared to all zeros. Then, for i
	// such that (j - pitch_lags[s] - 2) <= i < out_end, out[i] is
	// rewhitened into an LPC residual, res[i], via
	//
	//              4.0*LTP_scale_Q14
	//     res[i] = ----------------- * clamp(-1.0,
	//                 gain_Q16[s]
	//                                        d_LPC-1
	//                                          __              a_Q12[k]
	//                                 out[i] - \  out[i-k-1] * --------, 1.0)
	//                                          /_               4096.0
	//                                          k=0
	var outVal float32
	for i := (j - pitchLags[s] - 2); i < out_end; i++ {
		index := i + j
		if index < 0 || index >= len(res) || index >= len(out) {
			continue
		}

		res[index] = out[index]
		for k := 0; k < dLPC; k++ {
			if index-k > 0 {
				outVal = out[index-k-1]
			} else {
				outVal = 0
			}

			res[index] -= outVal * (aQ12[k] / 4096.0)
		}
		res[index] = clampFloat(-1.0, res[index], 1.0)
		res[index] *= (4.0 / LTPScaleQ14) / gainQ16[s]
	}

	// Then, for i such that
	// out_end <= i < j, lpc[i] is rewhitened into an LPC residual, res[i],
	// via
	//
	//                                      d_LPC-1
	//                  65536.0               __              a_Q12[k]
	//       res[i] = ----------- * (lpc[i] - \  lpc[i-k-1] * --------)
	//                gain_Q16[s]             /_               4096.0
	//                                        k=0
	//
	// This requires storage to buffer up to 256 values of lpc[i] from
	// previous subframes (240 from the current SILK frame and 16 from the
	// previous SILK frame).  This corresponds to WB with up to three
	// previous subframes in the current SILK frame, plus 16 samples for
	// d_LPC.  The astute reader will notice that, given the definition of
	// lpc[i] in Section 4.2.7.9.2, the output of this latter equation is
	// merely a scaled version of the values of res[i] from previous
	// subframes.
	var lpcVal float32
	for i := out_end; i < j; i++ {
		index := i + j
		if index < 0 || index >= len(res) {
			continue
		}

		res[index] = 0
		for k := 0; k < dLPC; k++ {
			if i-k > 0 {
				lpcVal = lpc[index-k-1]
			} else {
				lpcVal = d.finalLPCValues[len(d.finalLPCValues)-1+(i-k)]
			}
			res[index] += lpcVal * (aQ12[k] / 4096.0)
		}

		res[index] = lpc[index] - res[index]
		res[index] *= (65536.0 / gainQ16[s])
	}

	// Let e_Q23[i] for j <= i < (j + n) be the excitation for the current
	// subframe, and b_Q7[k] for 0 <= k < 5 be the coefficients of the LTP
	// filter taken from the codebook entry in one of Tables 39 through 41
	// corresponding to the index decoded for the current subframe in
	// Section 4.2.7.6.2.  Then for i such that j <= i < (j + n), the LPC
	// residual is

	//                          4
	//              e_Q23[i]   __                                  b_Q7[k]
	//    res[i] = --------- + \  res[i - pitch_lags[s] + 2 - k] * -------
	//              2.0**23    /_                                   128.0
	//                         k=0
	var resSum, resVal float32
	for i := j; i < (j + n); i++ {
		index := i + j
		if index < 0 || index >= len(res) {
			continue
		}

		resSum = 0
		for k := 0; k <= 4; k++ {
			resValIndex := index - pitchLags[s] + 2 - k
			if resValIndex < 0 || resValIndex >= len(res) {
				resVal = 0
			} else {
				resVal = res[resValIndex] * (float32(bQ7[s][k]) / 128.0)

			}

			resSum += resVal
		}

		res[index] = (float32(eQ23[i]) / 8388608.0) + resSum
	}

	return
}

// LPC synthesis uses the short-term LPC filter to predict the next
// output coefficient.  For i such that (j - d_LPC) <= i < j, let lpc[i]
// be the result of LPC synthesis from the last d_LPC samples of the
// previous subframe or zeros in the first subframe for this channel
// after either
//
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.2
func (d *Decoder) lpcSynthesis(out []float32, bandwidth Bandwidth, n, s, dLPC int, aQ12, res, gainQ16, lpc []float32) {
	finalLPCValuesIndex := 0

	// j be the index of the first sample in the residual corresponding to
	// the current subframe.
	j := 0

	//Then, for i such that j <= i < (j + n), the result of LPC synthesis
	//for the current subframe is
	//
	//                                     d_LPC-1
	//                gain_Q16[i]            __              a_Q12[k]
	//       lpc[i] = ----------- * res[i] + \  lpc[i-k-1] * --------
	//                  65536.0              /_               4096.0
	//                                       k=0
	//
	var currentLPCVal float32
	for i := j; i < (j + n); i++ {
		sampleIndex := i + (n * s)

		lpcVal := gainQ16[s] / 65536.0
		lpcVal *= res[sampleIndex]

		for k := 0; k < dLPC; k++ {
			if i-k > 0 {
				currentLPCVal = lpc[sampleIndex-k-1]
			} else {
				currentLPCVal = d.finalLPCValues[len(d.finalLPCValues)-1+(i-k)]
			}

			lpcVal += currentLPCVal * (aQ12[k] / 4096.0)
		}

		lpc[sampleIndex] = lpcVal

		// The decoder saves the final d_LPC values, i.e., lpc[i] such that
		// (j + n - d_LPC) <= i < (j + n), to feed into the LPC synthesis of the
		// next subframe.  This requires storage for up to 16 values of lpc[i]
		// (for WB frames).
		if (j+n-dLPC) <= i && i < (j+n) {
			d.finalLPCValues[finalLPCValuesIndex] = lpcVal
			finalLPCValuesIndex++
		}

		// Then, the signal is clamped into the final nominal range:
		//
		//     out[i] = clamp(-1.0, lpc[i], 1.0)
		//
		out[i] = clampFloat(-1.0, lpc[sampleIndex], 1.0)
	}
}

// The remainder of the reconstruction process for the frame does not
// need to be bit-exact, as small errors should only introduce
// proportionally small distortions.  Although the reference
// implementation only includes a fixed-point version of the remaining
// steps, this section describes them in terms of a floating-point
// version for simplicity.  This produces a signal with a nominal range
// of -1.0 to 1.0.
//
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
func (d *Decoder) silkFrameReconstruction(
	signalType frameSignalType, bandwidth Bandwidth,
	dLPC int,
	bQ7 [][]int8,
	pitchLags []int,
	eQ23 []int32,
	LTPscaleQ14 float32,
	wQ2 int16,
	aQ12, gainQ16, out []float32,
) {
	// let n be the number of samples in a subframe
	//
	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
	n := d.samplesInSubframe(bandwidth)

	// let lpc[i] be the result of LPC synthesis from the last d_LPC samples of the
	//  previous subframe or zeros in the first subframe for this channel
	lpc := make([]float32, n*subframeCount)

	// s be the index of the current subframe in this SILK frame
	// (0 or 1 for 10 ms frames, or 0 to 3 for 20 ms frames)
	for s := 0; s < subframeCount; s++ {
		// j be the index of the first sample in the residual corresponding to
		// the current subframe.
		//
		// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
		j := n * s

		// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.1
		res := d.ltpSynthesis(out, signalType, bQ7, pitchLags, eQ23, n, j, s, dLPC, LTPscaleQ14, bandwidth, wQ2, aQ12, gainQ16, lpc)

		//https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.2
		d.lpcSynthesis(out[n*s:], bandwidth, n, s, dLPC, aQ12, res, gainQ16, lpc)
	}
}

// Decode decodes many SILK subframes
//
//	An overview of the decoder is given in Figure 14.
//
//	     +---------+    +------------+
//	  -->| Range   |--->| Decode     |---------------------------+
//	   1 | Decoder | 2  | Parameters |----------+       5        |
//	     +---------+    +------------+     4    |                |
//	                         3 |                |                |
//	                          \/               \/               \/
//	                    +------------+   +------------+   +------------+
//	                    | Generate   |-->| LTP        |-->| LPC        |
//	                    | Excitation |   | Synthesis  |   | Synthesis  |
//	                    +------------+   +------------+   +------------+
//	                                            ^                |
//	                                            |                |
//	                        +-------------------+----------------+
//	                        |                                      6
//	                        |   +------------+   +-------------+
//	                        +-->| Stereo     |-->| Sample Rate |-->
//	                            | Unmixing   | 7 | Conversion  | 8
//	                            +------------+   +-------------+
//
//	  1: Range encoded bitstream
//	  2: Coded parameters
//	  3: Pulses, LSBs, and signs
//	  4: Pitch lags, Long-Term Prediction (LTP) coefficients
//	  5: Linear Predictive Coding (LPC) coefficients and gains
//	  6: Decoded signal (mono or mid-side stereo)
//	  7: Unmixed signal (mono or left-right stereo)
//	  8: Resampled signal
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.1
func (d *Decoder) Decode(in []byte, out []float32, isStereo bool, nanoseconds int, bandwidth Bandwidth) error {
	subframeSize := d.samplesInSubframe(bandwidth)
	switch {
	case nanoseconds != nanoseconds20Ms:
		return errUnsupportedSilkFrameDuration
	case isStereo:
		return errUnsupportedSilkStereo
	case (subframeSize * subframeCount) > len(out):
		return errOutBufferTooSmall
	}

	d.rangeDecoder.Init(in)

	voiceActivityDetected, lowBitRateRedundancy := d.decodeHeaderBits()
	if lowBitRateRedundancy {
		return errUnsupportedSilkLowBitrateRedundancy
	}

	signalType, quantizationOffsetType := d.determineFrameType(voiceActivityDetected)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
	gainQ16 := d.decodeSubframeQuantizations(signalType)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.1
	I1 := d.normalizeLineSpectralFrequencyStageOne(signalType == frameSignalTypeVoiced, bandwidth)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	dLPC, resQ10 := d.normalizeLineSpectralFrequencyStageTwo(bandwidth, I1)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
	nlsfQ15 := d.normalizeLineSpectralFrequencyCoefficients(dLPC, bandwidth, resQ10, I1)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
	d.normalizeLSFStabilization(nlsfQ15)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
	n1Q15, wQ2 := d.normalizeLSFInterpolation(nlsfQ15)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6
	a32Q17 := d.convertNormalizedLSFsToLPCCoefficients(n1Q15, bandwidth)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.5.7
	d.limitLPCCoefficientsRange(a32Q17)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.5.8
	aQ12 := d.limitLPCFilterPredictionGain(a32Q17)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.1
	_, pitchLags := d.decodePitchLags(signalType, bandwidth)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.2
	bQ7 := d.decodeLTPFilterCoefficients(signalType)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.3
	LTPscaleQ14 := d.decodeLTPScalingParamater(signalType)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.7
	lcgSeed := d.decodeLinearCongruentialGeneratorSeed()

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.8
	shellblocks := d.decodeShellblocks(nanoseconds, bandwidth)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.8.1
	rateLevel := d.decodeRatelevel(signalType == frameSignalTypeVoiced)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.8.2
	pulsecounts, lsbcounts := d.decodePulseAndLSBCounts(shellblocks, rateLevel)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.8.6
	eQ23 := d.decodeExcitation(signalType, quantizationOffsetType, lcgSeed, pulsecounts, lsbcounts)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
	d.silkFrameReconstruction(
		signalType, bandwidth,
		dLPC,
		bQ7,
		pitchLags,
		eQ23,
		LTPscaleQ14,
		wQ2,
		aQ12, gainQ16, out,
	)

	// n0Q15 is the LSF coefficients decoded for the prior frame
	// see normalizeLSFInterpolation.
	if len(d.n0Q15) != len(nlsfQ15) {
		d.n0Q15 = make([]int16, len(nlsfQ15))
	}

	copy(d.n0Q15, nlsfQ15)
	d.isPreviousFrameVoiced = signalType == frameSignalTypeVoiced
	d.haveDecoded = true

	return nil
}
