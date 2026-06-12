// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

const (
	plcFirstHarmonicAttenuation = 0.99
	plcLaterHarmonicAttenuation = 0.95
	plcFirstNoiseAttenuation    = 0.99
	plcLaterNoiseAttenuation    = 0.90
	plcRandomHistorySize        = 128
)

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

	return nil
}

func (d *Decoder) concealFrame(out []float32, bandwidth Bandwidth) {
	clear(out)
	if !d.haveDecoded {
		return
	}

	history := d.finalOutValues
	if d.isPreviousFrameVoiced {
		d.concealVoiced(out, history, bandwidth)
	} else {
		d.concealUnvoiced(out, history, bandwidth)
	}

	d.plcConcealedEnergy = signalEnergy(out)
	d.plcLossCount++
	d.savePLCState(out)
}

func (d *Decoder) concealVoiced(out, history []float32, bandwidth Bandwidth) {
	lag := d.previousLag
	if len(d.pitchLags) > 0 {
		lag = d.pitchLags[len(d.pitchLags)-1]
	}
	lag = max(1, min(lag, len(history)))

	attenuationStep := float32(plcFirstHarmonicAttenuation)
	if d.plcLossCount > 0 {
		attenuationStep = plcLaterHarmonicAttenuation
	}
	attenuation := attenuationStep
	subframeSize := max(1, d.samplesInSubframe(bandwidth))
	for i := range out {
		if i > 0 && i%subframeSize == 0 {
			attenuation *= attenuationStep
		}

		var sample float32
		if i >= lag {
			sample = out[i-lag]
		} else {
			sample = history[len(history)-lag+i]
		}

		d.plcRandSeed = 196314165*d.plcRandSeed + 907633515
		noiseIndex := int(d.plcRandSeed % plcRandomHistorySize)
		noise := history[len(history)-1-noiseIndex] * (1 - attenuation) * 0.1
		out[i] = clampNegativeOneToOne(sample*attenuation + noise)
	}

	maxLag := d.samplesInSubframe(bandwidth) * 18 / 5
	d.previousLag = min(maxLag, lag+(lag+50)/100)
}

func (d *Decoder) concealUnvoiced(out, history []float32, bandwidth Bandwidth) {
	attenuationStep := float32(plcFirstNoiseAttenuation)
	if d.plcLossCount > 0 {
		attenuationStep = plcLaterNoiseAttenuation
	}
	attenuation := attenuationStep
	subframeSize := max(1, d.samplesInSubframe(bandwidth))
	randomStart := len(history) - plcRandomHistorySize
	for i := range out {
		if i > 0 && i%subframeSize == 0 {
			attenuation *= attenuationStep
		}
		d.plcRandSeed = 196314165*d.plcRandSeed + 907633515
		source := history[randomStart+int(d.plcRandSeed%plcRandomHistorySize)]
		out[i] = clampNegativeOneToOne(source * attenuation)
	}
}

func (d *Decoder) savePLCState(out []float32) {
	d.saveFinalOutValues(out)
	dLPC := len(d.n0Q15)
	if dLPC == 0 {
		return
	}
	if cap(d.previousFrameLPCValues) < dLPC {
		d.previousFrameLPCValues = make([]float32, dLPC)
	} else {
		d.previousFrameLPCValues = d.previousFrameLPCValues[:dLPC]
	}
	if len(out) >= dLPC {
		copy(d.previousFrameLPCValues, out[len(out)-dLPC:])
	}
}

func (d *Decoder) gluePLCFrame(out []float32) {
	if d.plcLossCount == 0 {
		return
	}

	decodedEnergy := signalEnergy(out)
	if decodedEnergy > d.plcConcealedEnergy && decodedEnergy > 0 {
		gain := float32(0)
		if d.plcConcealedEnergy > 0 {
			gain = float32(math.Sqrt(d.plcConcealedEnergy / decodedEnergy))
		}
		rampSamples := max(1, len(out)/4)
		gainStep := (1 - gain) / float32(rampSamples)
		for i := range rampSamples {
			out[i] *= gain
			gain = min(float32(1), gain+gainStep)
		}
	}

	d.plcLossCount = 0
	d.plcConcealedEnergy = 0
}

func signalEnergy(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var energy float64
	for _, sample := range samples {
		energy += float64(sample) * float64(sample)
	}

	return energy / float64(len(samples))
}
