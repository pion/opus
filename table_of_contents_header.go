package main

type (
	tableOfContentsHeader byte

	configuration     byte
	configurationMode byte
)

func (t tableOfContentsHeader) configuration() configuration {
	return configuration(t >> 3)
}

func (t tableOfContentsHeader) isStereo() bool {
	return false
}

func (t tableOfContentsHeader) numberOfFrames() byte {
	return 0
}

const (
	configurationModeSilkOnly configurationMode = iota + 1
	configurationModeCELTOnly
	configurationModeHybrid
)

func (c configuration) mode() configurationMode {
	switch {
	case c >= 0 && c <= 11:
		return configurationModeSilkOnly
	case c >= 12 && c <= 15:
		return configurationModeHybrid
	case c >= 16 && c <= 31:
		return configurationModeCELTOnly
	default:
		return 0
	}
}
