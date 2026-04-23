// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gosec,varnamelen // CELT decode keeps RFC/reference branch structure and vector naming.
package celt

import "github.com/pion/opus/internal/rangecoding"

// Decoder maintains state for the RFC 6716 Section 4.3 CELT layer.
type Decoder struct {
	mode           *Mode
	rangeDecoder   rangecoding.Decoder
	previousLogE   [2][maxBands]float32
	previousLogE1  [2][maxBands]float32
	previousLogE2  [2][maxBands]float32
	overlap        [2][]float32
	postfilterMem  [2][]float32
	postfilter     postFilterState
	preemphasisMem [2]float32
	rng            uint32
	lossCount      int
}

// NewDecoder creates a CELT decoder with the static Opus 48 kHz mode.
func NewDecoder() Decoder {
	decoder := Decoder{mode: DefaultMode()}
	decoder.Reset()

	return decoder
}

// Reset clears frame-to-frame CELT decode state.
func (d *Decoder) Reset() {
	d.mode = DefaultMode()
	d.rangeDecoder = rangecoding.Decoder{}
	clear(d.previousLogE[0][:])
	clear(d.previousLogE[1][:])
	for channel := range d.previousLogE1 {
		for band := range d.previousLogE1[channel] {
			d.previousLogE1[channel][band] = -28
			d.previousLogE2[channel][band] = -28
		}
	}
	clear(d.preemphasisMem[:])
	d.postfilter = postFilterState{}
	d.rng = 0
	d.lossCount = 0

	for channelIndex := range d.overlap {
		if cap(d.overlap[channelIndex]) < shortBlockSampleCount {
			d.overlap[channelIndex] = make([]float32, shortBlockSampleCount)
		}
		clear(d.overlap[channelIndex])
		if cap(d.postfilterMem[channelIndex]) < postfilterHistorySampleCount {
			d.postfilterMem[channelIndex] = make([]float32, postfilterHistorySampleCount)
		}
		clear(d.postfilterMem[channelIndex])
	}
}

// Decode decodes one CELT frame into interleaved 48 kHz float PCM.
func (d *Decoder) Decode(
	in []byte,
	out []float32,
	isStereo bool,
	outputChannelCount int,
	frameSampleCount int,
	startBand int,
	endBand int,
) error {
	return d.decode(in, out, isStereo, outputChannelCount, frameSampleCount, startBand, endBand, nil)
}

// DecodeWithRange decodes one CELT frame using an Opus range decoder shared
// with the SILK layer in hybrid packets.
func (d *Decoder) DecodeWithRange(
	in []byte,
	out []float32,
	isStereo bool,
	outputChannelCount int,
	frameSampleCount int,
	startBand int,
	endBand int,
	rangeDecoder *rangecoding.Decoder,
) error {
	return d.decode(in, out, isStereo, outputChannelCount, frameSampleCount, startBand, endBand, rangeDecoder)
}

func (d *Decoder) decode(
	in []byte,
	out []float32,
	isStereo bool,
	outputChannelCount int,
	frameSampleCount int,
	startBand int,
	endBand int,
	rangeDecoder *rangecoding.Decoder,
) error {
	channelCount := 1
	if isStereo {
		channelCount = 2
	}
	if outputChannelCount != 1 && outputChannelCount != 2 {
		return errInvalidChannelCount
	}
	if len(out) < frameSampleCount*outputChannelCount {
		return errInvalidFrameSize
	}

	cfg := frameConfig{
		frameSampleCount:   frameSampleCount,
		startBand:          startBand,
		endBand:            endBand,
		channelCount:       channelCount,
		outputChannelCount: outputChannelCount,
	}
	// The reference decoder routes empty and one-byte CELT frames to PLC before
	// trying to parse side information.
	if len(in) <= 1 {
		lostInfo, validateErr := d.validateFrameConfig(cfg)
		if validateErr != nil {
			return validateErr
		}
		d.decodeLostFrame(&lostInfo, out[:frameSampleCount*outputChannelCount])

		return nil
	}

	info, err := d.decodeFrameSideInfo(in, cfg, rangeDecoder)
	if err != nil {
		return err
	}
	if info.silence {
		x := make([]float32, frameSampleCount)
		var y []float32
		if isStereo {
			y = make([]float32, frameSampleCount)
		}
		for channel := range info.channelCount {
			for band := info.startBand; band < info.endBand; band++ {
				d.previousLogE[channel][band] = -28
			}
		}
		d.denormaliseAndSynthesize(&info, x, y, [2][maxBands]float32{}, out)
		d.updateLogEHistory(&info)
		d.resetInactiveBandState(&info)
		d.rng = d.rangeDecoder.FinalRange()
		d.lossCount = 0
		if rangeDecoder != nil {
			*rangeDecoder = d.rangeDecoder
		}

		return nil
	}

	// RFC 6716 Sections 4.3.4 through 4.3.7 decode the normalized residual,
	// optionally repair collapsed transient blocks, then synthesize PCM.
	x := make([]float32, frameSampleCount)
	var y []float32
	if isStereo {
		y = make([]float32, frameSampleCount)
	}
	state := bandDecodeState{
		rangeDecoder: &d.rangeDecoder,
		seed:         d.rng,
	}
	totalBits := (int(info.totalBits) << bitResolution) - info.antiCollapseRsv
	collapseMasks := quantAllBands(&info, x, y, totalBits, &state)
	antiCollapseOn := false
	if info.antiCollapseRsv > 0 {
		antiCollapseOn = d.rangeDecoder.DecodeRawBits(1) != 0
	}
	bitsLeft := int(info.totalBits) - int(d.rangeDecoder.Tell())
	d.finalizeFineEnergy(&info, info.allocation.fineQuant, info.allocation.finePriority, bitsLeft)
	if antiCollapseOn {
		d.antiCollapse(&info, x, y, collapseMasks, state.seed)
	}

	bandEnergy := d.log2Amp(&info)
	d.denormaliseAndSynthesize(&info, x, y, bandEnergy, out)
	d.updateLogEHistory(&info)
	d.resetInactiveBandState(&info)
	d.rng = d.rangeDecoder.FinalRange()
	d.lossCount = 0
	if rangeDecoder != nil {
		*rangeDecoder = d.rangeDecoder
	}

	return nil
}

func (d *Decoder) decodeLostFrame(info *frameSideInfo, out []float32) {
	clear(out)
	decay := float32(1.5)
	if d.lossCount > 0 {
		decay = 0.5
	}
	for channel := range info.channelCount {
		for band := info.startBand; band < info.endBand; band++ {
			d.previousLogE[channel][band] -= decay
		}
	}
	if info.channelCount == 1 {
		copy(d.previousLogE[1][:], d.previousLogE[0][:])
	}
	d.resetInactiveBandState(info)
	for channel := range d.overlap {
		clear(d.overlap[channel])
		clear(d.postfilterMem[channel])
	}
	clear(d.preemphasisMem[:])
	d.rangeDecoder = rangecoding.Decoder{}
	d.lossCount++
}

// Mode returns the static CELT mode used by this decoder.
func (d *Decoder) Mode() *Mode {
	if d.mode == nil {
		d.mode = DefaultMode()
	}

	return d.mode
}

// FinalRange exposes the range coder state for RFC conformance tests.
func (d *Decoder) FinalRange() uint32 {
	return d.rangeDecoder.FinalRange()
}
