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

	analysis analysisState
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
}

func (e *Encoder) Mode() *Mode {
	if e.mode == nil {
		e.mode = DefaultMode()
	}

	return e.mode
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

// encodeTransientFlag writes the RFC 6716 Section 4.3.1 transient flag (no transient).
func (e *Encoder) encodeTransientFlag(info *frameSideInfo) {
	if info.lm > 0 && e.rangeEncoder.Tell()+3 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolLogP(3, 0)
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
}

// encodeSpread writes the default spread decision.
func (e *Encoder) encodeSpread(info *frameSideInfo) {
	if e.rangeEncoder.Tell()+4 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfSpread, uint32(info.spread))
	}
}

// encodeDynamicAllocation writes zero boost for all bands.
func (e *Encoder) encodeDynamicAllocation(info *frameSideInfo) uint {
	totalBitsEighth := info.totalBits << bitResolution
	dynamicAllocationLogP := initialDynamicAllocationLogP
	tellFrac := e.rangeEncoder.TellFrac()

	for band := info.startBand; band < info.endBand; band++ {
		width := info.channelCount * (int(bandEdges[band+1]-bandEdges[band]) << info.lm)
		quanta := min(width<<bitResolution, max(allocationTrimBitCost<<bitResolution, width))
		quantaBits := uint(quanta)

		for tellFrac+uint(dynamicAllocationLogP<<bitResolution) < totalBitsEighth {
			if quantaBits >= totalBitsEighth {
				totalBitsEighth = 0
			} else {
				totalBitsEighth -= quantaBits
			}

			break
		}
	}

	return totalBitsEighth
}

// encodeAllocationTrim writes the default allocation trim.
func (e *Encoder) encodeAllocationTrim(info *frameSideInfo, totalBitsEighth uint) {
	if e.rangeEncoder.TellFrac()+uint(allocationTrimBitCost<<bitResolution) <= totalBitsEighth {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfAllocationTrim, uint32(info.allocationTrim))
	}
}

// EncodeFrame encodes one CELT frame from float PCM.
func (e *Encoder) EncodeFrame(
	pcm []float32,
	frameBytes int,
	startBand int,
	endBand int,
) ([]byte, error) {
	if e.Mode() == nil {
		e.mode = DefaultMode()
	}

	if len(pcm) != shortBlockSampleCount<<e.mode.MaxLM() {
		return nil, errInvalidFrameSize
	}
	if startBand < 0 || startBand >= e.mode.BandCount() {
		return nil, errInvalidBand
	}
	if endBand <= startBand || endBand > e.mode.BandCount() {
		return nil, errInvalidBand
	}
	_ = frameBytes

	e.rangeEncoder.Init()

	analysis, err := analyzeFrame(e.mode, pcm, startBand, endBand, &e.analysis)
	if err != nil {
		return nil, err
	}

	info := analysis.info
	info.totalBits = uint(frameBytes) * 8

	if e.rangeEncoder.Tell() > info.totalBits {
		return e.rangeEncoder.Done(), nil
	}

	e.encodeSilenceFlag()
	e.encodePostFilter(&info)
	e.encodeTransientFlag(&info)
	e.encodeIntraEnergyFlag(&info)

	var targetLogE [2][maxBands]float32
	targetLogE[0] = analysis.logBandAmp
	e.encodeCoarseEnergy(&info, targetLogE)

	e.encodeTimeFrequencyChanges(&info)
	e.encodeSpread(&info)
	totalBitsEighth := e.encodeDynamicAllocation(&info)
	e.encodeAllocationTrim(&info, totalBitsEighth)

	tellFrac := int(e.rangeEncoder.TellFrac())
	bits := (int(info.totalBits) << bitResolution) - tellFrac - 1
	info.antiCollapseRsv = 0
	bits -= info.antiCollapseRsv
	info.allocation = e.computeAllocationMono(&info, bits)
	e.encodeFineEnergy(&info, info.allocation.fineQuant, targetLogE)

	totalBits := (int(info.totalBits) << bitResolution) - info.antiCollapseRsv
	shape := normaliseBandsForEncoding(&info, analysis.mdct, analysis.logBandAmp)
	bandState := bandEncodeState{
		rangeEncoder: &e.rangeEncoder,
		seed:         e.rng,
	}
	_ = quantAllBandsMono(&info, shape, totalBits, &bandState)

	bitsLeft := int(info.totalBits) - int(e.rangeEncoder.Tell())
	e.finalizeFineEnergy(&info, info.allocation.fineQuant, info.allocation.finePriority, targetLogE, bitsLeft)

	e.rng = e.rangeEncoder.FinalRange()

	return e.rangeEncoder.Done(), nil
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

func (e *Encoder) computeAllocationMono(info *frameSideInfo, bits int) allocationState {
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
