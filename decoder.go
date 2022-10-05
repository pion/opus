// Package opus provides a Opus Audio Codec RFC 6716 implementation
package opus

import (
	"fmt"

	"github.com/pion/opus/internal/bitdepth"
	"github.com/pion/opus/internal/silk"
)

// Decoder decodes the Opus bitstream into PCM
type Decoder struct {
	silkDecoder silk.Decoder
	silkBuffer  []float32
}

// NewDecoder creates a new Opus Decoder
func NewDecoder() Decoder {
	return Decoder{
		silkDecoder: silk.NewDecoder(),
		silkBuffer:  make([]float32, 320),
	}
}

// Decode decodes the Opus bitstream into PCM
func (d *Decoder) Decode(in []byte, out []byte) (bandwidth Bandwidth, isStereo bool, err error) {
	if len(in) < 1 {
		return 0, false, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	var encodedFrames [][]byte
	switch tocHeader.frameCode() {
	case frameCodeOneFrame:
		encodedFrames = append(encodedFrames, in[1:])
	default:
		return 0, false, fmt.Errorf("%w: %d", errUnsupportedFrameCode, tocHeader.frameCode())
	}

	if cfg.mode() != configurationModeSilkOnly {
		return 0, false, fmt.Errorf("%w: %d", errUnsupportedConfigurationMode, cfg.mode())
	}

	for _, encodedFrame := range encodedFrames {
		err := d.silkDecoder.Decode(encodedFrame, d.silkBuffer, tocHeader.isStereo(), cfg.frameDuration().nanoseconds(), silk.Bandwidth(cfg.bandwidth()))
		if err != nil {
			return 0, false, err
		}
	}

	if err := bitdepth.ConvertFloat32LittleEndianToSigned16LittleEndian(d.silkBuffer, out, 3); err != nil {
		return 0, false, err
	}

	return cfg.bandwidth(), tocHeader.isStereo(), nil
}
