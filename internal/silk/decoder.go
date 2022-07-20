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

	//   +------------+-------------+--------------------------+
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

// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.1
func (d *Decoder) Decode(in []byte, isStereo bool, nanoseconds int) (decoded []byte, err error) {
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

	d.decodeSubframeQuantizations(signalType)
	return
}
