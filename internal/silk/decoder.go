package silk

import (
	"fmt"

	"github.com/pion/opus/internal/rangecoding"
)

// Decoder maintains the state needed to decode a stream
// of Silk frames
type Decoder struct {
	rangeDecoder rangecoding.Decoder

	// Have we decoded a frame yet?
	haveDecoded bool

	// TODO, should have dedicated frame state
	logGain       uint32
	subframeState [4]struct {
		gain float64
	}
}

// NewDecoder creates a new Silk Decoder
func NewDecoder() *Decoder {
	return &Decoder{}
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
	switch frameTypeSymbol {
	case 0:
		signalType = frameSignalTypeInactive
		quantizationOffsetType = frameQuantizationOffsetTypeLow
	case 1:
		signalType = frameSignalTypeInactive
		quantizationOffsetType = frameQuantizationOffsetTypeHigh
	case 2:
		signalType = frameSignalTypeUnvoiced
		quantizationOffsetType = frameQuantizationOffsetTypeLow
	case 3:
		signalType = frameSignalTypeUnvoiced
		quantizationOffsetType = frameQuantizationOffsetTypeHigh
	case 4:
		signalType = frameSignalTypeVoiced
		quantizationOffsetType = frameQuantizationOffsetTypeLow
	case 5:
		signalType = frameSignalTypeVoiced
		quantizationOffsetType = frameQuantizationOffsetTypeHigh
	}

	return
}

// A separate quantization gain is coded for each 5 ms subframe
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
func (d *Decoder) decodeSubframeQuantizations(signalType frameSignalType) {
	var (
		logGain        uint32
		deltaGainIndex uint32
		gainIndex      uint32
	)

	for subframeIndex := 0; subframeIndex < 4; subframeIndex++ {

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
				gainIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBInactive)
			case frameSignalTypeVoiced:
				gainIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBVoiced)
			case frameSignalTypeUnvoiced:
				gainIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBUnvoiced)
			}

			// The 3 least significant bits are decoded using a uniform PDF:
			// These 6 bits are combined to form a value, gain_index, between 0 and 63.
			gainIndex = (gainIndex << 3) | d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainLSB)

			// When the gain for the previous subframe is available, then the
			// current gain is limited as follows:
			//     log_gain = max(gain_index, previous_log_gain - 16)
			if d.haveDecoded {
				logGain = maxUint32(gainIndex, d.logGain-16)
			} else {
				logGain = gainIndex
			}
		} else {
			// For subframes that do not have an independent gain (including the
			// first subframe of frames not listed as using independent coding
			// above), the quantization gain is coded relative to the gain from the
			// previous subframe
			deltaGainIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfDeltaQuantizationGain)

			// The following formula translates this index into a quantization gain
			// for the current subframe using the gain from the previous subframe:
			//      log_gain = clamp(0, max(2*delta_gain_index - 16, previous_log_gain + delta_gain_index - 4), 63)
			logGain = uint32(clamp(0, maxInt32(2*int32(deltaGainIndex)-16, int32(d.logGain+deltaGainIndex)-4), 63))
		}

		d.logGain = logGain

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

		gainQ16 := (1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7)
		d.subframeState[subframeIndex].gain = float64(gainQ16) / 65536
	}
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
func (d *Decoder) normalizeLineSpectralFrequencyStageTwo(bandwidth Bandwidth, I1 uint32) (resQ10 []int16) {
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
	dLPC := len(I2)

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
func (d *Decoder) normalizeLineSpectralFrequencyCoefficients(bandwidth Bandwidth, resQ10 []int16, I1 uint32) (nlsfQ15 []int16) {
	// Let d_LPC be the order of the codebook, i.e., 10 for NB and MB, and 16 for WB
	dLPC := len(resQ10)

	nlsfQ15 = make([]int16, len(resQ10))
	w2Q18 := make([]uint, len(resQ10))
	wQ9 := make([]int16, len(resQ10))

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
func (d *Decoder) normalizeLSFStabilization() {
	// TODO
}

// For 20 ms SILK frames, the first half of the frame (i.e., the first
// two subframes) may use normalized LSF coefficients that are
// interpolated between the decoded LSFs for the most recent coded frame
// (in the same channel) and the current frame
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
func (d *Decoder) normalizeLSFInterpolation() error {
	// Let n2_Q15[k] be the normalized LSF coefficients decoded by the
	// procedure in Section 4.2.7.5, n0_Q15[k] be the LSF coefficients
	// decoded for the prior frame, and w_Q2 be the interpolation factor.
	// Then, the normalized LSF coefficients used for the first half of a
	// 20 ms frame, n1_Q15[k], are
	//
	//      n1_Q15[k] = n0_Q15[k] + (w_Q2*(n2_Q15[k] - n0_Q15[k]) >> 2)
	if wQ2 := d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex); wQ2 != 4 {
		return errUnsupportedLSFInterpolation
	}

	return nil
}

func (d *Decoder) convertNormalizedLSFsToLPCCoefficients(I1 uint32, nlsfQ1 []int16, bandwidth Bandwidth) {
	cQ17 := make([]int32, len(nlsfQ1))
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
	for k := range nlsfQ1 {
		i := int32(nlsfQ1[k] >> 8)
		f := int32(nlsfQ1[k] & 255)

		cQ17[ordering[k]] = (cosQ12[i]*256 +
			(cosQ12[i+1]-cosQ12[i])*f + 4) >> 3
	}

	fmt.Println(cQ17)
	panic("")
}

// Decode decodes many SILK subframes
//   An overview of the decoder is given in Figure 14.
//
//        +---------+    +------------+
//     -->| Range   |--->| Decode     |---------------------------+
//      1 | Decoder | 2  | Parameters |----------+       5        |
//        +---------+    +------------+     4    |                |
//                            3 |                |                |
//                             \/               \/               \/
//                       +------------+   +------------+   +------------+
//                       | Generate   |-->| LTP        |-->| LPC        |
//                       | Excitation |   | Synthesis  |   | Synthesis  |
//                       +------------+   +------------+   +------------+
//                                               ^                |
//                                               |                |
//                           +-------------------+----------------+
//                           |                                      6
//                           |   +------------+   +-------------+
//                           +-->| Stereo     |-->| Sample Rate |-->
//                               | Unmixing   | 7 | Conversion  | 8
//                               +------------+   +-------------+
//
//     1: Range encoded bitstream
//     2: Coded parameters
//     3: Pulses, LSBs, and signs
//     4: Pitch lags, Long-Term Prediction (LTP) coefficients
//     5: Linear Predictive Coding (LPC) coefficients and gains
//     6: Decoded signal (mono or mid-side stereo)
//     7: Unmixed signal (mono or left-right stereo)
//     8: Resampled signal
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.1
func (d *Decoder) Decode(in []byte, isStereo bool, nanoseconds int, bandwidth Bandwidth) (decoded []byte, err error) {
	if nanoseconds != nanoseconds20Ms {
		return nil, errUnsupportedSilkFrameDuration
	} else if isStereo {
		return nil, errUnsupportedSilkStereo
	}

	d.rangeDecoder.Init(in)

	//The LP layer begins with two to eight header bits These consist of one
	// Voice Activity Detection (VAD) bit per frame (up to 3), followed by a
	// single flag indicating the presence of LBRR frames.
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.3
	voiceActivityDetected := d.rangeDecoder.DecodeSymbolLogP(1) == 1
	lowBitRateRedundancy := d.rangeDecoder.DecodeSymbolLogP(1) == 1
	if lowBitRateRedundancy {
		return nil, errUnsupportedSilkLowBitrateRedundancy
	}

	signalType, _ := d.determineFrameType(voiceActivityDetected)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
	d.decodeSubframeQuantizations(signalType)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.1
	I1 := d.normalizeLineSpectralFrequencyStageOne(voiceActivityDetected, bandwidth)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	resQ10 := d.normalizeLineSpectralFrequencyStageTwo(bandwidth, I1)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
	nlsfQ1 := d.normalizeLineSpectralFrequencyCoefficients(bandwidth, resQ10, I1)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
	d.normalizeLSFStabilization()

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
	if err := d.normalizeLSFInterpolation(); err != nil {
		return nil, err
	}

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6
	d.convertNormalizedLSFsToLPCCoefficients(I1, nlsfQ1, bandwidth)

	return
}
