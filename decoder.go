package opus

import "fmt"

// Decoder decodes the Opus bitstream into PCM
type Decoder struct {
}

// Decode decodes the Opus bitstream into PCM
func (d *Decoder) Decode(in []byte) (bandwidth Bandwidth, isStereo bool, frames [][]byte, err error) {
	if len(in) < 1 {
		return 0, false, nil, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(in[0])
	cfg := tocHeader.configuration()

	switch tocHeader.frameCode() {
	case frameCodeOneFrame:
		frames = append(frames, in[1:])
	default:
		return 0, false, nil, fmt.Errorf("%w: %d", errUnsupportedFrameCode, tocHeader.frameCode())
	}

	return cfg.bandwidth(), tocHeader.isStereo(), nil, nil
}
