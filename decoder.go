package opus

import (
	"fmt"

	"github.com/pion/opus/internal/silk"
)

// Decoder decodes the Opus bitstream into PCM
type Decoder struct {
	silkDecoder silk.Decoder
}

// NewDecoder creates a new Opus Decoder
func NewDecoder() *Decoder {
	return &Decoder{}
}

// Decode decodes the Opus bitstream into PCM
func (d *Decoder) Decode(in []byte) (bandwidth Bandwidth, isStereo bool, frames [][]byte, err error) {
	if len(in) < 1 {
		return 0, false, nil, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	var encodedFrames [][]byte
	switch tocHeader.frameCode() {
	case frameCodeOneFrame:
		encodedFrames = append(encodedFrames, in[1:])
	default:
		return 0, false, nil, fmt.Errorf("%w: %d", errUnsupportedFrameCode, tocHeader.frameCode())
	}

	if cfg.mode() != configurationModeSilkOnly {
		return 0, false, nil, fmt.Errorf("%w: %d", errUnsupportedConfigurationMode, cfg.mode())
	}

	for _, encodedFrame := range encodedFrames {
		decoded, err := d.silkDecoder.Decode(encodedFrame, []byte{}, tocHeader.isStereo(), cfg.frameDuration().nanoseconds(), silk.Bandwidth(cfg.bandwidth()))
		if err != nil {
			return 0, false, nil, err
		}

		frames = append(frames, decoded)
	}

	return cfg.bandwidth(), tocHeader.isStereo(), frames, nil
}
