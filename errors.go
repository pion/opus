package opus

import "errors"

var (
	errTooShortForTableOfContentsHeader = errors.New("packet is too short to contain table of contents header")

	errUnsupportedFrameCode = errors.New("unsupported frame code")

	errUnsupportedConfigurationMode = errors.New("unsupported configuration mode")
)
