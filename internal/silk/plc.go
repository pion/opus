// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// DecodePLC conceals a missing SILK packet while matching the channel count
// selected by the outer Opus decoder.
//
//nolint:cyclop
func (d *Decoder) DecodePLC(
	out []float32,
	isStereo bool,
	outputChannelCount int,
	nanoseconds int,
	bandwidth Bandwidth,
) error {
	frameCount := silkFrameCount(nanoseconds)
	silkFrameNanoseconds := min(nanoseconds, nanoseconds20Ms)
	sfCount := subframeCount(silkFrameNanoseconds)
	subframeSize := d.samplesInSubframe(bandwidth)
	if outputChannelCount != 1 && outputChannelCount != 2 {
		return errOutBufferTooSmall
	}

	channelCount := 1
	if isStereo && outputChannelCount == 2 {
		channelCount = 2
	}
	frameSampleCount := subframeSize * sfCount
	switch {
	case frameCount == 0 || sfCount == 0:
		return errUnsupportedSilkFrameDuration
	case frameSampleCount*frameCount*channelCount > len(out):
		return errOutBufferTooSmall
	}

	if !isStereo {
		for frame := range frameCount {
			frameOut := out[frame*frameSampleCount : (frame+1)*frameSampleCount]
			d.concealFrame(frameOut, bandwidth)
		}
		d.delayMono(out[:frameSampleCount*frameCount])
		// dec_API.c removes independent-gain clamping after every lost packet
		// so a falling signal does not bounce back on recovery.
		d.previousLogGain = 10

		return nil
	}

	if d.sideDecoder == nil {
		d.sideDecoder = newChannelDecoder()
	}
	mid, side := d.stereoScratchBuffers(frameSampleCount)
	for frame := range frameCount {
		d.concealFrame(mid, bandwidth)
		if d.previousDecodeOnlyMid {
			clear(side)
		} else {
			d.sideDecoder.concealFrame(side, bandwidth)
		}
		d.writeStereoFrame(
			out,
			mid,
			side,
			frame,
			frameSampleCount,
			d.previousStereoWeights[0],
			d.previousStereoWeights[1],
			bandwidth,
			outputChannelCount == 2,
		)
	}
	d.finishStereoOutput(out, frameSampleCount, frameCount, outputChannelCount == 2)
	d.previousLogGain = 10
	d.sideDecoder.previousLogGain = 10

	return nil
}

func (d *Decoder) concealFrame(out []float32, bandwidth Bandwidth) {
	// Startup PLC and an uncoded stereo side have no prediction history.
	if !d.haveDecoded {
		clear(out)

		return
	}

	d.concealFrameFixed(out, bandwidth)
}
