package silk

import (
	"fmt"

	"github.com/pion/opus/internal/rangecoding"
)

const (
	nanoseconds20Ms = 20000000
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
func (d *Decoder) Decode(in []byte, isStereo bool, nanoseconds int) (samples int, decoded []byte, err error) {
	if nanoseconds != nanoseconds20Ms {
		return 0, nil, errUnsupportedSilkFrameDuration
	} else if isStereo {
		return 0, nil, errUnsupportedSilkStereo
	}

	d.rangeDecoder.Init(in)

	voiceActivityDetected := d.rangeDecoder.DecodeSymbolLogP(1) == 1
	lowBitRateRedundancy := d.rangeDecoder.DecodeSymbolLogP(1) == 1
	if lowBitRateRedundancy {
		return 0, nil, errUnsupportedSilkLowBitrateRedundancy
	}

	if voiceActivityDetected {
		fmt.Println("VAD")
	}

	return
}
