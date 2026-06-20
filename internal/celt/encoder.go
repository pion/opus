// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:gosec // G115/G602: integer conversions are bounded by CELT frame and band sizes.
package celt

import "github.com/pion/opus/internal/rangecoding"

// Encoder encodes PCM audio into CELT-only Opus frames.
//
// It maintains the inter-frame state required by RFC 6716 Section 5.3:
// the previous log-energy per band used to predict coarse energy deltas
// (Section 5.3.3), and the analysis state (pre-emphasis memory and MDCT
// overlap buffer) needed to produce a continuous bitstream across frames.
//
// The encoder and decoder share the same deterministic bit-allocation
// algorithm (computeAllocation, Section 5.3.4). After encoding a sequence
// of symbols the value of rng must match the decoder's rng exactly
// (RFC 6716 Section 5.1) — use FinalRange on both sides to verify this
// invariant during testing.
type Encoder struct {
	mode         *Mode
	rangeEncoder rangecoding.Encoder
	rng          uint32

	previousLogE  [2][maxBands]float32
	previousLogE1 [2][maxBands]float32
	previousLogE2 [2][maxBands]float32

	analysis    analysisState
	mdctScratch forwardMDCTScratch
	fftScratch  []complex32

	bandNorm          []float32
	bandLowScratch    []float32
	bandCollapseMasks []byte
	pvqY              [2][]int
	pvqAbsX           [2][]float32
	pvqSign           [2][]float32
	cwrsScratch       []uint32
	normalisedBands   [2][]float32
}

func NewEncoder() Encoder {
	encoder := Encoder{mode: DefaultMode()}
	encoder.Reset()

	return encoder
}

func (e *Encoder) Reset() {
	e.mode = DefaultMode()
	e.rangeEncoder = rangecoding.Encoder{}
	e.rng = 0
	e.analysis = newAnalysisState()

	clear(e.previousLogE[0][:])
	clear(e.previousLogE[1][:])

	for channel := range e.previousLogE1 {
		for band := range e.previousLogE1[channel] {
			e.previousLogE1[channel][band] = -28
			e.previousLogE2[channel][band] = -28
		}
	}

	// Pre-allocate every buffer that quantAllBands* and algQuant touch so
	// EncodeFrame stays at zero allocs per frame.
	// normalisedBands/bandNorm need maxFrameSampleCount per channel — the full
	// MDCT spectrum (960 bins), not just the coded band range (800 bins).
	// pvq buffers are sized for the widest band at lm=3: maxBandSize*8 bins.
	// cwrsScratch needs k+2 slots; cwrsMaxPulseCount+2 covers all normal cases.
	maxBandSize := bandEdges[maxBands] - bandEdges[maxBands-1]
	e.bandNorm = make([]float32, 0, 2*maxFrameSampleCount)
	e.bandLowScratch = make([]float32, 0, maxBandSize<<maxLM)
	e.bandCollapseMasks = make([]byte, 0, 2*maxBands)
	for ch := range 2 {
		e.pvqY[ch] = make([]int, 0, maxBandSize<<maxLM)
		e.pvqAbsX[ch] = make([]float32, 0, maxBandSize<<maxLM)
		e.pvqSign[ch] = make([]float32, 0, maxBandSize<<maxLM)
		e.normalisedBands[ch] = make([]float32, 0, maxFrameSampleCount)
	}
	e.cwrsScratch = make([]uint32, 0, cwrsMaxPulseCount+2)
}

func (e *Encoder) Mode() *Mode {
	if e.mode == nil {
		e.mode = DefaultMode()
	}

	return e.mode
}

// FinalRange returns the range coder state after the last EncodeFrame call.
// Compare with the decoder's FinalRange to verify encoder/decoder sync (RFC 6716 Section 5.1).
func (e *Encoder) FinalRange() uint32 {
	return e.rng
}

func (e *Encoder) encodeCoarseEnergy(info *frameSideInfo, targetLogE [2][maxBands]float32) {
	probModel := eProbModel[info.lm][boolIndex(info.intraEnergy)]
	previousBandPrediction := [2]float32{}
	coef := energyPredictionCoefficients[info.lm]
	beta := energyBetaCoefficients[info.lm]
	if info.intraEnergy {
		coef = 0
		beta = energyIntraBeta
	}
	info.coarseEnergy = e.previousLogE
	for band := info.startBand; band < info.endBand; band++ {
		for channel := range info.channelCount {
			oldEnergy := max(float32(-9), e.previousLogE[channel][band])
			predicted := coef*oldEnergy + previousBandPrediction[channel]
			q := quantizeCoarseEnergyDelta(targetLogE[channel][band] - predicted)
			qEncoded := e.encodeCoarseEnergyDelta(info, probModel[:], band, q)
			qf := float32(qEncoded)
			energy := predicted + qf
			e.previousLogE[channel][band] = energy
			info.coarseEnergy[channel][band] = energy
			previousBandPrediction[channel] += qf - beta*qf
		}
	}
	if info.channelCount == 1 {
		copy(e.previousLogE[1][:], e.previousLogE[0][:])
	}
}

// encodeSilenceFlag writes the RFC 6716 Table 56 silence flag.
func (e *Encoder) encodeSilenceFlag() {
	if e.rangeEncoder.Tell() == 1 {
		e.rangeEncoder.EncodeSymbolLogP(15, 0)
	}
}

// encodePostFilter writes the disabled RFC 6716 post-filter symbol.
func (e *Encoder) encodePostFilter(info *frameSideInfo) {
	if info.startBand == 0 && e.rangeEncoder.Tell()+16 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolLogP(1, 0)
	}
}

// encodeTransientFlag writes the RFC 6716 Section 4.3.1 transient flag.
func (e *Encoder) encodeTransientFlag(info *frameSideInfo) {
	if info.lm > 0 && e.rangeEncoder.Tell()+3 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolLogP(3, uint32(boolIndex(info.transient)))
	}
}

// encodeIntraEnergyFlag writes the RFC 6716 Section 4.3.2.1 intra flag (inter).
func (e *Encoder) encodeIntraEnergyFlag(info *frameSideInfo) {
	if e.rangeEncoder.Tell()+3 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolLogP(3, 0)
	}
}

// encodeTimeFrequencyChanges writes zero tf_change for all bands.
func (e *Encoder) encodeTimeFrequencyChanges(info *frameSideInfo) {
	logP := firstTimeFrequencyChangeLogP
	if info.transient {
		logP = firstTransientFrequencyChangeLogP
	}

	budget := info.totalBits
	tell := e.rangeEncoder.Tell()
	tfSelectReserved := info.lm > 0 && tell+uint(logP)+1 <= budget
	if tfSelectReserved {
		budget--
	}

	for band := info.startBand; band < info.endBand; band++ {
		if tell+uint(logP) <= budget {
			e.rangeEncoder.EncodeSymbolLogP(uint(logP), 0)
			tell = e.rangeEncoder.Tell()
		}

		if info.transient {
			logP = nextTransientFrequencyChangeLogP
		} else {
			logP = nextTimeFrequencyChangeLogP
		}
	}

	table := tfSelectTable[info.lm]
	if tfSelectReserved &&
		table[4*boolIndex(info.transient)] !=
			table[4*boolIndex(info.transient)+2] {
		e.rangeEncoder.EncodeSymbolLogP(1, 0)
	}
	// decodeTimeFrequencyChanges remaps the raw tf_change bits through Tables
	// 60-63 (RFC 6716 §4.3.1) before handing info to quantAllBands. I have to
	// do the same here; without it the encoder passes tfChange=0 while the
	// decoder sees tfChange=3 on transient frames, desynchronising the range coder.
	for band := info.startBand; band < info.endBand; band++ {
		info.tfChange[band] = int(table[4*boolIndex(info.transient)+info.tfChange[band]])
	}
}

// encodeSpread writes the default spread decision.
func (e *Encoder) encodeSpread(info *frameSideInfo) {
	if e.rangeEncoder.Tell()+4 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfSpread, uint32(info.spread))
	}
}

// encodeDynamicAllocation mirrors decodeDynamicAllocation by emitting a zero
// boost flag per band while budget allows. The decoder reads one flag per
// band per RFC 6716 Section 4.3.3, so the encoder must emit the matching
// flags in the same order to keep the range coder in sync — even when no
// boost is applied.
func (e *Encoder) encodeDynamicAllocation(info *frameSideInfo) uint {
	totalBitsEighth := info.totalBits << bitResolution
	dynamicAllocationLogP := initialDynamicAllocationLogP
	tellFrac := e.rangeEncoder.TellFrac()

	for band := info.startBand; band < info.endBand; band++ {
		if tellFrac+uint(dynamicAllocationLogP<<bitResolution) < totalBitsEighth {
			e.rangeEncoder.EncodeSymbolLogP(uint(dynamicAllocationLogP), 0)
			tellFrac = e.rangeEncoder.TellFrac()
		}
		info.bandBoost[band] = 0
	}

	return totalBitsEighth
}

// encodeAllocationTrim writes the default allocation trim.
func (e *Encoder) encodeAllocationTrim(info *frameSideInfo, totalBitsEighth uint) {
	if e.rangeEncoder.TellFrac()+uint(allocationTrimBitCost<<bitResolution) <= totalBitsEighth {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfAllocationTrim, uint32(info.allocationTrim))
	}
}

// EncodeFrame encodes one CELT frame from float PCM into dst.
// It returns the number of bytes written. dst must be at least frameBytes long.
//
//nolint:cyclop // The frame encoder mirrors RFC 6716 flow and is intentionally linear.
func (e *Encoder) EncodeFrame(pcm [][]float32, dst []byte, frameBytes, startBand, endBand int) (int, error) {
	if e.Mode() == nil {
		e.mode = DefaultMode()
	}
	if len(pcm) != 1 && len(pcm) != 2 {
		return 0, errInvalidChannelCount
	}
	frameSamples := shortBlockSampleCount << e.mode.MaxLM()
	for ch := range pcm {
		if len(pcm[ch]) != frameSamples {
			return 0, errInvalidFrameSize
		}
	}
	if startBand < 0 || startBand >= e.mode.BandCount() {
		return 0, errInvalidBand
	}
	if endBand <= startBand || endBand > e.mode.BandCount() {
		return 0, errInvalidBand
	}
	if len(dst) < frameBytes {
		return 0, errDstTooSmall
	}

	e.rangeEncoder.Init()

	transient := detectTransient(pcm, &e.analysis)
	analysis, err := analyzeFrame(
		e.mode, pcm, startBand, endBand, &e.analysis, &e.mdctScratch, &e.fftScratch,
		transient,
	)
	if err != nil {
		return 0, err
	}

	info := analysis.info
	info.totalBits = uint(frameBytes) * 8

	if e.rangeEncoder.Tell() > info.totalBits {
		return e.rangeEncoder.FlushInto(dst), nil
	}

	e.encodeSilenceFlag()
	e.encodePostFilter(&info)
	e.encodeTransientFlag(&info)
	e.encodeIntraEnergyFlag(&info)

	var targetLogE [2][maxBands]float32
	for ch := range info.channelCount {
		targetLogE[ch] = analysis.logBandAmp[ch]
	}
	e.encodeCoarseEnergy(&info, targetLogE)

	e.encodeTimeFrequencyChanges(&info)
	e.encodeSpread(&info)
	totalBitsEighth := e.encodeDynamicAllocation(&info)
	e.encodeAllocationTrim(&info, totalBitsEighth)

	tellFrac := int(e.rangeEncoder.TellFrac())
	bits := (int(info.totalBits) << bitResolution) - tellFrac - 1
	info.antiCollapseRsv = 0
	if info.transient && info.lm >= 2 && bits >= (info.lm+2)<<bitResolution {
		info.antiCollapseRsv = 1 << bitResolution
	}
	bits -= info.antiCollapseRsv
	targetIntensity := 0
	targetDualStereo := 0
	if info.channelCount == 2 {
		frameSampleCount := shortBlockSampleCount << info.lm
		bitrateBps := int(info.totalBits) * sampleRate / frameSampleCount
		frameMs := max(1, frameSampleCount*1000/sampleRate)
		targetIntensity = intensityStartBand(bitrateBps, frameMs)
		if chooseDualStereo(analysis.mdct[0], analysis.mdct[1], info.lm) {
			targetDualStereo = 1
		}
	}
	info.allocation = e.computeAllocationMono(&info, bits, targetIntensity, targetDualStereo)
	e.encodeFineEnergy(&info, info.allocation.fineQuant, targetLogE)

	totalBits := (int(info.totalBits) << bitResolution) - info.antiCollapseRsv
	bandState := bandEncodeState{
		rangeEncoder:   &e.rangeEncoder,
		seed:           e.rng,
		norm:           e.bandNorm[:0],
		lowbandScratch: e.bandLowScratch[:0],
		collapseMasks:  e.bandCollapseMasks[:0],
	}
	shape0 := normaliseBandsForEncoding(&info, analysis.mdct[0], analysis.logBandAmp[0], e.normalisedBands[0][:0])
	if info.channelCount == 2 {
		shape1 := normaliseBandsForEncoding(&info, analysis.mdct[1], analysis.logBandAmp[1], e.normalisedBands[1][:0])
		_ = quantAllBandsStereo(&info, shape0, shape1, totalBits, &bandState,
			e.pvqY, e.pvqAbsX, e.pvqSign, e.cwrsScratch)
	} else {
		_ = quantAllBandsMono(&info, shape0, totalBits, &bandState,
			e.pvqY[0], e.pvqAbsX[0], e.pvqSign[0], e.cwrsScratch)
	}

	if info.antiCollapseRsv > 0 {
		// RFC 6716 §4.3.5 puts one raw tail bit here right after the band
		// residuals; the decoder reads it before finalizeFineEnergy. I always
		// write 0 — the noise injection it controls is left for a later pass.
		e.rangeEncoder.EncodeRawBits(1, 0)
	}

	bitsLeft := int(info.totalBits) - int(e.rangeEncoder.Tell())
	e.finalizeFineEnergy(&info, info.allocation.fineQuant, info.allocation.finePriority, targetLogE, bitsLeft)

	e.rng = e.rangeEncoder.FinalRange()

	return e.rangeEncoder.FlushInto(dst), nil
}

func smallEnergySymbol(delta int) uint32 {
	switch {
	case delta < 0:
		return 1
	case delta > 0:
		return 2
	default:
		return 0
	}
}

func (e *Encoder) encodeCoarseEnergyDelta(info *frameSideInfo, probModel []uint8, band int, delta int) int {
	tell := e.rangeEncoder.Tell()
	if tell >= info.totalBits {
		return -1
	}

	bitsLeft := info.totalBits - tell
	switch {
	case bitsLeft >= 15:
		probIndex := 2 * min(band, maxBands-1)
		e.rangeEncoder.EncodeLaplace(
			uint32(probModel[probIndex])<<7,
			uint32(probModel[probIndex+1])<<6,
			delta,
		)

		return delta
	case bitsLeft >= 2:
		if delta < -1 {
			delta = -1
		} else if delta > 1 {
			delta = 1
		}
		e.rangeEncoder.EncodeSymbolWithICDF(icdfSmallEnergy, smallEnergySymbol(delta))

		return delta
	default:
		if delta < 0 {
			e.rangeEncoder.EncodeSymbolLogP(1, 1)

			return -1
		}
		e.rangeEncoder.EncodeSymbolLogP(1, 0)

		return 0
	}
}

func quantizeCoarseEnergyDelta(target float32) int {
	if target >= 0 {
		return int(target + 0.5)
	}

	return -int(-target + 0.5)
}

func clampFineEnergySymbol(value int, bits int) int {
	if value < 0 {
		return 0
	}
	maxValue := (1 << bits) - 1
	if value > maxValue {
		return maxValue
	}

	return value
}

func fineEnergyStep(bits int) float32 {
	return float32(uint(1)<<(14-bits)) / 16384
}

func (e *Encoder) encodeFineEnergy(info *frameSideInfo, fineQuant [maxBands]int, targetLogE [2][maxBands]float32) {
	for band := info.startBand; band < info.endBand; band++ {
		if fineQuant[band] <= 0 {
			continue
		}

		step := fineEnergyStep(fineQuant[band])
		for channel := range info.channelCount {
			residual := targetLogE[channel][band] - e.previousLogE[channel][band]
			q2 := clampFineEnergySymbol(int((residual+0.5)/step), fineQuant[band])

			e.rangeEncoder.EncodeRawBits(uint(fineQuant[band]), uint32(q2))

			offset := (float32(q2)+0.5)*step - 0.5
			e.previousLogE[channel][band] += offset
		}
	}

	if info.channelCount == 1 {
		copy(e.previousLogE[1][:], e.previousLogE[0][:])
	}
}

func (e *Encoder) computeAllocationMono(
	info *frameSideInfo, bits, targetIntensity, targetDualStereo int,
) allocationState {
	state := allocationState{bits: bits}
	caps := allocationCaps(info.lm, info.channelCount)
	balance := 0
	state.codedBands = computeAllocation(
		info.startBand,
		info.endBand,
		info.bandBoost[:],
		caps[:],
		info.allocationTrim,
		&state.intensity,
		&state.dualStereo,
		bits,
		&balance,
		state.pulses[:],
		state.fineQuant[:],
		state.finePriority[:],
		info.channelCount,
		info.lm,
		nil,
		&e.rangeEncoder,
		targetIntensity,
		targetDualStereo,
	)
	state.balance = balance

	return state
}

func (e *Encoder) finalizeFineEnergy(
	info *frameSideInfo,
	fineQuant [maxBands]int,
	finePriority [maxBands]int,
	targetLogE [2][maxBands]float32,
	bitsLeft int,
) {
	for priority := range 2 {
		for band := info.startBand; band < info.endBand && bitsLeft >= info.channelCount; band++ {
			if fineQuant[band] >= maxFineBits || finePriority[band] != priority {
				continue
			}
			step := float32(uint(1)<<(14-fineQuant[band]-1)) / 16384
			for channel := range info.channelCount {
				q2 := uint32(0)
				if targetLogE[channel][band]-e.previousLogE[channel][band] >= 0 {
					q2 = 1
				}
				e.rangeEncoder.EncodeRawBits(1, q2)
				offset := (float32(q2) - 0.5) * step
				e.previousLogE[channel][band] += offset
				bitsLeft--
			}
		}
	}
	if info.channelCount == 1 {
		copy(e.previousLogE[1][:], e.previousLogE[0][:])
	}
}
