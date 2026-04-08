// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import "fmt"

const (
	postFilterPitchBase = 16
	postFilterGainStep  = 0.09375
)

type frameConfig struct {
	frameSampleCount int
	startBand        int
	endBand          int
	channelCount     int
}

type frameSideInfo struct {
	lm              int
	totalBits       uint
	startBand       int
	endBand         int
	channelCount    int
	silence         bool
	postFilter      postFilter
	transient       bool
	shortBlockCount int
	intraEnergy     bool
}

type postFilter struct {
	enabled bool
	octave  int
	period  int
	gain    float32
	tapset  int
}

// decodeFrameSideInfo consumes the initial CELT symbols through the intra-energy
// flag in the order specified by RFC 6716 Table 56. Coarse energy decoding,
// TF changes, allocation, and PVQ residual decoding are intentionally left to
// the following CELT slices.
func (d *Decoder) decodeFrameSideInfo(data []byte, cfg frameConfig) (frameSideInfo, error) {
	info, err := d.validateFrameConfig(cfg)
	if err != nil {
		return frameSideInfo{}, err
	}

	info.totalBits = uint(len(data) * 8)
	d.rangeDecoder.Init(data)

	d.decodeSilenceFlag(&info)
	if info.silence {
		return info, nil
	}

	if err = d.decodePostFilter(&info); err != nil {
		return frameSideInfo{}, err
	}
	d.decodeTransientFlag(&info)
	d.decodeIntraEnergyFlag(&info)

	return info, nil
}

func (d *Decoder) validateFrameConfig(cfg frameConfig) (frameSideInfo, error) {
	// RFC 6716 Section 4.3.3 defines LM as log2(frame_size/120).
	lm, err := d.Mode().LMForFrameSampleCount(cfg.frameSampleCount)
	if err != nil {
		return frameSideInfo{}, err
	}
	if cfg.startBand < 0 || cfg.startBand >= d.Mode().BandCount() {
		return frameSideInfo{}, errInvalidBand
	}
	if cfg.endBand <= cfg.startBand || cfg.endBand > d.Mode().BandCount() {
		return frameSideInfo{}, errInvalidBand
	}
	if cfg.channelCount != 1 && cfg.channelCount != 2 {
		return frameSideInfo{}, errInvalidChannelCount
	}

	return frameSideInfo{
		lm:           lm,
		startBand:    cfg.startBand,
		endBand:      cfg.endBand,
		channelCount: cfg.channelCount,
	}, nil
}

func (d *Decoder) decodeSilenceFlag(info *frameSideInfo) {
	tell := d.rangeDecoder.Tell()
	switch {
	case tell >= info.totalBits:
		info.silence = true
	case tell == 1:
		// RFC 6716 Table 56 starts CELT frames with a {32767,1}/32768 silence flag.
		info.silence = d.rangeDecoder.DecodeSymbolLogP(15) == 1
	}
}

// decodePostFilter decodes the optional pitch post-filter header fields listed
// in RFC 6716 Table 56: enable flag, octave, raw period suffix, raw gain, and tapset.
func (d *Decoder) decodePostFilter(info *frameSideInfo) error {
	// The reference decoder only reads the pitch post-filter when CELT start==0
	// and there are at least 16 conservative whole bits left in the frame.
	if info.startBand != 0 || d.rangeDecoder.Tell()+16 > info.totalBits {
		return nil
	}
	if d.rangeDecoder.DecodeSymbolLogP(1) == 0 {
		return nil
	}

	octave, ok := d.rangeDecoder.DecodeUniform(6)
	if !ok {
		return fmt.Errorf("%w: post-filter octave", errRangeCoderSymbol)
	}
	// RFC 6716 Table 56 stores the post-filter period/gain as raw tail bits,
	// not as range-coded symbols.
	rawPeriod := d.rangeDecoder.DecodeRawBits(4 + uint(octave))
	rawGain := d.rangeDecoder.DecodeRawBits(3)

	info.postFilter = postFilter{
		enabled: true,
		octave:  int(octave),
		period:  (postFilterPitchBase << octave) + int(rawPeriod) - 1,
		gain:    postFilterGainStep * float32(rawGain+1),
	}

	if d.rangeDecoder.Tell()+2 <= info.totalBits {
		info.postFilter.tapset = int(d.rangeDecoder.DecodeSymbolWithICDF(icdfTapset))
	}

	return nil
}

// decodeTransientFlag decodes the RFC 6716 Section 4.3.1 global transient flag.
// 2.5 ms CELT frames cannot be split further, so they do not code this symbol.
func (d *Decoder) decodeTransientFlag(info *frameSideInfo) {
	if info.lm == 0 || d.rangeDecoder.Tell()+3 > info.totalBits {
		return
	}

	info.transient = d.rangeDecoder.DecodeSymbolLogP(3) == 1
	if info.transient {
		info.shortBlockCount = 1 << info.lm
	}
}

// decodeIntraEnergyFlag decodes the RFC 6716 Section 4.3.2.1 flag that selects
// intra-frame coarse-energy prediction. The coarse energy itself is decoded later.
func (d *Decoder) decodeIntraEnergyFlag(info *frameSideInfo) {
	if d.rangeDecoder.Tell()+3 > info.totalBits {
		return
	}

	info.intraEnergy = d.rangeDecoder.DecodeSymbolLogP(3) == 1
}
