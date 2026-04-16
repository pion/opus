// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"slices"

	"github.com/pion/opus/internal/rangecoding"
)

// Decoder maintains the state needed to decode a stream
// of Silk frames.
type Decoder struct {
	rangeDecoder rangecoding.Decoder
	sideDecoder  *Decoder

	// SILK resets its per-channel prediction state whenever the internal
	// decoder rate changes between NB, MB, and WB.
	previousBandwidth Bandwidth

	// Have we decoded a frame yet?
	haveDecoded bool

	// Is the previous frame a voiced frame?
	isPreviousFrameVoiced bool
	previousLag           int

	previousLogGain int32

	//  The decoder saves the final d_LPC values, i.e., lpc[i] such that
	// (j + n - d_LPC) <= i < (j + n), to feed into the LPC synthesis of the
	// next subframe.  This requires storage for up to 16 values of lpc[i]
	// (for WB frames).
	previousFrameLPCValues []float32

	// This requires storage to buffer up to 306 values of out[i] from
	// previous subframes.
	// https://www.rfc-editor.org/rfc/rfc6716#section-4.2.7.9.1
	finalOutValues []float32

	previousGainQ16 int32
	sLPCQ14Buf      [maxSilkLPCOrder]int32
	outBuf          [maxSilkOutBufferLength]int16

	// n0Q15 are the LSF coefficients decoded for the prior frame
	// see normalizeLSFInterpolation
	n0Q15 []int16

	previousStereoWeights [2]int32
	previousMidValues     [2]float32
	previousSideValue     float32
	previousDecodeOnlyMid bool
	wasStereo             bool
	stereoMid             []float32
	stereoSide            []float32
}

// NewDecoder creates a new Silk Decoder.
func NewDecoder() Decoder {
	return Decoder{
		sideDecoder:     newChannelDecoder(),
		finalOutValues:  make([]float32, 306),
		previousGainQ16: 65536,
	}
}

func newChannelDecoder() *Decoder {
	return &Decoder{
		finalOutValues:  make([]float32, 306),
		previousGainQ16: 65536,
	}
}

func (d *Decoder) resetPredictionState() {
	d.haveDecoded = false
	d.isPreviousFrameVoiced = false
	d.previousLag = 100
	d.previousLogGain = 10
	d.previousFrameLPCValues = nil
	clear(d.finalOutValues)
	d.previousGainQ16 = 65536
	clear(d.sLPCQ14Buf[:])
	clear(d.outBuf[:])
	d.n0Q15 = nil
}

// RFC 6716 Sections 4.2.7.4, 4.2.7.5.5, and 4.2.7.6.1 require the side
// channel to restart gain, LSF, and pitch prediction after an uncoded frame.
func (d *Decoder) resetSideDecoderPrediction() {
	if d.sideDecoder == nil {
		d.sideDecoder = newChannelDecoder()
	}

	d.sideDecoder.resetPredictionState()
}

// silk_decoder_set_fs() in the RFC 6716 reference implementation resets the
// predictor history whenever the internal SILK rate changes. The normative
// predictor dependencies are described in Sections 4.2.7.4, 4.2.7.5.5, and
// 4.2.7.6.1, so carrying them across NB/MB/WB switches changes later frames.
func (d *Decoder) resetPredictionForBandwidthChange(bandwidth Bandwidth) {
	if d.previousBandwidth != 0 && d.previousBandwidth != bandwidth {
		d.resetPredictionState()
	}
	d.previousBandwidth = bandwidth
}

// The LP layer begins with two to eight header bits These consist of one
// Voice Activity Detection (VAD) bit per frame (up to 3), followed by a
// single flag indicating the presence of LBRR frames.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.3
func (d *Decoder) decodeHeaderBits(frameCount int) (voiceActivityDetected []bool, lowBitRateRedundancy bool) {
	voiceActivityDetected = make([]bool, frameCount)
	for i := range frameCount {
		voiceActivityDetected[i] = d.rangeDecoder.DecodeSymbolLogP(1) == 1
	}
	lowBitRateRedundancy = d.rangeDecoder.DecodeSymbolLogP(1) == 1

	return
}

// decodeLowBitrateRedundancyFlags expands RFC 6716 Section 4.2.4's global
// LBRR-present bit into one flag per SILK frame.
func (d *Decoder) decodeLowBitrateRedundancyFlags(frameCount int, present bool) []bool {
	flags := make([]bool, frameCount)
	if !present {
		return flags
	}

	switch frameCount {
	case 1:
		flags[0] = true
	case 2:
		d.decodeLowBitrateRedundancyFlagSymbol(flags, icdfLowBitrateRedundancyFlags40Ms)
	case 3:
		d.decodeLowBitrateRedundancyFlagSymbol(flags, icdfLowBitrateRedundancyFlags60Ms)
	}

	return flags
}

// decodeLowBitrateRedundancyFlagSymbol decodes the Table 4 bitmap symbol used
// for 40 ms and 60 ms SILK packets.
func (d *Decoder) decodeLowBitrateRedundancyFlagSymbol(flags []bool, icdf []uint) {
	symbol := d.rangeDecoder.DecodeSymbolWithICDF(icdf)
	for i := range flags {
		flags[i] = symbol&(1<<i) != 0
	}
}

// RFC 6716 Table 7 contains the mid-side stereo prediction weights.
var stereoWeightsQ13 = []int32{ // nolint:gochecknoglobals
	-13732, -10050, -8266, -7526, -6500, -5000, -2950, -820,
	820, 2950, 5000, 6500, 7526, 8266, 10050, 13732,
}

// RFC 6716 Section 4.2.7.1 decodes mid-side stereo prediction weights.
func (d *Decoder) decodeStereoPredictionWeights() (w0Q13, w1Q13 int32) {
	n := int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfStereoWeightsStageOne))    // #nosec G115
	i0 := int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfStereoWeightsStageTwo))   // #nosec G115
	i1 := int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfStereoWeightsStageThree)) // #nosec G115
	i2 := int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfStereoWeightsStageTwo))   // #nosec G115
	i3 := int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfStereoWeightsStageThree)) // #nosec G115

	return stereoPredictionWeights(n, i0, i1, i2, i3)
}

// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.1
func stereoPredictionWeights(n, i0, i1, i2, i3 int32) (w0Q13, w1Q13 int32) {
	wi0 := i0 + 3*(n/5)
	wi1 := i2 + 3*(n%5)

	w1Q13 = stereoWeightsQ13[wi1] +
		(((stereoWeightsQ13[wi1+1]-stereoWeightsQ13[wi1])*6554)>>16)*(2*i3+1)
	w0Q13 = stereoWeightsQ13[wi0] +
		(((stereoWeightsQ13[wi0+1]-stereoWeightsQ13[wi0])*6554)>>16)*(2*i1+1) -
		w1Q13

	return w0Q13, w1Q13
}

// RFC 6716 Section 4.2.7.2 decodes the mid-only flag for side-channel skipping.
func (d *Decoder) decodeMidOnlyFlag() bool {
	return d.rangeDecoder.DecodeSymbolWithICDF(icdfStereoMidOnly) == 1
}

// Each SILK frame contains a single "frame type" symbol that jointly
// codes the signal type and quantization offset type of the
// corresponding frame.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.3
func (d *Decoder) determineFrameType(voiceActivityDetected bool) (
	signalType frameSignalType,
	quantizationOffsetType frameQuantizationOffsetType,
) {
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

	return signalType, quantizationOffsetType
}

// A separate quantization gain is coded for each 5 ms subframe
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
func (d *Decoder) decodeSubframeQuantizations(
	signalType frameSignalType,
	subframeCount int,
	isFirstSilkFrameInOpusFrame bool,
) (gainQ16 []float32) {
	var logGain, deltaGainIndex, gainIndex int32
	gainQ16 = make([]float32, subframeCount)

	for subframeIndex := range subframeCount {
		// The subframe gains are either coded independently, or relative to the
		// gain from the most recent coded subframe in the same channel.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
		// !d.haveDecoded also covers side-channel frames that resume after
		// a mid-only frame, where the previous side frame was not coded.
		if subframeIndex == 0 && (isFirstSilkFrameInOpusFrame || !d.haveDecoded) {
			// In an independently coded subframe gain, the 3 most significant bits
			// of the quantization gain are decoded using a PDF selected from
			// Table 11 based on the decoded signal type
			switch signalType {
			case frameSignalTypeInactive:
				//nolint:gosec
				gainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBInactive))
			case frameSignalTypeVoiced:
				//nolint:gosec
				gainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBVoiced))
			case frameSignalTypeUnvoiced:
				//nolint:gosec
				gainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfIndependentQuantizationGainMSBUnvoiced))
			}

			// The 3 least significant bits are decoded using a uniform PDF:
			// These 6 bits are combined to form a value, gain_index, between 0 and 63.
			//
			//nolint:gosec
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
			//
			//nolint:gosec
			deltaGainIndex = int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfDeltaQuantizationGain))

			// The following formula translates this index into a quantization gain
			// for the current subframe using the gain from the previous subframe:
			//      log_gain = clamp(0, max(2*delta_gain_index - 16, previous_log_gain + delta_gain_index - 4), 63)
			logGain = clamp(0, maxInt32(2*deltaGainIndex-16, d.previousLogGain+deltaGainIndex-4), 63)
		}

		d.previousLogGain = logGain

		// silk_gains_dequant() (gain_quant.c) dequantizes log_gain for the k'th
		// subframe and converts it into a linear Q16 scale factor via
		//
		//       gain_Q16[k] = silk_log2lin((0x1D1C71*log_gain>>16) + 2090)
		//
		inLogQ7 := (0x1D1C71 * logGain >> 16) + 2090
		i := inLogQ7 >> 7  //nolint:varnamelen // integer exponent
		f := inLogQ7 & 127 //nolint:varnamelen // fractional exponent

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

	return gainQ16
}

// A set of normalized Line Spectral Frequency (LSF) coefficients follow
// the quantization gains in the bitstream and represent the Linear
// Predictive Coding (LPC) coefficients for the current SILK frame.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.1.
//
//nolint:cyclop
func (d *Decoder) normalizeLineSpectralFrequencyStageOne(
	voiceActivityDetected bool,
	bandwidth Bandwidth,
) (stageOneIndex uint32) {
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
		stageOneIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandUnvoiced)
	case voiceActivityDetected && (bandwidth == BandwidthNarrowband || bandwidth == BandwidthMediumband):
		stageOneIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandVoiced)
	case !voiceActivityDetected && (bandwidth == BandwidthWideband):
		stageOneIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexWidebandUnvoiced)
	case voiceActivityDetected && (bandwidth == BandwidthWideband):
		stageOneIndex = d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageOneIndexWidebandVoiced)
	}

	return
}

// A set of normalized Line Spectral Frequency (LSF) coefficients follow
// the quantization gains in the bitstream and represent the Linear
// Predictive Coding (LPC) coefficients for the current SILK frame.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2.
func (d *Decoder) normalizeLineSpectralFrequencyStageTwo(
	bandwidth Bandwidth,
	stageOneIndex uint32,
) (dLPC int, resQ10 []int16) {
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
	for i := range I2 {
		// the decoder reads a symbol using the PDF corresponding
		// to I1 from either Table 17 or Table 18 and subtracts 4 from the
		// result to give an index in the range -4 to 4, inclusive.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
		//
		//nolint:gosec
		I2[i] = int8(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndex[codebook[stageOneIndex][i]])) - 4

		// If the index is either -4 or 4, it reads a second symbol using the PDF in
		// Table 19, and adds the value of this second symbol to the index,
		// using the same sign.  This gives the index, I2[k], a total range of
		// -10 to 10, inclusive.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
		if I2[i] == -4 {
			//nolint:gosec // G115
			I2[i] -= int8(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFStageTwoIndexExtension))
		} else if I2[i] == 4 {
			//nolint:gosec // G115
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
	for k := dLPC - 1; k >= 0; k-- { //nolint:varnamelen
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
			var predQ8 int
			if bandwidth == BandwidthWideband {
				//nolint:gosec // G115
				predictionWeightIndex := predictionWeightSelectionForWidebandNormalizedLSF[stageOneIndex][k]
				predQ8 = int( //nolint:gosec // G115
					predictionWeightForWidebandNormalizedLSF[predictionWeightIndex][k],
				)
			} else {
				predictionWeightIndex := predictionWeightSelectionForNarrowbandAndMediumbandNormalizedLSF[stageOneIndex][k]
				predQ8 = int( //nolint:gosec // G115
					predictionWeightForNarrowbandAndMediumbandNormalizedLSF[predictionWeightIndex][k],
				)
			}

			firstOperand = (int(resQ10[k+1]) * predQ8) >> 8
		}

		// The following computes
		//
		// (((I2[k]<<10) - sign(I2[k])*102)*qstep)>>16
		//.
		secondOperand := (((int(I2[k]) << 10) - sign(int(I2[k]))*102) * qstep) >> 16

		resQ10[k] = int16(firstOperand + secondOperand) //nolint:gosec // G115
	}

	return dLPC, resQ10
}

// Once the stage-1 index I1 and the stage-2 residual res_Q10[] have
// been decoded, the final normalized LSF coefficients can be
// reconstructed.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
func (d *Decoder) normalizeLineSpectralFrequencyCoefficients(
	dLPC int,
	bandwidth Bandwidth,
	resQ10 []int16,
	stageOneIndex uint32,
) (nlsfQ15 []int16) {
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
	for k := range dLPC { //nolint:varnamelen
		previousCodebookValue, nextCodebookValue := uint(0), uint(256)
		if k != 0 {
			previousCodebookValue = cb1Q8[stageOneIndex][k-1]
		}

		if k+1 != dLPC {
			nextCodebookValue = cb1Q8[stageOneIndex][k+1]
		}

		w2Q18[k] = (1024/(cb1Q8[stageOneIndex][k]-previousCodebookValue) +
			1024/(nextCodebookValue-cb1Q8[stageOneIndex][k])) << 16

		// This is reduced to an unsquared, Q9 value using
		// the following square-root approximation:
		//
		//     i = ilog(w2_Q18[k])
		//     f = (w2_Q18[k]>>(i-8)) & 127
		//     y = ((i&1) ? 32768 : 46214) >> ((32-i)>>1)
		//     w_Q9[k] = y + ((213*f*y)>>16)
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
		i := ilog(int(w2Q18[k]))              //nolint:gosec // G115
		f := int((w2Q18[k] >> (i - 8)) & 127) //nolint:gosec // G115

		y := 46214
		if (i & 1) != 0 {
			y = 32768
		}

		y >>= ((32 - i) >> 1)
		wQ9[k] = int16(y + ((213 * f * y) >> 16)) //nolint:gosec // G115

		// Given the stage-1 codebook entry cb1_Q8[], the stage-2 residual
		// res_Q10[], and their corresponding weights, w_Q9[], the reconstructed
		// normalized LSF coefficients are
		//
		//    NLSF_Q15[k] = clamp(0,
		//               (cb1_Q8[k]<<7) + (res_Q10[k]<<14)/w_Q9[k], 32767)
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
		nlsfQ15[k] = int16(clamp(0, //nolint:gosec // G115
			int32((int(cb1Q8[stageOneIndex][k])<<7)+(int(resQ10[k])<<14)/int(wQ9[k])), 32767)) //nolint:gosec // G115
	}

	return nlsfQ15
}

// The normalized LSF stabilization procedure ensures that
// consecutive values of the normalized LSF coefficients, NLSF_Q15[],
// are spaced some minimum distance apart (predetermined to be the 0.01
// percentile of a large training set).
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
//
//nolint:cyclop
func (d *Decoder) normalizeLSFStabilization(nlsfQ15 []int16, dLPC int, bandwidth Bandwidth) {
	// Let NDeltaMin_Q15[k] be the minimum required spacing for the current
	// audio bandwidth from Table 25.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
	NDeltaMinQ15 := codebookMinimumSpacingForNormalizedLSCoefficientsNarrowbandAndMediumband
	if bandwidth == BandwidthWideband {
		NDeltaMinQ15 = codebookMinimumSpacingForNormalizedLSCoefficientsWideband
	}

	// The procedure starts off by trying to make small adjustments that
	// attempt to minimize the amount of distortion introduced.  After 20
	// such adjustments, it falls back to a more direct method that
	// guarantees the constraints are enforced but may require large
	// adjustments.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
	for adjustment := 0; adjustment <= 19; adjustment++ {
		// First, the procedure finds the index
		// i where NLSF_Q15[i] - NLSF_Q15[i-1] - NDeltaMin_Q15[i] is the
		// smallest, breaking ties by using the lower value of i.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
		i := 0
		iValue := int(math.MaxInt)

		for nlsfIndex := 0; nlsfIndex <= len(nlsfQ15); nlsfIndex++ {
			// For the purposes of computing this spacing for the first and last coefficient,
			// NLSF_Q15[-1] is taken to be 0 and NLSF_Q15[d_LPC] is taken to be 32768
			//
			// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
			previousNLSF := 0
			currentNLSF := 32768
			if nlsfIndex != 0 {
				previousNLSF = int(nlsfQ15[nlsfIndex-1]) // #nosec G602
			}
			if nlsfIndex != len(nlsfQ15) {
				currentNLSF = int(nlsfQ15[nlsfIndex])
			}

			spacingValue := currentNLSF - previousNLSF - NDeltaMinQ15[nlsfIndex]
			if spacingValue < iValue {
				i = nlsfIndex
				iValue = spacingValue
			}
		}

		switch {
		// If this value is non-negative, then the stabilization stops; the coefficients
		// satisfy all the constraints.
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
		case iValue >= 0:
			return
		// if i == 0, it sets NLSF_Q15[0] to NDeltaMin_Q15[0]
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
		case i == 0:
			nlsfQ15[0] = int16(NDeltaMinQ15[0]) //nolint:gosec // G115

			continue
		// if i == d_LPC, it sets
		//  NLSF_Q15[d_LPC-1] to (32768 - NDeltaMin_Q15[d_LPC])
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
		case i == dLPC:
			nlsfQ15[dLPC-1] = int16(32768 - NDeltaMinQ15[dLPC]) //nolint:gosec // G115

			continue
		}

		// 	For all other values of i, both NLSF_Q15[i-1] and NLSF_Q15[i] are updated as
		// follows:
		//                                              i-1
		//                                              __
		//     min_center_Q15 = (NDeltaMin_Q15[i]>>1) + \  NDeltaMin_Q15[k]
		//                                              /_
		//                                              k=0
		//
		minCenterQ15 := NDeltaMinQ15[i] >> 1
		for k := 0; k <= i-1; k++ {
			minCenterQ15 += NDeltaMinQ15[k]
		}

		// 		                                                d_LPC
		//                                                      __
		//     max_center_Q15 = 32768 - (NDeltaMin_Q15[i]>>1) - \  NDeltaMin_Q15[k]
		//                                                      /_
		//                                                     k=i+1
		maxCenterQ15 := 32768 - (NDeltaMinQ15[i] >> 1)
		for k := i + 1; k <= dLPC; k++ {
			maxCenterQ15 -= NDeltaMinQ15[k]
		}

		//     center_freq_Q15 = clamp(min_center_Q15[i],
		//                     (NLSF_Q15[i-1] + NLSF_Q15[i] + 1)>>1
		//                     max_center_Q15[i])
		centerFreqQ15 := int(clamp(
			int32(minCenterQ15), //nolint:gosec // G115
			int32((int(nlsfQ15[i-1])+int(nlsfQ15[i])+1)>>1), //nolint:gosec // G115
			int32(maxCenterQ15)), //nolint:gosec // G115
		)

		//    NLSF_Q15[i-1] = center_freq_Q15 - (NDeltaMin_Q15[i]>>1)
		//    NLSF_Q15[i] = NLSF_Q15[i-1] + NDeltaMin_Q15[i]
		nlsfQ15[i-1] = int16(centerFreqQ15 - NDeltaMinQ15[i]>>1) //nolint:gosec // G115
		nlsfQ15[i] = nlsfQ15[i-1] + int16(NDeltaMinQ15[i])       //nolint:gosec // G115
	}

	// After the 20th repetition of the above procedure, the following
	// fallback procedure executes once.  First, the values of NLSF_Q15[k]
	// for 0 <= k < d_LPC are sorted in ascending order.  Then, for each
	// value of k from 0 to d_LPC-1, NLSF_Q15[k] is set to
	slices.Sort(nlsfQ15)

	// Then, for each value of k from 0 to d_LPC-1, NLSF_Q15[k] is set to
	//
	//   max(NLSF_Q15[k], NLSF_Q15[k-1] + NDeltaMin_Q15[k])
	for k := 0; k <= dLPC-1; k++ {
		prevNLSF := int16(0)
		if k != 0 {
			prevNLSF = nlsfQ15[k-1] // #nosec G602
		}

		nlsfQ15[k] = maxInt16(nlsfQ15[k], saturatingAddInt16(prevNLSF, int16(NDeltaMinQ15[k]))) //nolint:gosec // G115
	}

	// Next, for each value of k from d_LPC-1 down to 0, NLSF_Q15[k] is set
	// to
	//
	//   min(NLSF_Q15[k], NLSF_Q15[k+1] - NDeltaMin_Q15[k+1])
	for k := dLPC - 1; k >= 0; k-- {
		nextNLSF := 32768
		if k != dLPC-1 {
			nextNLSF = int(nlsfQ15[k+1])
		}

		nlsfQ15[k] = minInt16(nlsfQ15[k], int16(nextNLSF-NDeltaMinQ15[k+1])) //nolint:gosec // G115
	}
}

// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
func (d *Decoder) normalizeLSFInterpolation(n2Q15 []int16, nanoseconds int) (n1Q15 []int16, wQ2 int16) {
	// Let n2_Q15[k] be the normalized LSF coefficients decoded by the
	// procedure in Section 4.2.7.5, n0_Q15[k] be the LSF coefficients
	// decoded for the prior frame, and w_Q2 be the interpolation factor.
	// Then, the normalized LSF coefficients used for the first half of a
	// 20 ms frame, n1_Q15[k], are
	//
	//      n1_Q15[k] = n0_Q15[k] + (w_Q2*(n2_Q15[k] - n0_Q15[k]) >> 2)
	if nanoseconds != nanoseconds20Ms {
		return nil, 4
	}

	wQ2 = int16(d.rangeDecoder.DecodeSymbolWithICDF(icdfNormalizedLSFInterpolationIndex)) //nolint:gosec // G115
	if wQ2 == 4 || !d.haveDecoded {
		return nil, wQ2
	}
	if len(d.n0Q15) != len(n2Q15) {
		return nil, wQ2
	}

	n1Q15 = make([]int16, len(n2Q15))
	for k := range n1Q15 {
		interpolated := int32(wQ2) * (int32(n2Q15[k]) - int32(d.n0Q15[k])) >> 2 //nolint:gosec // G602
		n1Q15[k] = int16(int32(d.n0Q15[k]) + interpolated)                      //nolint:gosec // G115
	}

	return
}

func (d *Decoder) generateAQ12(q15 []int16, bandwidth Bandwidth, aQ12 [][]float32) [][]float32 {
	if q15 == nil {
		return aQ12
	}

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.6
	a32Q17 := d.convertNormalizedLSFsToLPCCoefficients(q15, bandwidth)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.5.7
	d.limitLPCCoefficientsRange(a32Q17)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.5.8
	aQ12 = append(aQ12, d.limitLPCFilterPredictionGain(a32Q17))

	return aQ12
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
		pQ16[k+1] = pQ16[k-1]*2 - int32(((int64(cQ17[2*k])*int64(pQ16[k]))+32768)>>16)     //nolint:gosec // G115
		qQ16[k+1] = qQ16[k-1]*2 - int32(((int64(cQ17[(2*k)+1])*int64(qQ16[k]))+32768)>>16) //nolint:gosec // G115

		for j := k; j > 1; j-- {
			pQ16[j] += pQ16[j-2] - int32(((int64(cQ17[2*k])*int64(pQ16[j-1]))+32768)>>16)     //nolint:gosec // G115
			qQ16[j] += qQ16[j-2] - int32(((int64(cQ17[(2*k)+1])*int64(qQ16[j-1]))+32768)>>16) //nolint:gosec // G115
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
	for k := range d2 {
		a32Q17[k] = -(qQ16[k+1] - qQ16[k]) - (pQ16[k+1] + pQ16[k])
		a32Q17[dLPC-k-1] = (qQ16[k+1] - qQ16[k]) - (pQ16[k+1] + pQ16[k])
	}

	return a32Q17
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
//
//nolint:cyclop
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
	for i := range shellblocks {
		pulsecounts[i] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdfPulseCount[rateLevel])) //nolint:gosec // g115

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
				pulsecounts[i] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdfPulseCount[9])) //nolint:gosec // g115
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
				pulsecounts[i] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdfPulseCount[10])) //nolint:gosec // g115
			}
		}
	}

	return pulsecounts, lsbcounts
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
		for j := range 2 {
			d.partitionPulseCount(icdfPulseCountSplit8SamplePartitions, samplePartition16[j], samplePartition8)
			for k := range 2 {
				d.partitionPulseCount(icdfPulseCountSplit4SamplePartitions, samplePartition8[k], samplePartition4)
				for l := range 2 {
					d.partitionPulseCount(icdfPulseCountSplit2SamplePartitions, samplePartition4[l], samplePartition2)
					eRaw[eRawIndex] = int32(samplePartition2[0])
					eRawIndex++

					eRaw[eRawIndex] = int32(samplePartition2[1])
					eRawIndex++
				}
			}
		}
	}

	return eRaw
}

// After the decoder reads the pulse locations for all blocks, it reads
// the LSBs (if any) for each block in turn.  Inside each block, it
// reads all the LSBs for each coefficient in turn, even those where no
// pulses were allocated, before proceeding to the next one.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.4
func (d *Decoder) decodeExcitationLSB(eRaw []int32, lsbcounts []uint8) {
	for i := range eRaw {
		for bit := uint8(0); bit < lsbcounts[i/pulsecountLargestPartitionSize]; bit++ {
			eRaw[i] = (eRaw[i] << 1) | int32(d.rangeDecoder.DecodeSymbolWithICDF(icdfExcitationLSB)) //nolint:gosec // g115
		}
	}
}

// After decoding the pulse locations and the LSBs, the decoder knows
// the magnitude of each coefficient in the excitation.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.8.5
//
//nolint:cyclop,gocyclo,gocognit
func (d *Decoder) decodeExcitationSign(
	eRaw []int32,
	signalType frameSignalType,
	quantizationOffsetType frameQuantizationOffsetType,
	pulsecounts []uint8,
) {
	for i := range eRaw {
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
//
//nolint:cyclop
func (d *Decoder) decodeExcitation(
	signalType frameSignalType,
	quantizationOffsetType frameQuantizationOffsetType,
	seed uint32,
	pulsecounts, lsbcounts []uint8,
) (eQ23 []int32) {
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
		offsetQ23 = 60
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
	for i := range eRaw {
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

		eQ23[i] = (eRaw[i] << 8) - int32(sign(int(eRaw[i])))*20 + offsetQ23 //nolint:gosec // g115
		seed = (196314165*seed + 907633515) & 0xFFFFFFFF
		if seed&0x80000000 != 0 {
			eQ23[i] *= -1
		}
		seed = (seed + uint32(eRaw[i])) & 0xFFFFFFFF //nolint:gosec // g115
	}

	return eQ23
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
		halves[0] = uint8(d.rangeDecoder.DecodeSymbolWithICDF(icdf[block-1])) //nolint:gosec // g115
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
			abs := int32(sign(int(val))) * val //nolint:gosec // g115
			if maxabsQ17 < uint(abs) {         //nolint:gosec // g115
				maxabsQ17K = uint(k)  //nolint:gosec // g115
				maxabsQ17 = uint(abs) //nolint:gosec // g115
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
			scQ16 := uint(65470)
			scQ16 -= ((maxabsQ12 - 32767) << 14) / ((maxabsQ12 * (maxabsQ17K + 1)) >> 2)

			// RFC 6716 spells out the bandwidth expansion recurrence here as
			// sc_Q16[k]. This branch keeps that recurrence as the shared Go
			// implementation so the coefficient range limiter and the
			// prediction-gain limiter use the same code path:
			//
			//            a32_Q17[k] = (a32_Q17[k]*sc_Q16[k]) >> 16
			//
			//           sc_Q16[k+1] = (sc_Q16[0]*sc_Q16[k] + 32768) >> 16
			//
			// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.7
			expandLPCCoefficientsBandwidth(a32Q17, int32(scQ16)) //nolint:gosec // G115
		} else {
			break
		}
	}

	// After 10 rounds of bandwidth expansion are performed, they are simply
	// saturated to 16 bits:
	//
	//     a32_Q17[k] = clamp(-32768, (a32_Q17[k] + 16) >> 5, 32767) << 5
	//
	// RFC 6716 section 4.2.7.5.7 says the 10th bandwidth-expansion round is
	// special: even if the coefficients would no longer overflow in Q12, the
	// decoder still has to saturate them in Q12 and then convert them back to
	// Q17 for the prediction-gain limiter. The extracted C reference does the
	// same thing in silk_NLSF2A() (NLSF2A.c), so this branch follows that
	// behavior exactly instead of stopping after the 9th expansion.
	if bandwidthExpansionRound == 10 {
		for k := range a32Q17 {
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
	aQ12Int := make([]int16, len(a32Q17))
	for n := range a32Q17 {
		aQ12Int[n] = int16((a32Q17[n] + 16) >> 5) //nolint:gosec // G115
	}
	for i := range 16 {
		if lpcInversePredictionGain(aQ12Int) >= 107374 {
			break
		}

		// RFC 6716 section 4.2.7.5.8 applies up to 16 more rounds of bandwidth
		// expansion when the inverse prediction gain is too small. The chirp
		// factor starts at 65534 and decreases as 65536 - (2 << i), which is
		// the same sequence used by silk_NLSF2A() in NLSF2A.c. After each
		// expansion we re-quantize to Q12 before checking stability again,
		// because the C reference measures the gain on the exact coefficients
		// used by reconstruction.
		expandLPCCoefficientsBandwidth(a32Q17, int32(65536-(2<<i)))
		for n := range a32Q17 {
			aQ12Int[n] = int16((a32Q17[n] + 16) >> 5) //nolint:gosec // G115
		}
	}

	aQ12 = make([]float32, len(aQ12Int))
	for n := range aQ12Int {
		aQ12[n] = float32(aQ12Int[n])
	}

	return aQ12
}

//nolint:cyclop
func lpcInversePredictionGain(aQ12 []int16) int32 {
	const (
		inversePredictionGainQA     = 24
		inversePredictionGainALimit = 16773022
	)

	order := len(aQ12)
	var atmpQA [2][16]int32
	aNewQA := atmpQA[order&1][:]
	dcResp := int32(0)
	for k := range order {
		dcResp += int32(aQ12[k])
		aNewQA[k] = int32(aQ12[k]) << (inversePredictionGainQA - 12)
	}
	// RFC 6716 section 4.2.7.5.8 has two prose mismatches here: it spells
	// the summation bound as d_PLC instead of d_LPC, and it says the filter is
	// unstable when "DC_resp > 4096". The extracted C reference in
	// silk_LPC_inverse_pred_gain() (LPC_inv_pred_gain.c) sums over the LPC
	// order and rejects DC_resp >= 4096, and RFC 6716 section 6 says the
	// reference source takes precedence for conformance.
	if dcResp >= 4096 {
		return 0
	}

	invGainQ30 := int32(1 << 30)
	for coefIndex := order - 1; coefIndex > 0; coefIndex-- {
		// This is the fixed-point Levinson recurrence from RFC 6716 section
		// 4.2.7.5.8. The code intentionally mirrors
		// silk_LPC_inverse_pred_gain_QA() in LPC_inv_pred_gain.c, including
		// the Q24/Q30 scaling, the reflection-coefficient stability checks,
		// and the saturating numerator update, because tiny arithmetic
		// differences here can flip a filter from stable to unstable.
		if aNewQA[coefIndex] > inversePredictionGainALimit || aNewQA[coefIndex] < -inversePredictionGainALimit {
			return 0
		}

		rcQ31 := -(aNewQA[coefIndex] << (31 - inversePredictionGainQA))
		rcMult1Q30 := int32(1<<30) - smmul(rcQ31, rcQ31)
		mult2Q := 32 - clz32(absInt32(rcMult1Q30))
		rcMult2 := inverse32VarQ(rcMult1Q30, mult2Q+30)
		invGainQ30 = smmul(invGainQ30, rcMult1Q30) << 2

		aOldQA := aNewQA
		aNewQA = atmpQA[coefIndex&1][:]
		for n := 0; n < coefIndex; n++ {
			tmpQA := saturatingSubInt32(
				aOldQA[n],
				int32(rshiftRound64(int64(aOldQA[coefIndex-n-1])*int64(rcQ31), 31)), //nolint:gosec // G115
			)
			tmp64 := rshiftRound64(int64(tmpQA)*int64(rcMult2), mult2Q)
			if tmp64 > math.MaxInt32 || tmp64 < math.MinInt32 {
				return 0
			}
			aNewQA[n] = int32(tmp64)
		}
	}

	if aNewQA[0] > inversePredictionGainALimit || aNewQA[0] < -inversePredictionGainALimit {
		return 0
	}

	rcQ31 := -(aNewQA[0] << (31 - inversePredictionGainQA))
	rcMult1Q30 := int32(1<<30) - smmul(rcQ31, rcQ31)
	invGainQ30 = smmul(invGainQ30, rcMult1Q30) << 2

	return invGainQ30
}

func pitchLagCodebooks(bandwidth Bandwidth) (lowPartICDF []uint, lagScale, lagMin, lagMax uint32) {
	switch bandwidth {
	case BandwidthNarrowband:
		return icdfPrimaryPitchLagLowPartNarrowband, 4, 16, 144
	case BandwidthMediumband:
		return icdfPrimaryPitchLagLowPartMediumband, 6, 24, 216
	case BandwidthWideband:
		return icdfPrimaryPitchLagLowPartWideband, 8, 32, 288
	}

	return nil, 0, 0, 0
}

func (d *Decoder) decodePrimaryPitchLag(lagAbsolute bool, lowPartICDF []uint, lagScale, lagMin uint32) int {
	if lagAbsolute {
		lagHigh := d.rangeDecoder.DecodeSymbolWithICDF(icdfPrimaryPitchLagHighPart)
		lagLow := d.rangeDecoder.DecodeSymbolWithICDF(lowPartICDF)

		return int(lagHigh*lagScale + lagLow + lagMin)
	}

	deltaLagIndex := d.rangeDecoder.DecodeSymbolWithICDF(icdfPrimaryPitchLagChange)
	if deltaLagIndex == 0 {
		lagHigh := d.rangeDecoder.DecodeSymbolWithICDF(icdfPrimaryPitchLagHighPart)
		lagLow := d.rangeDecoder.DecodeSymbolWithICDF(lowPartICDF)

		return int(lagHigh*lagScale + lagLow + lagMin)
	}

	return d.previousLag + int(deltaLagIndex) - 9
}

func pitchContourCodebooks(bandwidth Bandwidth, nanoseconds int) (lagCb [][]int8, lagIcdf []uint) {
	switch bandwidth {
	case BandwidthNarrowband:
		if nanoseconds == nanoseconds10Ms {
			return codebookSubframePitchCounterNarrowband10Ms, icdfSubframePitchContourNarrowband10Ms
		}

		return codebookSubframePitchCounterNarrowband20Ms, icdfSubframePitchContourNarrowband20Ms
	case BandwidthMediumband, BandwidthWideband:
		if nanoseconds == nanoseconds10Ms {
			return codebookSubframePitchCounterMediumbandOrWideband10Ms, icdfSubframePitchContourMediumbandOrWideband10Ms
		}

		return codebookSubframePitchCounterMediumbandOrWideband20Ms, icdfSubframePitchContourMediumbandOrWideband20Ms
	}

	return nil, nil
}

// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.1
func (d *Decoder) decodePitchLags(
	signalType frameSignalType,
	bandwidth Bandwidth,
	nanoseconds int,
	isFirstSilkFrameInOpusFrame bool,
) (lagMax uint32, pitchLags []int) {
	if signalType != frameSignalTypeVoiced {
		return 0, nil
	}

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

	lagAbsolute := isFirstSilkFrameInOpusFrame || !d.isPreviousFrameVoiced
	lowPartICDF, lagScale, lagMin, lagMax := pitchLagCodebooks(bandwidth)

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

	// The final primary pitch lag is then
	//
	//              lag = lag_high*lag_scale + lag_low + lag_min
	//
	// where lag_high is the high part, lag_low is the low part, and
	// lag_scale and lag_min are the values from the "Scale" and "Minimum
	// Lag" columns of Table 30, respectively.
	lag := d.decodePrimaryPitchLag(lagAbsolute, lowPartICDF, lagScale, lagMin)
	d.previousLag = lag

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

	lagCb, lagIcdf := pitchContourCodebooks(bandwidth, nanoseconds)
	contourIndex := d.rangeDecoder.DecodeSymbolWithICDF(lagIcdf)

	// Then the final pitch lag for that subframe is
	//
	//     pitch_lags[k] = clamp(lag_min, lag + lag_cb[contour_index][k],
	//                           lag_max)
	pitchLags = make([]int, subframeCount(nanoseconds))
	for i := range pitchLags {
		pitchLags[i] = int(clamp(
			int32(lagMin),                          //nolint:gosec
			int32(lag+int(lagCb[contourIndex][i])), //nolint:gosec
			int32(lagMax)),                         //nolint:gosec
		)
	}

	return lagMax, pitchLags
}

// This allows the encoder to trade off the prediction gain between
// packets against the recovery time after packet loss.
//
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.3
func (d *Decoder) decodeLTPScalingParameter(
	signalType frameSignalType,
	isFirstSilkFrameInOpusFrame bool,
) float32 {
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
	if signalType != frameSignalTypeVoiced || !isFirstSilkFrameInOpusFrame {
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
func (d *Decoder) decodeLTPFilterCoefficients(signalType frameSignalType, subframeCount int) (bQ7 [][]int8) {
	if signalType != frameSignalTypeVoiced {
		return bQ7
	}

	bQ7 = make([][]int8, subframeCount)
	for i := range bQ7 {
		bQ7[i] = make([]int8, 5)
	}

	// This is signaled with an explicitly-coded "periodicity index".  This
	// immediately follows the subframe pitch lags, and is coded using the
	// 3-entry PDF from Table 37.
	periodicityIndex := d.rangeDecoder.DecodeSymbolWithICDF(icdfPeriodicityIndex)

	// The indices of the filters for each subframe follow.  They are all
	// coded using the PDF from Table 38 corresponding to the periodicity
	// index.  Tables 39 through 41 contain the corresponding filter taps as
	// signed Q7 integers.
	for i := range subframeCount {
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

	return bQ7
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
//
//nolint:gocognit,cyclop,unused // Kept as the RFC-prose reference path while fixed-point reconstruction is evaluated.
func (d *Decoder) ltpSynthesis(
	out []float32,
	bQ7 [][]int8,
	pitchLags []int,
	n, j, s, dLPC int, //nolint:varnamelen
	ltpScaleQ14 float32,
	wQ2 int16,
	aQ12, gainQ16, res, resLag []float32,
) {
	// If this is the third or fourth subframe of a 20 ms SILK frame and the LSF
	// interpolation factor, w_Q2 (see Section 4.2.7.5.5), is less than 4,
	// then let out_end be set to (j - (s-2)*n) and let LTP_scale_Q14 be set
	// to 16384.  Otherwise, set out_end to (j - s*n) and set LTP_scale_Q14
	// to the Q14 LTP scaling value from Section 4.2.7.6.3.
	var outEnd int
	if s < 2 || wQ2 == 4 {
		outEnd = -s * n
	} else {
		outEnd = -(s - 2) * n
		ltpScaleQ14 = 16384.0
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
	for i := (-pitchLags[s]) - 2; i < outEnd; i++ {
		index := i + j

		var (
			resVal     float32
			resIndex   int
			writeToLag bool
		)

		switch {
		case index >= len(res):
			continue
		case index >= 0:
			resVal = out[index]
			resIndex = index
		default:
			resIndex = len(resLag) + index
			resVal = d.finalOutValues[len(d.finalOutValues)+index]
			writeToLag = true
		}

		for k := range dLPC {
			var outVal float32
			if outIndex := index - k - 1; outIndex >= 0 {
				outVal = out[outIndex]
			} else {
				outVal = d.finalOutValues[len(d.finalOutValues)+outIndex]
			}

			resVal -= outVal * (aQ12[k] / 4096.0)
		}

		resVal = clampNegativeOneToOne(resVal)
		resVal *= (4.0 * ltpScaleQ14) / gainQ16[s]

		if !writeToLag {
			res[resIndex] = resVal
		} else {
			resLag[resIndex] = resVal
		}
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
	// d_LPC.

	// The astute reader will notice that, given the definition of
	// lpc[i] in Section 4.2.7.9.2, the output of this latter equation is
	// merely a scaled version of the values of res[i] from previous
	// subframes.
	if s > 0 {
		scaledGain := gainQ16[s-1] / gainQ16[s]
		for i := outEnd; i < 0; i++ {
			index := j + i
			if index < 0 {
				resLag[len(resLag)+index] *= scaledGain
			} else {
				res[index] *= scaledGain
			}
		}
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
		resSum = res[i]
		for k := 0; k <= 4; k++ {
			if resIndex := i - pitchLags[s] + 2 - k; resIndex < 0 {
				resVal = resLag[len(resLag)+resIndex]
			} else {
				resVal = res[resIndex]
			}

			resSum += resVal * (float32(bQ7[s][k]) / 128.0)
		}

		res[i] = resSum
	}
}

// LPC synthesis uses the short-term LPC filter to predict the next
// output coefficient.  For i such that (j - d_LPC) <= i < j, let lpc[i]
// be the result of LPC synthesis from the last d_LPC samples of the
// previous subframe or zeros in the first subframe for this channel
// after either
//
// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.2
func (d *Decoder) lpcSynthesis(
	out []float32,
	n, s, dLPC int, //nolint:varnamelen
	aQ12, res, gainQ16, lpc []float32,
) {
	// j be the index of the first sample in the residual corresponding to
	// the current subframe.
	j := 0

	// Then, for i such that j <= i < (j + n), the result of LPC synthesis
	// for the current subframe is
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

		for k, aQ12 := range aQ12[:dLPC] {
			lpcIndex := sampleIndex - k - 1
			switch {
			case lpcIndex >= 0:
				currentLPCVal = lpc[lpcIndex]
			case s == 0:
				previousIndex := len(d.previousFrameLPCValues) - 1 + (i - k)
				if previousIndex >= 0 {
					currentLPCVal = d.previousFrameLPCValues[previousIndex]
				} else {
					currentLPCVal = 0
				}
			default:
				currentLPCVal = 0
			}

			lpcVal += currentLPCVal * (aQ12 / 4096.0)
		}

		lpc[sampleIndex] = lpcVal

		// Then, the signal is clamped into the final nominal range:
		//
		//     out[i] = clamp(-1.0, lpc[i], 1.0)
		//
		out[i] = clampNegativeOneToOne(lpc[sampleIndex])

		//  The decoder saves the final d_LPC values, i.e., lpc[i] such that
		// (j + n - d_LPC) <= i < (j + n), to feed into the LPC synthesis of the
		// next subframe.  This requires storage for up to 16 values of lpc[i]
		// (for WB frames).
		// The final d_LPC synthesized samples become the history for the next
		// subframe. RFC 6716 section 4.2.7.9 describes that continuity
		// requirement, and decode_frame.c preserves this state even for the
		// first decoded frame. The old haveDecoded guard skipped that initial
		// handoff and left the next frame with an all-zero LPC history.
		if len(out)-1 == i {
			d.previousFrameLPCValues = append([]float32{}, lpc[len(lpc)-dLPC:]...)
		}
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
//
//nolint:unused // Kept as the RFC-prose reference path while fixed-point reconstruction is evaluated.
func (d *Decoder) silkFrameReconstruction(
	signalType frameSignalType, bandwidth Bandwidth,
	subframeCount int,
	dLPC int,
	lagMax uint32,
	bQ7 [][]int8,
	pitchLags []int,
	eQ23 []int32,
	ltpScaleQ14 float32,
	wQ2 int16,
	aQ12 [][]float32,
	gainQ16, out []float32,
) {
	// let n be the number of samples in a subframe
	//
	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
	n := d.samplesInSubframe(bandwidth)

	// let lpc[i] be the result of LPC synthesis from the last d_LPC samples of the
	//  previous subframe or zeros in the first subframe for this channel
	lpc := make([]float32, n*subframeCount)

	// For unvoiced frames (see Section 4.2.7.3), the LPC residual for i
	// such that j <= i < (j + n) is simply a normalized copy of the
	// excitation signal, i.e.,
	//
	//               e_Q23[i]
	//     res[i] = ---------
	//               2.0**23
	res := make([]float32, len(eQ23))
	resLag := make([]float32, lagMax+2)
	for i := range res {
		res[i] = float32(eQ23[i]) / 8388608.0
	}

	// subFrame be the index of the current subframe in this SILK frame
	// (0 or 1 for 10 ms frames, or 0 to 3 for 20 ms frames)
	for subFrame := range subframeCount {
		// For 20 ms SILK frames, the first half of the frame (i.e., the first
		// two subframes) may use normalized LSF coefficients that are
		// interpolated between the decoded LSFs for the most recent coded frame
		// (in the same channel) and the current frame
		//
		// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
		aQ12Index := 0
		if subFrame > 1 && len(aQ12) > 1 {
			aQ12Index = 1
		}

		// j be the index of the first sample in the residual corresponding to
		// the current subframe.
		//
		// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9
		j := n * subFrame

		// Voiced SILK frames, on the other hand, pass the excitation through an
		// LTP filter using the parameters decoded in Section 4.2.7.6 to produce
		// an LPC residual.
		//
		// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.1
		if signalType == frameSignalTypeVoiced {
			d.ltpSynthesis(
				out,
				bQ7, pitchLags,
				n, j, subFrame, dLPC,
				ltpScaleQ14,
				wQ2,
				aQ12[aQ12Index], gainQ16, res, resLag,
			)
		}

		// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.9.2
		d.lpcSynthesis(out[n*subFrame:], n, subFrame, dLPC, aQ12[aQ12Index], res, gainQ16, lpc)
	}
}

func silkLTPMemLength(bandwidth Bandwidth) int {
	switch bandwidth {
	case BandwidthNarrowband:
		return 160
	case BandwidthMediumband:
		return 240
	case BandwidthWideband:
		return 320
	}

	return 0
}

//nolint:cyclop // Mirrors silk_decode_core() setup from the RFC 6716 C reference.
func (d *Decoder) silkFrameReconstructionFixed(
	signalType frameSignalType, bandwidth Bandwidth,
	subframeCount int,
	dLPC int,
	bQ7 [][]int8,
	pitchLags []int,
	eQ23 []int32,
	ltpScaleQ14 float32,
	wQ2 int16,
	aQ12 [][]float32,
	gainQ16, out []float32,
) {
	frameLength := len(out)
	if frameLength == 0 {
		return
	}

	predCoefQ12 := make([][maxSilkLPCOrder]int16, len(aQ12))
	for setIndex := range aQ12 {
		for i := range min(dLPC, len(aQ12[setIndex])) {
			predCoefQ12[setIndex][i] = int16(aQ12[setIndex][i]) //nolint:gosec // G115
		}
	}

	ltpCoefQ14 := make([][silkLTPOrder]int16, subframeCount)
	if signalType == frameSignalTypeVoiced {
		for subframe := range min(subframeCount, len(bQ7)) {
			for i := range min(silkLTPOrder, len(bQ7[subframe])) {
				ltpCoefQ14[subframe][i] = int16(bQ7[subframe][i]) << 7
			}
		}
	}

	gainsQ16 := make([]int32, len(gainQ16))
	for i, gain := range gainQ16 {
		gainsQ16[i] = int32(gain) //nolint:gosec // G115
	}

	excQ14 := make([]int32, len(eQ23))
	for i, excitation := range eQ23 {
		excQ14[i] = excitation << 6
	}

	xq := make([]int16, frameLength)
	d.decodeCoreFixed(
		xq,
		excQ14,
		signalType,
		bandwidth,
		dLPC,
		predCoefQ12,
		ltpCoefQ14,
		pitchLags,
		gainsQ16,
		int32(ltpScaleQ14),
		wQ2,
	)

	for i, sample := range xq {
		out[i] = float32(sample) / 32768.0
	}

	ltpMemLength := silkLTPMemLength(bandwidth)
	mvLen := ltpMemLength - frameLength
	if mvLen > 0 {
		copy(d.outBuf[:mvLen], d.outBuf[frameLength:frameLength+mvLen])
		copy(d.outBuf[mvLen:ltpMemLength], xq)
	} else {
		copy(d.outBuf[:ltpMemLength], xq[frameLength-ltpMemLength:])
	}
}

// decodeCoreFixed mirrors the RFC 6716 fixed-point silk_decode_core() loop.
//
//nolint:cyclop,gocognit,gosec,nestif // Directly mirrors the C reference's fixed-point decode_core loop.
func (d *Decoder) decodeCoreFixed(
	xq []int16,
	excQ14 []int32,
	signalType frameSignalType,
	bandwidth Bandwidth,
	dLPC int,
	predCoefQ12 [][maxSilkLPCOrder]int16,
	ltpCoefQ14 [][silkLTPOrder]int16,
	pitchLags []int,
	gainsQ16 []int32,
	ltpScaleQ14 int32,
	wQ2 int16,
) {
	n := d.samplesInSubframe(bandwidth)
	ltpMemLength := silkLTPMemLength(bandwidth)
	interpolatedNLSF := wQ2 < 4
	sLPCQ14 := make([]int32, maxSilkSubframeLength+maxSilkLPCOrder)
	sLTPQ15 := make([]int32, 2*maxSilkFrameLength)
	sLTP := make([]int16, maxSilkFrameLength)
	resQ14 := make([]int32, maxSilkSubframeLength)
	copy(sLPCQ14[:maxSilkLPCOrder], d.sLPCQ14Buf[:])
	sLTPBufIndex := ltpMemLength
	if d.previousGainQ16 == 0 {
		d.previousGainQ16 = 65536
	}

	for subframe := 0; subframe*n < len(xq); subframe++ {
		subframeOffset := subframe * n
		coefIndex := subframe >> 1
		if coefIndex >= len(predCoefQ12) {
			coefIndex = len(predCoefQ12) - 1
		}
		aQ12 := predCoefQ12[coefIndex][:]
		gainQ16 := gainsQ16[min(subframe, len(gainsQ16)-1)]
		gainQ10 := gainQ16 >> 6
		invGainQ31 := inverse32VarQ(gainQ16, 47)
		gainAdjQ16 := int32(1 << 16)
		if gainQ16 != d.previousGainQ16 {
			gainAdjQ16 = div32VarQ(d.previousGainQ16, gainQ16, 16)
			for i := range maxSilkLPCOrder {
				sLPCQ14[i] = smulww(gainAdjQ16, sLPCQ14[i])
			}
		}
		d.previousGainQ16 = gainQ16

		if signalType == frameSignalTypeVoiced {
			lag := pitchLags[min(subframe, len(pitchLags)-1)]
			if subframe == 0 || (subframe == 2 && interpolatedNLSF) {
				startIndex := max(0, ltpMemLength-lag-dLPC-silkLTPOrder/2)
				if subframe == 2 {
					copy(d.outBuf[ltpMemLength:], xq[:2*n])
				}
				lpcAnalysisFilterFixed(
					sLTP[startIndex:],
					d.outBuf[startIndex+subframe*n:],
					aQ12,
					ltpMemLength-startIndex,
					dLPC,
				)
				if subframe == 0 {
					invGainQ31 = smulwb(invGainQ31, ltpScaleQ14) << 2
				}
				for i := 0; i < lag+silkLTPOrder/2; i++ {
					sLTPQ15[sLTPBufIndex-i-1] = smulwb(invGainQ31, int32(sLTP[ltpMemLength-i-1]))
				}
			} else if gainAdjQ16 != 1<<16 {
				for i := 0; i < lag+silkLTPOrder/2; i++ {
					sLTPQ15[sLTPBufIndex-i-1] = smulww(gainAdjQ16, sLTPQ15[sLTPBufIndex-i-1])
				}
			}

			predLagIndex := sLTPBufIndex - lag + silkLTPOrder/2
			for i := range n {
				ltpPredQ13 := int32(2)
				ltpPredQ13 = smlaWB(ltpPredQ13, sLTPQ15[predLagIndex], int32(ltpCoefQ14[subframe][0]))
				ltpPredQ13 = smlaWB(ltpPredQ13, sLTPQ15[predLagIndex-1], int32(ltpCoefQ14[subframe][1]))
				ltpPredQ13 = smlaWB(ltpPredQ13, sLTPQ15[predLagIndex-2], int32(ltpCoefQ14[subframe][2]))
				ltpPredQ13 = smlaWB(ltpPredQ13, sLTPQ15[predLagIndex-3], int32(ltpCoefQ14[subframe][3]))
				ltpPredQ13 = smlaWB(ltpPredQ13, sLTPQ15[predLagIndex-4], int32(ltpCoefQ14[subframe][4]))
				predLagIndex++

				resQ14[i] = excQ14[subframeOffset+i] + (ltpPredQ13 << 1)
				sLTPQ15[sLTPBufIndex] = resQ14[i] << 1
				sLTPBufIndex++
			}
		} else {
			copy(resQ14[:n], excQ14[subframeOffset:subframeOffset+n])
		}

		for i := range n {
			lpcPredQ10 := int32(dLPC >> 1)
			for k := range dLPC {
				lpcPredQ10 = smlaWB(lpcPredQ10, sLPCQ14[maxSilkLPCOrder+i-k-1], int32(aQ12[k]))
			}
			sLPCQ14[maxSilkLPCOrder+i] = resQ14[i] + (lpcPredQ10 << 4)
			xq[subframeOffset+i] = saturate16(rshiftRound32(smulww(sLPCQ14[maxSilkLPCOrder+i], gainQ10), 8))
		}

		copy(sLPCQ14[:maxSilkLPCOrder], sLPCQ14[n:n+maxSilkLPCOrder])
	}

	copy(d.sLPCQ14Buf[:], sLPCQ14[:maxSilkLPCOrder])
}

func lpcAnalysisFilterFixed(out, in []int16, b []int16, length, order int) {
	for ix := order; ix < length; ix++ {
		out32Q12 := smulbb(int32(in[ix-1]), int32(b[0]))
		for j := 1; j < order; j++ {
			out32Q12 = smlaBB(out32Q12, int32(in[ix-1-j]), int32(b[j]))
		}
		out32Q12 = (int32(in[ix]) << 12) - out32Q12
		out[ix] = saturate16(rshiftRound32(out32Q12, 12))
	}
	clear(out[:min(order, len(out))])
}

func (d *Decoder) decodeFrame(
	out []float32,
	voiceActivityDetected bool,
	nanoseconds int,
	bandwidth Bandwidth,
	isFirstSilkFrameInOpusFrame bool,
	skipLTPScaling bool,
) error {
	d.resetPredictionForBandwidthChange(bandwidth)

	subframeCount := subframeCount(nanoseconds)

	signalType, quantizationOffsetType := d.determineFrameType(voiceActivityDetected)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.4
	gainQ16 := d.decodeSubframeQuantizations(signalType, subframeCount, isFirstSilkFrameInOpusFrame)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.1
	I1 := d.normalizeLineSpectralFrequencyStageOne(signalType == frameSignalTypeVoiced, bandwidth)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	dLPC, resQ10 := d.normalizeLineSpectralFrequencyStageTwo(bandwidth, I1)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.3
	nlsfQ15 := d.normalizeLineSpectralFrequencyCoefficients(dLPC, bandwidth, resQ10, I1)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.4
	d.normalizeLSFStabilization(nlsfQ15, dLPC, bandwidth)

	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
	n1Q15, wQ2 := d.normalizeLSFInterpolation(nlsfQ15, nanoseconds)

	// For 20 ms SILK frames, the first half of the frame (i.e., the first
	// two subframes) may use normalized LSF coefficients that are
	// interpolated between the decoded LSFs for the most recent coded frame
	// (in the same channel) and the current frame
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.5
	aQ12 := [][]float32{}
	aQ12 = d.generateAQ12(n1Q15, bandwidth, aQ12)
	aQ12 = d.generateAQ12(nlsfQ15, bandwidth, aQ12)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.1
	_, pitchLags := d.decodePitchLags(signalType, bandwidth, nanoseconds, isFirstSilkFrameInOpusFrame)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.2
	bQ7 := d.decodeLTPFilterCoefficients(signalType, subframeCount)

	// https://www.rfc-editor.org/rfc/rfc6716.html#section-4.2.7.6.3
	ltpScaleQ14 := d.decodeLTPScalingParameter(signalType, isFirstSilkFrameInOpusFrame && !skipLTPScaling)

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
	d.silkFrameReconstructionFixed(
		signalType, bandwidth,
		subframeCount,
		dLPC,
		bQ7,
		pitchLags,
		eQ23,
		ltpScaleQ14,
		wQ2,
		aQ12,
		gainQ16, out,
	)

	d.isPreviousFrameVoiced = signalType == frameSignalTypeVoiced

	// n0Q15 is the LSF coefficients decoded for the prior frame
	// see normalizeLSFInterpolation.
	if len(d.n0Q15) != len(nlsfQ15) {
		d.n0Q15 = make([]int16, len(nlsfQ15))
	}
	copy(d.n0Q15, nlsfQ15)

	d.saveFinalOutValues(out)
	d.haveDecoded = true

	return nil
}

func (d *Decoder) saveFinalOutValues(out []float32) {
	if len(out) >= len(d.finalOutValues) {
		copy(d.finalOutValues, out[len(out)-len(d.finalOutValues):])

		return
	}

	copy(d.finalOutValues, d.finalOutValues[len(out):])
	copy(d.finalOutValues[len(d.finalOutValues)-len(out):], out)
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
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.8
func (d *Decoder) stereoPhaseOneSampleCount(bandwidth Bandwidth) int {
	switch bandwidth {
	case BandwidthNarrowband:
		return 64
	case BandwidthMediumband:
		return 96
	case BandwidthWideband:
		return 128
	}

	return 0
}

func (d *Decoder) stereoPhaseOneDenominatorQ16(bandwidth Bandwidth) int32 {
	switch bandwidth {
	case BandwidthNarrowband:
		return 1024 // (1 << 16) / 64
	case BandwidthMediumband:
		return 682 // (1 << 16) / 96
	case BandwidthWideband:
		return 512 // (1 << 16) / 128
	}

	return 0
}

func (d *Decoder) delayMid(out []float32) {
	if len(out) == 0 {
		return
	}

	previousSample := d.previousMidValues[1]
	previousMidValues := d.previousMidValues
	if len(out) == 1 {
		previousMidValues[0] = previousMidValues[1]
		previousMidValues[1] = out[0]
	} else {
		previousMidValues[0] = out[len(out)-2]
		previousMidValues[1] = out[len(out)-1]
	}

	for i := range out {
		out[i], previousSample = previousSample, out[i]
	}

	d.previousMidValues = previousMidValues
}

// RFC 6716 Section 4.2.8 applies a one-sample delay to mono output so mono
// to stereo transitions remain seamless.
func (d *Decoder) delayMono(out []float32) {
	d.delayMid(out)
	d.previousSideValue = 0
	d.wasStereo = false
}

func silkSampleToPCM16(sample float32) int16 {
	switch {
	case sample >= 32767.0/32768.0:
		return math.MaxInt16
	case sample <= -1:
		return math.MinInt16
	default:
		return int16(sample * 32768) //nolint:gosec // Samples come from the fixed-point SILK core.
	}
}

// RFC 6716 Section 4.2.8 converts mid-side stereo to left-right stereo.
// Keep this stage fixed-point too: stereo_MS_to_LR.c interpolates Q13
// predictors and rounds the predicted side sample before the final L/R sum.
func (d *Decoder) stereoUnmix(mid, side, out []float32, w0Q13, w1Q13 int32, bandwidth Bandwidth) {
	phaseOneSampleCount := d.stereoPhaseOneSampleCount(bandwidth)
	pred0Q13 := d.previousStereoWeights[0]
	pred1Q13 := d.previousStereoWeights[1]
	denomQ16 := d.stereoPhaseOneDenominatorQ16(bandwidth)
	delta0Q13 := rshiftRound32(smulbb(w0Q13-pred0Q13, denomQ16), 16)
	delta1Q13 := rshiftRound32(smulbb(w1Q13-pred1Q13, denomQ16), 16)
	midPrev2Q0 := silkSampleToPCM16(d.previousMidValues[0])
	midPrev1Q0 := silkSampleToPCM16(d.previousMidValues[1])
	sidePrevQ0 := silkSampleToPCM16(d.previousSideValue)

	for i := range mid {
		if i < phaseOneSampleCount {
			pred0Q13 += delta0Q13
			pred1Q13 += delta1Q13
		} else {
			pred0Q13 = w0Q13
			pred1Q13 = w1Q13
		}

		midQ0 := silkSampleToPCM16(mid[i])
		sideQ0 := silkSampleToPCM16(side[i])
		predictedSideQ8 := int32(sidePrevQ0) << 8
		sumQ11 := (int32(midPrev2Q0) + 2*int32(midPrev1Q0) + int32(midQ0)) << 9
		predictedSideQ8 = smlaWB(predictedSideQ8, sumQ11, pred0Q13)
		predictedSideQ8 = smlaWB(predictedSideQ8, int32(midPrev1Q0)<<11, pred1Q13)
		predictedSideQ0 := saturate16(rshiftRound32(predictedSideQ8, 8))

		out[i*2] = float32(saturate16(int32(midPrev1Q0)+int32(predictedSideQ0))) / 32768
		out[i*2+1] = float32(saturate16(int32(midPrev1Q0)-int32(predictedSideQ0))) / 32768

		midPrev2Q0 = midPrev1Q0
		midPrev1Q0 = midQ0
		sidePrevQ0 = sideQ0
	}

	d.previousStereoWeights[0] = w0Q13
	d.previousStereoWeights[1] = w1Q13
	d.previousMidValues[0] = float32(midPrev2Q0) / 32768
	d.previousMidValues[1] = float32(midPrev1Q0) / 32768
	d.previousSideValue = float32(sidePrevQ0) / 32768
	d.wasStereo = true
}

// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.8
func (d *Decoder) decodeMono(
	out []float32,
	voiceActivityDetected []bool,
	frameSampleCount int,
	silkFrameNanoseconds int,
	bandwidth Bandwidth,
) error {
	for i := range voiceActivityDetected {
		frameOut := out[i*frameSampleCount : (i+1)*frameSampleCount]
		if err := d.decodeFrame(
			frameOut,
			voiceActivityDetected[i],
			silkFrameNanoseconds,
			bandwidth,
			i == 0,
			false,
		); err != nil {
			return err
		}
	}

	d.delayMono(out[:frameSampleCount*len(voiceActivityDetected)])

	return nil
}

func (d *Decoder) stereoScratchBuffers(frameSampleCount int) (mid, side []float32) {
	if cap(d.stereoMid) < frameSampleCount {
		d.stereoMid = make([]float32, frameSampleCount)
	}
	if cap(d.stereoSide) < frameSampleCount {
		d.stereoSide = make([]float32, frameSampleCount)
	}

	return d.stereoMid[:frameSampleCount], d.stereoSide[:frameSampleCount]
}

func (d *Decoder) writeStereoFrame(
	out []float32,
	mid []float32,
	side []float32,
	frameIndex int,
	frameSampleCount int,
	w0Q13 int32,
	w1Q13 int32,
	bandwidth Bandwidth,
	outputStereo bool,
) {
	if outputStereo {
		frameOut := out[frameIndex*frameSampleCount*2 : (frameIndex+1)*frameSampleCount*2]
		d.stereoUnmix(mid, side, frameOut, w0Q13, w1Q13, bandwidth)

		return
	}

	frameOut := out[frameIndex*frameSampleCount : (frameIndex+1)*frameSampleCount]
	copy(frameOut, mid)
}

func (d *Decoder) finishStereoOutput(out []float32, frameSampleCount int, frameCount int, outputStereo bool) {
	if outputStereo {
		return
	}

	// dec_API.c buffers the mid channel directly when the API requests mono
	// from an internally stereo SILK stream.
	d.delayMid(out[:frameSampleCount*frameCount])
	d.wasStereo = true
}

// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.1
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.2
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.8
func (d *Decoder) decodeStereo(
	out []float32,
	midVoiceActivityDetected []bool,
	sideVoiceActivityDetected []bool,
	frameSampleCount int,
	silkFrameNanoseconds int,
	bandwidth Bandwidth,
	outputStereo bool,
) error {
	if d.sideDecoder == nil {
		d.sideDecoder = newChannelDecoder()
	}
	// RFC 6716 Section 4.2.7.1 resets previous stereo weights on transitions
	// from mono to stereo.
	if !d.wasStereo {
		d.previousStereoWeights = [2]int32{}
		d.previousSideValue = 0
		d.sideDecoder = newChannelDecoder()
		d.resetSideDecoderPrediction()
		d.previousDecodeOnlyMid = false
	}

	mid, side := d.stereoScratchBuffers(frameSampleCount)

	isFirstSideFrame := true
	for i := range midVoiceActivityDetected {
		w0Q13, w1Q13 := d.decodeStereoPredictionWeights()
		midOnly := !sideVoiceActivityDetected[i] && d.decodeMidOnlyFlag()

		// RFC 6716 Sections 4.2.7.2, 4.2.7.4, and 4.2.7.6.1 restart side
		// prediction when the previous side frame was not coded.
		if !midOnly && d.previousDecodeOnlyMid {
			d.resetSideDecoderPrediction()
		}

		if err := d.decodeFrame(
			mid,
			midVoiceActivityDetected[i],
			silkFrameNanoseconds,
			bandwidth,
			i == 0,
			false,
		); err != nil {
			return err
		}

		if !midOnly {
			d.sideDecoder.rangeDecoder = d.rangeDecoder
			if err := d.sideDecoder.decodeFrame(
				side,
				sideVoiceActivityDetected[i],
				silkFrameNanoseconds,
				bandwidth,
				isFirstSideFrame,
				d.previousDecodeOnlyMid,
			); err != nil {
				return err
			}
			d.rangeDecoder = d.sideDecoder.rangeDecoder
			isFirstSideFrame = false
		} else {
			clear(side)
		}

		d.writeStereoFrame(out, mid, side, i, frameSampleCount, w0Q13, w1Q13, bandwidth, outputStereo)
		d.previousDecodeOnlyMid = midOnly
	}
	d.finishStereoOutput(out, frameSampleCount, len(midVoiceActivityDetected), outputStereo)

	return nil
}

// consumeLowBitrateRedundancy advances over RFC 6716 Sections 4.2.4 and 4.2.5
// LBRR syntax so regular SILK frames remain decodable. The redundant audio is
// intentionally discarded for now; exposing FEC recovery is separate from
// accepting valid packets that carry LBRR data.
func (d *Decoder) consumeLowBitrateRedundancy(
	midFlags []bool,
	sideFlags []bool,
	isStereo bool,
	silkFrameNanoseconds int,
	bandwidth Bandwidth,
) error {
	discard := NewDecoder()
	discard.rangeDecoder = d.rangeDecoder
	frameSampleCount := discard.samplesInSubframe(bandwidth) * subframeCount(silkFrameNanoseconds)
	midScratch := make([]float32, frameSampleCount)
	sideScratch := make([]float32, frameSampleCount)

	previousMidCoded := false
	previousSideCoded := false
	for i := range midFlags {
		midCoded := midFlags[i]
		sideCoded := isStereo && sideFlags[i]
		if err := discard.consumeLowBitrateRedundancyFrame(
			midScratch,
			sideScratch,
			midCoded,
			sideCoded,
			isStereo,
			i == 0 || !previousMidCoded,
			i == 0 || !previousSideCoded,
			silkFrameNanoseconds,
			bandwidth,
		); err != nil {
			return err
		}
		previousMidCoded = midCoded
		previousSideCoded = sideCoded
	}

	d.rangeDecoder = discard.rangeDecoder

	return nil
}

// consumeLowBitrateRedundancyFrame advances one redundant SILK frame through a
// throwaway decoder so the regular payload keeps the correct entropy position.
func (d *Decoder) consumeLowBitrateRedundancyFrame(
	midScratch []float32,
	sideScratch []float32,
	midCoded bool,
	sideCoded bool,
	isStereo bool,
	independentMid bool,
	independentSide bool,
	silkFrameNanoseconds int,
	bandwidth Bandwidth,
) error {
	if midCoded && isStereo {
		_, _ = d.decodeStereoPredictionWeights()
		if !sideCoded {
			_ = d.decodeMidOnlyFlag()
		}
	}
	if midCoded {
		if err := d.decodeFrame(
			midScratch,
			true,
			silkFrameNanoseconds,
			bandwidth,
			independentMid,
			false,
		); err != nil {
			return err
		}
	}
	if !sideCoded {
		return nil
	}

	d.sideDecoder.rangeDecoder = d.rangeDecoder
	if err := d.sideDecoder.decodeFrame(
		sideScratch,
		true,
		silkFrameNanoseconds,
		bandwidth,
		independentSide,
		false,
	); err != nil {
		return err
	}
	d.rangeDecoder = d.sideDecoder.rangeDecoder

	return nil
}

// Decode decodes one SILK frame of mono or stereo audio.
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.1
func (d *Decoder) Decode(
	in []byte,
	out []float32,
	isStereo bool,
	nanoseconds int,
	bandwidth Bandwidth,
) error {
	d.rangeDecoder.Init(in)

	return d.decodeWithInitializedRange(out, isStereo, isStereo, nanoseconds, bandwidth)
}

// DecodeWithRange decodes one SILK frame from an Opus range decoder shared
// with the CELT layer, as required by RFC 6716 hybrid packets.
func (d *Decoder) DecodeWithRange(
	rangeDecoder *rangecoding.Decoder,
	out []float32,
	isStereo bool,
	nanoseconds int,
	bandwidth Bandwidth,
) error {
	if rangeDecoder == nil {
		return errOutBufferTooSmall
	}
	d.rangeDecoder = *rangeDecoder
	err := d.decodeWithInitializedRange(out, isStereo, isStereo, nanoseconds, bandwidth)
	*rangeDecoder = d.rangeDecoder

	return err
}

// DecodeWithRangeToChannels decodes one SILK frame while matching the API
// output channel count selected by the outer Opus decoder.
func (d *Decoder) DecodeWithRangeToChannels(
	rangeDecoder *rangecoding.Decoder,
	out []float32,
	isStereo bool,
	outputChannelCount int,
	nanoseconds int,
	bandwidth Bandwidth,
) error {
	if rangeDecoder == nil {
		return errOutBufferTooSmall
	}
	d.rangeDecoder = *rangeDecoder
	err := d.decodeWithInitializedRange(out, isStereo, outputChannelCount == 2, nanoseconds, bandwidth)
	*rangeDecoder = d.rangeDecoder

	return err
}

func (d *Decoder) decodeWithInitializedRange(
	out []float32,
	isStereo bool,
	outputStereo bool,
	nanoseconds int,
	bandwidth Bandwidth,
) error {
	frameCount := silkFrameCount(nanoseconds)
	silkFrameNanoseconds := min(nanoseconds, nanoseconds20Ms)

	sfCount := subframeCount(silkFrameNanoseconds)
	subframeSize := d.samplesInSubframe(bandwidth)
	channelCount := 1
	if isStereo && outputStereo {
		channelCount = 2
	}
	switch {
	case frameCount == 0 || sfCount == 0:
		return errUnsupportedSilkFrameDuration
	case (subframeSize * sfCount * frameCount * channelCount) > len(out):
		return errOutBufferTooSmall
	}

	midVoiceActivityDetected, midLowBitRateRedundancy := d.decodeHeaderBits(frameCount)

	frameSampleCount := subframeSize * sfCount
	if !isStereo {
		midLowBitrateRedundancyFlags := d.decodeLowBitrateRedundancyFlags(frameCount, midLowBitRateRedundancy)
		if err := d.consumeLowBitrateRedundancy(
			midLowBitrateRedundancyFlags,
			nil,
			false,
			silkFrameNanoseconds,
			bandwidth,
		); err != nil {
			return err
		}

		return d.decodeMono(out, midVoiceActivityDetected, frameSampleCount, silkFrameNanoseconds, bandwidth)
	}

	sideVoiceActivityDetected, sideLowBitRateRedundancy := d.decodeHeaderBits(frameCount)
	midLowBitrateRedundancyFlags := d.decodeLowBitrateRedundancyFlags(frameCount, midLowBitRateRedundancy)
	sideLowBitrateRedundancyFlags := d.decodeLowBitrateRedundancyFlags(frameCount, sideLowBitRateRedundancy)
	if err := d.consumeLowBitrateRedundancy(
		midLowBitrateRedundancyFlags,
		sideLowBitrateRedundancyFlags,
		true,
		silkFrameNanoseconds,
		bandwidth,
	); err != nil {
		return err
	}

	return d.decodeStereo(
		out,
		midVoiceActivityDetected,
		sideVoiceActivityDetected,
		frameSampleCount,
		silkFrameNanoseconds,
		bandwidth,
		outputStereo,
	)
}
