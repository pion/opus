package silk

import (
	"fmt"

	"github.com/pion/opus/internal/rangecoding"
)

type (
	frameSignalType             byte
	frameQuantizationOffsetType byte
)

const (
	nanoseconds20Ms = 20000000

	frameSignalTypeInactive frameSignalType = iota + 1
	frameSignalTypeUnvoiced
	frameSignalTypeVoiced

	frameQuantizationOffsetTypeLow frameQuantizationOffsetType = iota + 1
	frameQuantizationOffsetTypeHigh
)

var (
	// +----------+-----------------------------+
	// | VAD Flag | PDF                         |
	// +----------+-----------------------------+
	// | Inactive | {26, 230, 0, 0, 0, 0}/256   |
	// |          |                             |
	// | Active   | {0, 0, 24, 74, 148, 10}/256 |
	// +----------+-----------------------------+
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.3
	icdfFrameTypeVADInactive = []uint{256, 26, 256}
	icdfFrameTypeVADActive   = []uint{256, 24, 98, 246, 256}
)

// Decoder maintains the state needed to decode a stream
// of Silk frames
type Decoder struct {
	rangeDecoder rangecoding.Decoder
}

// NewDecoder creates a new Silk Decoder
func NewDecoder() *Decoder {
	return &Decoder{}
}

// Decode decodes many SILK subframes
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

	// Each SILK frame contains a single "frame type" symbol that jointly
	// codes the signal type and quantization offset type of the
	// corresponding frame.
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.3
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

	signalType := frameSignalType(0)
	quantizationOffsetType := frameQuantizationOffsetType(0)

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

	fmt.Println(signalType)
	fmt.Println(quantizationOffsetType)

	return
}
