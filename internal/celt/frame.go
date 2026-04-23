// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import "fmt"

const (
	postFilterPitchBase               = 16
	postFilterGainStep                = 0.09375
	bitResolution                     = 3
	defaultSpreadDecision             = 2
	defaultAllocationTrim             = 5
	initialDynamicAllocationLogP      = 6
	minDynamicAllocationLogP          = 2
	allocationTrimBitCost             = 6
	firstTimeFrequencyChangeLogP      = 4
	firstTransientFrequencyChangeLogP = 2
	nextTimeFrequencyChangeLogP       = 5
	nextTransientFrequencyChangeLogP  = 4
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
	coarseEnergy    [2][maxBands]float32
	tfChange        [maxBands]int
	tfSelect        int
	spread          int
	bandBoost       [maxBands]int
	allocationTrim  int
	allocation      allocationState
	antiCollapseRsv int
}

type postFilter struct {
	enabled bool
	octave  int
	period  int
	gain    float32
	tapset  int
}

// decodeFrameSideInfo consumes the initial CELT symbols through the allocation
// header in the order specified by RFC 6716 Table 56. Pulse allocation and PVQ
// residual decoding are intentionally left to the following CELT slices.
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
	d.prepareCoarseEnergyHistory(&info)
	d.decodeCoarseEnergy(&info)
	d.decodeAllocationHeader(&info)
	d.decodeAllocationAndFineEnergy(&info)

	return info, nil
}

func (d *Decoder) prepareCoarseEnergyHistory(info *frameSideInfo) {
	if info.channelCount != 1 {
		return
	}

	// Mono coarse-energy prediction uses one history stream. When decoding mono
	// after stereo, seed it with the louder previous channel for each band before
	// decodeCoarseEnergy mirrors the decoded mono energy back to both channels.
	for band := range d.previousLogE[0] {
		d.previousLogE[0][band] = max(d.previousLogE[0][band], d.previousLogE[1][band])
	}
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
		lm:             lm,
		startBand:      cfg.startBand,
		endBand:        cfg.endBand,
		channelCount:   cfg.channelCount,
		spread:         defaultSpreadDecision,
		allocationTrim: defaultAllocationTrim,
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

// decodeCoarseEnergy decodes the RFC 6716 Section 4.3.2.1 fixed-resolution
// coarse energy deltas and updates the CELT previous-frame energy state.
func (d *Decoder) decodeCoarseEnergy(info *frameSideInfo) {
	probModel := eProbModel[info.lm][boolIndex(info.intraEnergy)]
	previousBandPrediction := [2]float32{}
	coef := energyPredictionCoefficients[info.lm]
	beta := energyBetaCoefficients[info.lm]
	if info.intraEnergy {
		coef = 0
		beta = energyIntraBeta
	}
	info.coarseEnergy = d.previousLogE

	for band := info.startBand; band < info.endBand; band++ {
		for channel := range info.channelCount {
			qi := d.decodeCoarseEnergyDelta(info, probModel[:], band)
			q := float32(qi)
			oldEnergy := max(float32(-9), d.previousLogE[channel][band])
			energy := coef*oldEnergy + previousBandPrediction[channel] + q

			d.previousLogE[channel][band] = energy
			info.coarseEnergy[channel][band] = energy
			previousBandPrediction[channel] += q - beta*q
		}
	}
	if info.channelCount == 1 {
		copy(d.previousLogE[1][:], d.previousLogE[0][:])
		copy(info.coarseEnergy[1][:], info.coarseEnergy[0][:])
	}
}

func (d *Decoder) decodeCoarseEnergyDelta(info *frameSideInfo, probModel []uint8, band int) int {
	tell := d.rangeDecoder.Tell()
	if tell >= info.totalBits {
		return -1
	}

	bitsLeft := info.totalBits - tell
	switch {
	case bitsLeft >= 15:
		probIndex := 2 * min(band, maxBands-1)

		return d.rangeDecoder.DecodeLaplace(
			uint32(probModel[probIndex])<<7,
			uint32(probModel[probIndex+1])<<6,
		)
	case bitsLeft >= 2:
		return smallEnergyDelta(d.rangeDecoder.DecodeSymbolWithICDF(icdfSmallEnergy))
	default:
		return -int(d.rangeDecoder.DecodeSymbolLogP(1))
	}
}

func (d *Decoder) decodeAllocationHeader(info *frameSideInfo) {
	d.decodeTimeFrequencyChanges(info)
	d.decodeSpread(info)
	totalBitsEighth := d.decodeDynamicAllocation(info, info.totalBits<<bitResolution)
	d.decodeAllocationTrim(info, totalBitsEighth)
}

// decodeAllocationAndFineEnergy follows RFC 6716 Section 4.3.3 after the
// allocation header: reserve the Section 4.3.5 anti-collapse bit, compute
// shape/fine-energy budgets, then decode the first fine-energy refinement pass.
func (d *Decoder) decodeAllocationAndFineEnergy(info *frameSideInfo) {
	totalBits := int(info.totalBits)           //nolint:gosec // G115: CELT frame bit counts are packet-bounded.
	tellFrac := int(d.rangeDecoder.TellFrac()) //nolint:gosec // G115: entropy cursor is packet-bounded.
	bits := (totalBits << bitResolution) - tellFrac - 1
	info.antiCollapseRsv = 0
	if info.transient && info.lm >= 2 && bits >= (info.lm+2)<<bitResolution {
		info.antiCollapseRsv = 1 << bitResolution
	}
	bits -= info.antiCollapseRsv
	info.allocation = d.computeAllocation(info, bits)
	d.decodeFineEnergy(info, info.allocation.fineQuant)
}

// decodeTimeFrequencyChanges decodes the RFC 6716 Section 4.3.1 per-band
// tf_change flags and optional tf_select bit, then maps them through Tables 60-63.
func (d *Decoder) decodeTimeFrequencyChanges(info *frameSideInfo) {
	logP := firstTimeFrequencyChangeLogP
	if info.transient {
		logP = firstTransientFrequencyChangeLogP
	}

	budget := info.totalBits
	tell := d.rangeDecoder.Tell()
	tfSelectReserved := info.lm > 0 && tell+uint(logP)+1 <= budget
	if tfSelectReserved {
		budget--
	}

	current := 0
	changed := 0
	for band := info.startBand; band < info.endBand; band++ {
		if tell+uint(logP) <= budget {
			current ^= int(d.rangeDecoder.DecodeSymbolLogP(uint(logP)))
			tell = d.rangeDecoder.Tell()
			changed |= current
		}
		info.tfChange[band] = current

		if info.transient {
			logP = nextTransientFrequencyChangeLogP
		} else {
			logP = nextTimeFrequencyChangeLogP
		}
	}

	info.tfSelect = 0
	table := tfSelectTable[info.lm]
	if tfSelectReserved &&
		table[4*boolIndex(info.transient)+changed] !=
			table[4*boolIndex(info.transient)+2+changed] {
		info.tfSelect = int(d.rangeDecoder.DecodeSymbolLogP(1))
	}

	for band := info.startBand; band < info.endBand; band++ {
		info.tfChange[band] = int(table[4*boolIndex(info.transient)+2*info.tfSelect+info.tfChange[band]])
	}
}

func (d *Decoder) decodeSpread(info *frameSideInfo) {
	info.spread = defaultSpreadDecision
	if d.rangeDecoder.Tell()+4 <= info.totalBits {
		info.spread = int(d.rangeDecoder.DecodeSymbolWithICDF(icdfSpread))
	}
}

// decodeDynamicAllocation decodes RFC 6716 Section 4.3.3 band boost offsets in
// 1/8-bit units and returns the boost-adjusted total bit budget in 1/8-bit units.
func (d *Decoder) decodeDynamicAllocation(info *frameSideInfo, totalBitsEighth uint) uint {
	caps := allocationCaps(info.lm, info.channelCount)
	dynamicAllocationLogP := initialDynamicAllocationLogP
	tellFrac := d.rangeDecoder.TellFrac()

	for band := info.startBand; band < info.endBand; band++ {
		width := info.channelCount * (int(bandEdges[band+1]-bandEdges[band]) << info.lm)
		quanta := min(width<<bitResolution, max(allocationTrimBitCost<<bitResolution, width))
		quantaBits := uint(quanta) // #nosec G115 -- quanta is positive by construction from CELT band widths.
		loopLogP := dynamicAllocationLogP
		boost := 0

		for tellFrac+uint(loopLogP<<bitResolution) < totalBitsEighth && boost < caps[band] {
			flag := d.rangeDecoder.DecodeSymbolLogP(uint(loopLogP))
			tellFrac = d.rangeDecoder.TellFrac()
			if flag == 0 {
				break
			}

			boost += quanta
			if quantaBits >= totalBitsEighth {
				totalBitsEighth = 0
			} else {
				totalBitsEighth -= quantaBits
			}
			loopLogP = 1
		}

		info.bandBoost[band] = boost
		if boost > 0 {
			dynamicAllocationLogP = max(minDynamicAllocationLogP, dynamicAllocationLogP-1)
		}
	}

	return totalBitsEighth
}

func (d *Decoder) decodeAllocationTrim(info *frameSideInfo, totalBitsEighth uint) {
	info.allocationTrim = defaultAllocationTrim
	if d.rangeDecoder.TellFrac()+uint(allocationTrimBitCost<<bitResolution) <= totalBitsEighth {
		info.allocationTrim = int(d.rangeDecoder.DecodeSymbolWithICDF(icdfAllocationTrim))
	}
}

func allocationCaps(lm, channelCount int) [maxBands]int {
	caps := [maxBands]int{}
	indexBase := maxBands * (2*lm + channelCount - 1)
	for band := range maxBands {
		width := int(bandEdges[band+1]-bandEdges[band]) << lm
		caps[band] = (int(bandCaps[indexBase+band]) + 64) * channelCount * width >> 2
	}

	return caps
}

func smallEnergyDelta(symbol uint32) int {
	switch symbol {
	case 1:
		return -1
	case 2:
		return 1
	default:
		return 0
	}
}

func boolIndex(v bool) int {
	if v {
		return 1
	}

	return 0
}
