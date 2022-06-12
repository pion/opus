package opus

import "errors"

var (
	errTooShortForTableOfContentsHeader = errors.New("Packet is too short to contain table of contents header")

	errUnsupportedFrameCode                   = errors.New("unsupported frame code")
	errTooShortForArbitraryLengthFrames       = errors.New("packet is too short to contain arbitrary length frames")
	errArbitraryLengthFrameVBRUnsupported     = errors.New("arbitrary length frames with VBR is unsupported")
	errArbitraryLengthFramePaddingUnsupported = errors.New("arbitrary length frames with padding is unsupported")

	errUnsupportedConfigurationMode = errors.New("unsupported configuration mode")
)
