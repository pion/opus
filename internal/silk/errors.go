package silk

import "errors"

var (
	errUnsupportedSilkFrameDuration        = errors.New("only silk frames with a duration of 20ms supported")
	errUnsupportedSilkStereo               = errors.New("silk decoder does not support stereo")
	errUnsupportedSilkLowBitrateRedundancy = errors.New("silk decoder does not low bit-rate redundancy")
)
