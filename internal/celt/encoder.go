// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:gocognit,gosec // G115/G602: bounded by CELT frame/band sizes; EncodeFrame mirrors the reference flow.
package celt

import (
	"math"
	"math/bits"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/pion/opus/internal/slicetools"
)

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
	pvqYFloat         [2][]float32
	cwrsScratch       []uint32
	normalizedBands   [2][]float32
	pitchBuf          []float32
	pitchChannels     [2][]float32
	bandTmpScratch    []float32
	scratch           encoderScratch
	// rdoScratch and rdoState back the stereo-angle search. They live here so
	// the per-frame band state can borrow them instead of allocating.
	rdoScratch [4][]float32
	rdoState   [2]rangecoding.State

	spreadAverage      int
	hfAverage          int
	tapsetDecision     int
	prevSpreadDecision int
	prevIntensityBand  int
	// specAvg tracks the running spectral level the temporal-VBR term compares
	// each frame against (st->spec_avg in celt_encoder.c).
	specAvg float32
	// stereoSaving estimates how many bits mid/side is saving over plain
	// stereo; the VBR target spends less when the channels are redundant.
	stereoSaving float32
	// lastCodedBands feeds the band-skip hysteresis in computeAllocation
	// (st->lastCodedBands in libopus celt_encoder.c). Zero means "no previous
	// frame", which the update below seeds directly instead of clamping.
	lastCodedBands int
	prevLogBandAmp [2][maxBands]float32
	// consecTransient counts consecutive transient frames (st->consec_transient
	// in libopus celt_encoder.c) — anti-collapse only fires for the first two
	// in a row.
	consecTransient int

	// Application mode plumbing — set by root encoder via setter methods.
	vbr            bool
	constrainedVBR bool
	lossRate       int
	complexity     int
	// bitrate is the nominal target the caller asked for. The reference caps
	// equiv_rate with it, which is what keeps a VBR frame's rate-derived
	// decisions from tracking the output buffer instead of the target.
	bitrate int

	// VBR bit reservoir state (celt_encoder.c), all in 1/8-bit units.
	vbrReservoir int32
	vbrDrift     int32
	vbrOffset    int32
	vbrCount     int32
}

func (e *Encoder) SetComplexity(c int) {
	e.complexity = c
}

func (e *Encoder) Complexity() int {
	return e.complexity
}

func NewEncoder() Encoder {
	encoder := Encoder{mode: DefaultMode(), complexity: 5}
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
	// normalizedBands/bandNorm need maxFrameSampleCount per channel — the full
	// MDCT spectrum (960 bins), not just the coded band range (800 bins).
	// pvq buffers are sized for the widest band at lm=3: maxBandSize*8 bins.
	// cwrsScratch needs k+2 slots; cwrsMaxPulseCount+2 covers all normal cases.
	maxBandSize := bandEdges[maxBands] - bandEdges[maxBands-1]
	e.bandNorm = make([]float32, 0, 2*maxFrameSampleCount)
	e.bandLowScratch = make([]float32, 0, maxBandSize<<maxLM)
	e.bandCollapseMasks = make([]byte, 0, 2*maxBands)
	for ch := range 2 {
		e.pvqY[ch] = make([]int, 0, maxBandSize<<maxLM)
		e.pvqYFloat[ch] = make([]float32, 0, maxBandSize<<maxLM)
		e.pvqAbsX[ch] = make([]float32, 0, maxBandSize<<maxLM)
		e.pvqSign[ch] = make([]float32, 0, maxBandSize<<maxLM)
		e.normalizedBands[ch] = make([]float32, 0, maxFrameSampleCount)
	}
	e.cwrsScratch = make([]uint32, 0, cwrsMaxPulseCount+2)
	e.bandTmpScratch = make([]float32, 0, maxFrameSampleCount)
	e.pitchBuf = make([]float32, 0, (combFilterMaxPeriod+maxFrameSampleCount)>>1)

	// libopus seeds tonal_average at 256 (celt_encoder.c:3091), the midpoint
	// of the spreading metric — starting at zero biases the first frames
	// toward aggressive spreading.
	e.spreadAverage = 256
	e.hfAverage = 0
	e.tapsetDecision = 0
	e.prevSpreadDecision = defaultSpreadDecision
	e.prevIntensityBand = 0
	e.stereoSaving = 0
	e.specAvg = 0
	e.lastCodedBands = 0
	e.consecTransient = 0
	e.analysis.prefilter = postFilterState{}

	e.vbrReservoir = 0
	e.vbrDrift = 0
	e.vbrOffset = 0
	e.vbrCount = 0
}

func (e *Encoder) SetVBR(vbr bool) {
	e.vbr = vbr
}

func (e *Encoder) SetConstrainedVBR(cvbr bool) {
	e.constrainedVBR = cvbr
}

// SetBitrate sets the nominal target bitrate in bits per second.
func (e *Encoder) SetBitrate(bps int) {
	e.bitrate = bps
}

// SetLossRate sets the expected packet loss rate (0-100 percent).
func (e *Encoder) SetLossRate(rate int) {
	e.lossRate = rate
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

// encodePostFilter writes the RFC 6716 Table 56 post-filter symbols.
func (e *Encoder) encodePostFilter(info *frameSideInfo) {
	if info.startBand != 0 || e.rangeEncoder.Tell()+16 > info.totalBits {
		return
	}
	if !info.postFilter.enabled {
		e.rangeEncoder.EncodeSymbolLogP(1, 0)

		return
	}

	e.rangeEncoder.EncodeSymbolLogP(1, 1)

	// Encode period as octave + fine pitch (RFC 6716 §4.3.7.1).
	// pitch_index = period + 1 is split into octave = floor(log2) - 4
	// and fine = pitch_index - (16 << octave), matching libopus
	// celt_encoder.c lines 2055-2058.
	period1 := uint32(info.postFilter.period + 1)
	octave := uint(bits.Len32(period1) - 5)
	e.rangeEncoder.EncodeUniform(6, uint32(octave))
	fine := period1 - (16 << octave)
	e.rangeEncoder.EncodeRawBits(4+octave, fine)

	e.rangeEncoder.EncodeRawBits(3, uint32(info.postFilter.qq))

	if e.rangeEncoder.Tell()+2 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfTapset, uint32(info.postFilter.tapset))
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

	// tf_change is delta-coded against the previous band, so only transitions
	// cost bits (libopus tf_encode).
	curr := 0
	tfChanged := 0
	for band := info.startBand; band < info.endBand; band++ {
		if tell+uint(logP) <= budget {
			e.rangeEncoder.EncodeSymbolLogP(uint(logP), uint32(info.tfChange[band]^curr))
			tell = e.rangeEncoder.Tell()
			curr = info.tfChange[band]
			tfChanged |= curr
		} else {
			info.tfChange[band] = curr
		}

		if info.transient {
			logP = nextTransientFrequencyChangeLogP
		} else {
			logP = nextTimeFrequencyChangeLogP
		}
	}

	table := tfSelectTable[info.lm]
	base := 4 * boolIndex(info.transient)
	// Only worth signaling tf_select when the two tables differ for the
	// transitions this frame actually used.
	if tfSelectReserved && table[base+tfChanged] != table[base+2+tfChanged] {
		e.rangeEncoder.EncodeSymbolLogP(1, uint32(info.tfSelect))
	} else {
		info.tfSelect = 0
	}

	// decodeTimeFrequencyChanges remaps the raw tf_change bits through Tables
	// 60-63 (RFC 6716 §4.3.1) before handing info to quantAllBands. I have to
	// do the same here; without it the encoder passes tfChange=0 while the
	// decoder sees tfChange=3 on transient frames, desynchronising the range coder.
	for band := info.startBand; band < info.endBand; band++ {
		info.tfChange[band] = int(table[base+2*info.tfSelect+info.tfChange[band]])
	}
}

// encodeSpread writes the default spread decision.
func (e *Encoder) encodeSpread(info *frameSideInfo) {
	if e.rangeEncoder.Tell()+4 <= info.totalBits {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfSpread, uint32(info.spread))
	}
}

// encodeDynamicAllocation mirrors decodeDynamicAllocation by emitting boost
// flags per band. offsets[band] is the number of boost quanta computed by
// dynallocAnalysis; the encoder writes one flag per quantum, stopping when
// the flag is 0 or the budget is exhausted. The decoder reads the same
// sequence (RFC 6716 Section 4.3.3), so the two must stay in lockstep.
func (e *Encoder) encodeDynamicAllocation(info *frameSideInfo, offsets [maxBands]int) uint {
	totalBitsEighth := info.totalBits << bitResolution
	caps := allocationCaps(info.lm, info.channelCount)
	dynamicAllocationLogP := initialDynamicAllocationLogP
	tellFrac := e.rangeEncoder.TellFrac()

	for band := info.startBand; band < info.endBand; band++ {
		width := info.channelCount * (int(bandEdges[band+1]-bandEdges[band]) << info.lm)
		quanta := min(width<<bitResolution, max(allocationTrimBitCost<<bitResolution, width))
		quantaBits := uint(quanta)
		loopLogP := dynamicAllocationLogP
		boost := 0

		for j := 0; tellFrac+uint(loopLogP<<bitResolution) < totalBitsEighth && boost < caps[band]; j++ {
			flag := j < offsets[band]
			e.rangeEncoder.EncodeSymbolLogP(uint(loopLogP), uint32(boolIndex(flag)))
			tellFrac = e.rangeEncoder.TellFrac()
			if !flag {
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

// encodeAllocationTrim chooses and writes the allocation trim, falling back to
// defaultAllocationTrim (matching the decoder's fallback) when there isn't
// enough budget left to signal it.
func (e *Encoder) encodeAllocationTrim(
	info *frameSideInfo, logBandAmp [2][maxBands]float32, mdct [2][]float32, totalBitsEighth uint,
	tfEstimate float32, intensity, equivRate int,
) {
	info.allocationTrim = defaultAllocationTrim
	if e.rangeEncoder.TellFrac()+uint(allocationTrimBitCost<<bitResolution) <= totalBitsEighth {
		info.allocationTrim = chooseAllocationTrim(
			logBandAmp,
			mdct,
			info.channelCount, info.lm, info.endBand,
			tfEstimate, intensity, &e.stereoSaving, equivRate,
		)
		e.rangeEncoder.EncodeSymbolWithICDF(icdfAllocationTrim, uint32(info.allocationTrim))
	}
}

// chooseSpread runs the spreading_decision estimator, or falls back to a flat
// level. libopus only runs the estimator at complexity>=3 with long blocks and
// enough of a byte budget (celt_encoder.c ~line 2317); below that it uses
// SPREAD_NORMAL, or SPREAD_NONE at complexity 0 specifically.
func (e *Encoder) chooseSpread(
	info *frameSideInfo, normalized [2][]float32, spreadWeight [maxBands]int,
	effectiveBytes int, prefilterEnabled bool,
) int {
	if info.transient || e.complexity < 3 || effectiveBytes < 10*info.channelCount {
		if e.complexity == 0 {
			return spreadNone
		}

		return spreadNormal
	}

	return spreadingDecision(
		normalized, info.lm, info.startBand, info.endBand, info.channelCount,
		&e.spreadAverage, &e.hfAverage, &e.tapsetDecision,
		e.prevSpreadDecision, prefilterEnabled && !info.transient,
		spreadWeight,
	)
}

// normaliseChannels divides every band by its amplitude so each one carries
// unit norm, which is the domain the analysis stages downstream expect.
func (e *Encoder) normaliseChannels(info *frameSideInfo, analysis *analysisResult) [2][]float32 {
	var out [2][]float32
	for ch := range info.channelCount {
		out[ch] = normaliseBandsForEncoding(
			info, analysis.mdct[ch], analysis.logBandAmp[ch], e.normalizedBands[ch][:0])
	}

	return out
}

// choosePrefilter runs the pitch search and decides whether the pre-filter is
// worth enabling. srcs hold the pre-emphasized signal with combFilterMaxPeriod
// samples of history in front, which is the layout libopus' run_prefilter uses.
func (e *Encoder) choosePrefilter(
	srcs [][]float32, frameSampleCount, frameBytes int, tfEstimate float32,
) (bool, int, int, float32, int) {
	// The history pion keeps is two samples longer than the reference's, so
	// line the window up with libopus' `pre`.
	const pad = postfilterHistorySampleCount - combFilterMaxPeriod
	pitchInput := e.pitchChannels[:len(srcs)]
	for ch, src := range srcs {
		pitchInput[ch] = src[pad:]
	}

	pitchLen := (combFilterMaxPeriod + frameSampleCount) >> 1
	buf := slicetools.Resize(&e.pitchBuf, pitchLen)
	pitchDownsample(pitchInput, buf, pitchLen, 2, &e.scratch)

	// The top 1.5 octave of the range is skipped: short-term correlation there
	// produces too many false positives.
	pitchPeriod := pitchSearch(
		buf[combFilterMaxPeriod>>1:], buf,
		frameSampleCount, combFilterMaxPeriod-3*combFilterMinPeriod, &e.scratch,
	)
	pitchPeriod = combFilterMaxPeriod - pitchPeriod

	pitchGain := removeDoubling(
		buf, combFilterMaxPeriod, combFilterMinPeriod, frameSampleCount,
		&pitchPeriod, e.analysis.prefilter.period, e.analysis.prefilter.gain, &e.scratch,
	)
	pitchPeriod = min(pitchPeriod, combFilterMaxPeriod-2)

	// The reference trims the raw correlation before thresholding, and backs
	// the filter off further when packet loss is expected: the post-filter is
	// recursive, so a lost frame keeps ringing (celt_encoder.c:1498).
	pitchGain *= 0.7
	if e.lossRate > 2 {
		pitchGain *= 0.5
	}
	if e.lossRate > 4 {
		pitchGain *= 0.5
	}
	if e.lossRate > 8 {
		pitchGain = 0
	}

	enabled, qq, quantizedGain := prefilterDecision(
		pitchPeriod, pitchGain,
		e.analysis.prefilter.period, e.analysis.prefilter.gain,
		frameBytes, len(srcs), tfEstimate,
		uint(frameBytes)*8, e.rangeEncoder.Tell(),
	)
	if enabled && shouldCancelPrefilter(
		srcs, &e.analysis, frameSampleCount, pitchPeriod, quantizedGain, e.tapsetDecision,
	) {
		enabled, qq, quantizedGain = false, 0, 0
	}

	return enabled, pitchPeriod, qq, quantizedGain, e.tapsetDecision
}

// updatePrefilterState saves or resets the prefilter state for the next frame.
func (e *Encoder) updatePrefilterState(
	info *frameSideInfo, enabled bool,
	period int, gain float32, qq int, tapset int,
) {
	// libopus stores the searched period and tapset whether or not the filter
	// ran (celt_encoder.c:2768); only the gain goes to zero. Resetting the
	// period instead traps the filter off: the next frame's continuity check
	// compares against it, so a reset always looks like a pitch jump and adds
	// 0.2 to the enable threshold, which a mid-strength pitch never clears.
	e.analysis.prefilter.period = period
	e.analysis.prefilter.tapset = tapset
	e.analysis.prefilter.gain = 0
	if enabled {
		e.analysis.prefilter.gain = gain

		info.postFilter = postFilter{
			enabled: true,
			period:  period,
			gain:    gain,
			qq:      qq,
			tapset:  tapset,
		}
	}
}

// computeIntensityAndDualStereo returns the intensity band and dual stereo flag
// for the current frame. Intensity band includes ±1 hysteresis to avoid oscillation.
func (e *Encoder) computeIntensityAndDualStereo(
	info *frameSideInfo, normalized [2][]float32, equivRate int,
) (targetIntensity, targetDualStereo int) {
	if info.channelCount != 2 {
		return 0, 0
	}

	raw := intensityBandForRate(equivRate/1000, e.prevIntensityBand)
	e.prevIntensityBand = raw

	targetIntensity = min(info.endBand, max(info.startBand, raw))
	if chooseDualStereo(normalized, info.lm) {
		targetDualStereo = 1
	}

	return targetIntensity, targetDualStereo
}

// computeVBR returns the VBR target in 1/8-bit units for the current frame.
//
// Simplified version of libopus compute_vbr() (celt_encoder.c, ~line 1605).
// Not ported: tonality/activity boost, stereo saving, surround masking,
// temporal VBR — these need the full analysis pipeline pion doesn't have yet.
// vbrTFCalibration is the average tf_estimate the target is calibrated for,
// so a typical frame gets no boost (celt_encoder.c compute_vbr).
const vbrTFCalibration = 0.044

func computeVBR(
	baseTarget int, // 1/8-bit units
	maxDepth float32,
	totBoostBits int,
	constrainedVBR bool,
	channelCount int,
	lm int,
	tfEstimate float32,
	intensity, lastCodedBands int, stereoSaving float32,
	equivRate int, temporalVBR float32,
) int {
	target := baseTarget

	// Stereo savings: bands coded in intensity stereo carry one channel's
	// worth of shape, so the frame needs fewer bits the more redundant the
	// two channels are. Capped by the share of the spectrum that is actually
	// stereo-coded (celt_encoder.c compute_vbr).
	if channelCount == 2 {
		codedBands := lastCodedBands
		if codedBands == 0 {
			codedBands = maxBands
		}
		codedBins := int(bandEdges[codedBands]) << lm
		codedStereoBands := min(intensity, codedBands)
		codedBins += int(bandEdges[codedStereoBands]) << lm
		codedStereoDOF := (int(bandEdges[codedStereoBands]) << lm) - codedStereoBands
		if codedBins > 0 {
			maxFrac := 0.8 * float32(codedStereoDOF) / float32(codedBins)
			saving := min32(stereoSaving, 1.0)
			target -= min(
				int(maxFrac*float32(target)),
				int((saving-0.1)*float32(codedStereoDOF<<bitResolution)),
			)
		}
	}

	target += totBoostBits - (19 << lm) // dynalloc calibration

	// Transient boost, compensated for the average frame. The reference scales
	// the target by tf_estimate rather than switching on the transient flag.
	target += int((tfEstimate - vbrTFCalibration) * float32(target))

	// The floor is the depth the spectrum can actually use: the coded bin count
	// times the per-bin depth. The Q shift around it in celt_encoder.c is a
	// no-op in the float build, so maxDepth goes in unscaled.
	bins := int(bandEdges[maxBands-2]) << lm
	floorDepth := int(float32(channelCount*bins<<bitResolution) * maxDepth)
	floorDepth = max(floorDepth, target>>2)
	target = min(target, floorDepth)

	// Constrained VBR can't sustain a higher bitrate for long, so pull 1/3
	// of the way back to baseTarget (libopus's fixed 0.67 factor).
	if constrainedVBR {
		target = baseTarget + int(0.67*float32(target-baseTarget))
	}

	// Temporal VBR only applies to frames that are not asking for finer time
	// resolution, and it fades out as the rate approaches 96 kb/s, where there
	// are enough bits that leveling them out stops paying.
	if tfEstimate < 0.2 {
		amount := 0.0000031 * float32(max(0, min(32000, 96000-equivRate)))
		target += int(temporalVBR * amount * float32(target))
	}

	return max(min(target, 2*baseTarget), 0)
}

// applyVBR computes the VBR-adjusted effectiveBytes for the current frame
// and updates the bit reservoir/drift state that biases future frames.
// tellFrac is e.rangeEncoder.TellFrac() at the point of the call. Mirrors
// celt_encoder.c's VBR block around compute_vbr (~lines 2436-2530).
// equivalentRate expresses the frame's byte budget as a steady bit rate, net of
// the per-packet overhead the reference charges, and never above the nominal
// target (celt_encoder.c:1926). In VBR the budget is the whole output buffer,
// so without the cap every rate-derived decision would read as if the encoder
// were running at hundreds of kb/s.
func (e *Encoder) equivalentRate(maxBytes, lm, channelCount int) int {
	overhead := (40*channelCount + 20) * ((400 >> lm) - 50)
	equiv := maxBytes*8*50<<(3-lm) - overhead
	if e.bitrate > 0 {
		equiv = min(equiv, e.bitrate-overhead)
	}

	return equiv
}

// settleFrameBudget fixes how many bytes the frame gets — running the VBR
// target when it applies — and returns what is left for the band shapes after
// the header and the anti-collapse reservation.
func (e *Encoder) settleFrameBudget(
	info *frameSideInfo, logBandAmp [2][maxBands]float32, dr dynallocResult,
	effectiveBytes, rateBytes, maxBytes int, tfEstimate float32, intensity int,
) (int, int) {
	tellFrac := int(e.rangeEncoder.TellFrac())
	if e.vbr {
		effectiveBytes = e.applyVBR(info, logBandAmp, dr, rateBytes, maxBytes, tellFrac, tfEstimate, intensity)
	}
	info.totalBits = uint(effectiveBytes) * 8

	shapeBits := (int(info.totalBits) << bitResolution) - tellFrac - 1
	info.antiCollapseRsv = 0
	if info.transient && info.lm >= 2 && shapeBits >= (info.lm+2)<<bitResolution {
		info.antiCollapseRsv = 1 << bitResolution
	}

	return effectiveBytes, shapeBits - info.antiCollapseRsv
}

// frameBudget returns the byte budget the rate-derived decisions are measured
// against, and the ceiling the frame may actually occupy.
func (e *Encoder) frameBudget(
	dst []byte, frameBytes, frameSamples, lm, channelCount int,
) (rateBytes, maxBytes, equivRate int) {
	rateBytes = e.rateBytes(frameBytes, frameSamples)
	maxBytes = e.frameCeiling(dst, frameBytes)

	return rateBytes, maxBytes, e.equivalentRate(maxBytes, lm, channelCount)
}

// rateBytes is the byte budget the VBR target is measured against. The
// reference derives it straight from the nominal bitrate (celt_encoder.c:1904),
// so it covers the whole frame's share including the packet header byte that
// the CBR payload budget leaves out; the reservoir absorbs the difference.
func (e *Encoder) rateBytes(frameBytes, frameSamples int) int {
	if !e.vbr || e.bitrate <= 0 {
		return frameBytes
	}

	return e.bitrate * frameSamples / (sampleRate * 8)
}

// frameCeiling is how many bytes the frame may occupy. libopus caps a VBR frame
// at the room the caller left (nbCompressedBytes), which is what lets it run
// above the nominal rate on a hard frame; CBR stays pinned to its share.
// temporalVBR measures how far the frame's spectral level sits above or below
// the running average, following a decaying envelope so a brief dip does not
// register. The VBR target leans on it to spend more on a frame that is
// louder than its neighbors (celt_encoder.c).
func (e *Encoder) temporalVBR(
	logBandAmp [2][maxBands]float32, startBand, endBand, channelCount, lm int, transient bool,
) float32 {
	follow := float32(-10.0)
	var frameAvg float32
	var offset float32
	if transient {
		offset = 0.5 * float32(lm)
	}
	for band := startBand; band < endBand; band++ {
		follow = max32(follow-1.0, logBandAmp[0][band]-offset)
		if channelCount == 2 {
			follow = max32(follow, logBandAmp[1][band]-offset)
		}
		frameAvg += follow
	}
	frameAvg /= float32(endBand - startBand)

	tvbr := min32(3.0, max32(-1.5, frameAvg-e.specAvg))
	e.specAvg += 0.02 * tvbr

	return tvbr
}

func (e *Encoder) frameCeiling(dst []byte, frameBytes int) int {
	if !e.vbr {
		return frameBytes
	}

	return min(len(dst), maxCELTFrameBytes)
}

func (e *Encoder) applyVBR(
	info *frameSideInfo, logBandAmp [2][maxBands]float32, dr dynallocResult,
	frameBytes, maxBytes, tellFrac int, tfEstimate float32, intensity int,
) int {
	lm, channelCount := info.lm, info.channelCount
	temporalVBR := e.temporalVBR(logBandAmp, info.startBand, info.endBand, channelCount, lm, info.transient)
	vbrRate := frameBytes << 6 // libopus vbr_rate, 1/8-bit units
	equivRate := e.equivalentRate(maxBytes, lm, channelCount)

	if e.constrainedVBR {
		// libopus allows any multiple of vbrRate as the bound; pion always
		// uses 2x (vbr_bound == vbr_rate in celt_encoder.c).
		maxAllowed := max(2, (2*vbrRate-int(e.vbrReservoir))>>6)
		maxBytes = min(maxBytes, maxAllowed)
	}

	baseTarget := max(0, vbrRate-((40*channelCount+20)<<3))
	if e.constrainedVBR {
		baseTarget += int(e.vbrOffset)
	}

	// rawTarget folds in tellFrac (bits already spent) before rounding to
	// bytes. libopus uses this pre-rounding value for the drift update below
	// and the rounded value for the reservoir — they're not the same number.
	rawTarget := computeVBR(
		baseTarget, dr.maxDepth, dr.totBoostBits, e.constrainedVBR, channelCount, lm, tfEstimate,
		intensity, e.lastCodedBands, e.stereoSaving,
		equivRate, temporalVBR,
	) + tellFrac

	// The frame still has to fit what has already been written plus the
	// dynalloc boosts, or the range coder runs out of room (libopus
	// min_allowed).
	minAllowed := ((tellFrac + dr.totBoostBits + (1 << 6) - 1) >> 6) + 2

	// The ceiling is how much room the caller left, not the nominal rate:
	// unconstrained VBR is allowed to spend over the average on a hard frame
	// and win it back later through the reservoir.
	nbAvailableBytes := max(minAllowed, (rawTarget+(1<<5))>>6)
	nbAvailableBytes = min(nbAvailableBytes, maxBytes)

	// A reservoir that has gone negative means the last frames spent under the
	// rate; the reference hands those bits back here instead of dropping them,
	// which is what keeps constrained VBR on target over time.
	nbAvailableBytes += e.updateVBRReservoir(vbrRate, rawTarget, nbAvailableBytes<<6)

	return min(nbAvailableBytes, maxBytes)
}

// updateVBRReservoir tracks the VBR bit surplus/deficit for this frame and
// updates the drift correction that biases baseTarget on future frames.
// All three args are in 1/8-bit units (see applyVBR), matching libopus's
// vbr_reservoir/vbr_drift/vbr_offset in celt_encoder.c.
func (e *Encoder) updateVBRReservoir(vbrRate, rawTarget, roundedTarget int) int {
	if !e.vbr {
		return 0
	}

	var alpha float32
	if e.vbrCount < 970 {
		e.vbrCount++
		alpha = 1.0 / float32(e.vbrCount+20)
	} else {
		alpha = 0.001
	}

	if !e.constrainedVBR {
		return 0
	}

	e.vbrReservoir += int32(roundedTarget - vbrRate)

	driftDelta := float32(rawTarget-vbrRate) - float32(e.vbrOffset) - float32(e.vbrDrift)
	e.vbrDrift += int32(alpha * driftDelta)
	e.vbrOffset = -e.vbrDrift

	adjust := 0
	if e.vbrReservoir < 0 {
		adjust = int(-e.vbrReservoir) >> 6
		e.vbrReservoir = 0
	}

	return adjust
}

// newBandState wires up the per-frame state the band quantiser works from,
// including the linear band energies libopus hands quant_all_bands.
func (e *Encoder) newBandState(
	info *frameSideInfo, logBandAmp [2][maxBands]float32,
) bandEncodeState {
	state := bandEncodeState{
		rangeEncoder:   &e.rangeEncoder,
		seed:           e.rng,
		norm:           e.bandNorm[:0],
		lowbandScratch: e.bandLowScratch[:0],
		collapseMasks:  e.bandCollapseMasks[:0],
		tmpScratch:     e.bandTmpScratch[:0],
		// libopus only searches the stereo angle at complexity 8 and up: it
		// encodes every stereo band twice, so the cost is real.
		thetaRDO:   e.complexity >= thetaRDOComplexity && info.channelCount == 2,
		rdoScratch: &e.rdoScratch,
		rdoState:   &e.rdoState,
	}
	for ch := range info.channelCount {
		for band := info.startBand; band < info.endBand; band++ {
			state.bandEnergy[ch][band] = float32(math.Exp2(
				float64(logBandAmp[ch][band]+energyMeans[band])))
		}
	}

	return state
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

	transient, tfEstimate, tfChan := detectTransient(pcm, &e.analysis)

	// libopus runs the pitch search on the pre-emphasized signal with its
	// comb-filter history in front (celt_encoder.c:run_prefilter), so the
	// pre-emphasis happens before the prefilter decision, not inside the MDCT
	// analysis.
	var srcs [2][]float32
	for ch := range pcm {
		srcs[ch] = preemphasisChannel(e.mode, pcm[ch], ch, &e.analysis)
	}
	prefilterEnabled, pitchPeriod, prefilterQq, prefilterGain, prefilterTapset := e.choosePrefilter(
		srcs[:len(pcm)], len(pcm[0]), frameBytes, tfEstimate,
	)

	// libopus gates the last-chance transient check on complexity>=5
	// (celt_encoder.c:2216).
	analysis, patched, err := analyzeFrame(
		e.mode, pcm, srcs, startBand, endBand, &e.analysis, &e.mdctScratch, &e.fftScratch,
		transient,
		prefilterEnabled, pitchPeriod, prefilterGain, prefilterTapset,
		e.previousLogE, e.complexity >= 5,
	)
	if err != nil {
		return 0, err
	}
	if patched {
		// analyzeFrame already flipped info.transient; what is left is the
		// fixed estimate the reference hands tf_analysis and the VBR target
		// for a frame the time-domain metric missed.
		tfEstimate = 0.2
	}

	info := analysis.info
	info.totalBits = uint(frameBytes) * 8

	e.updatePrefilterState(&info, prefilterEnabled, pitchPeriod, prefilterGain, prefilterQq, prefilterTapset)

	if e.rangeEncoder.Tell() > info.totalBits {
		return e.rangeEncoder.FlushIntoPadded(dst, frameBytes), nil
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

	// Compute dynalloc offsets, spread_weight and importance BEFORE the spread
	// and tf decisions: spread_weight feeds spreadingDecision and importance
	// feeds tfAnalysis (libopus runs dynalloc_analysis first).
	rateBytes, maxBytes, equivRate := e.frameBudget(dst, frameBytes, frameSamples, info.lm, info.channelCount)
	effectiveBytes := rateBytes
	dr := dynallocAnalysis(
		analysis.logBandAmp, e.prevLogBandAmp,
		info.lm, info.startBand, info.endBand, info.channelCount,
		effectiveBytes, info.transient, e.vbr, e.constrainedVBR,
	)
	offsets := dr.offsets
	spreadWeight := dr.spreadWeight

	// tf_analysis and spreading_decision both read the normalized bands, so
	// build them once here (libopus normalise_bands, celt_encoder.c:2241).
	// libopus disables variable tf resolution for very small frames; its
	// fallback is tf_res = isTransient for every band, not zero.
	normalized := e.normaliseChannels(&info, &analysis)

	if effectiveBytes >= 15*info.channelCount && e.complexity >= 2 {
		lambda := max(80, 20480/effectiveBytes+2)
		// libopus measures tf resolution on the channel that drove the
		// transient decision, not always channel 0. Handing tfAnalysis that
		// channel directly keeps its own tf_chan index at 0.
		info.tfSelect = tfAnalysis(
			normalized[tfChan], info.lm, info.startBand, info.endBand, info.transient,
			lambda, tfEstimate, 0, len(analysis.mdct[tfChan]), &dr.importance, &info.tfChange,
			&e.scratch,
		)
	} else {
		for band := info.startBand; band < info.endBand; band++ {
			info.tfChange[band] = boolIndex(info.transient)
		}
		info.tfSelect = 0
	}
	e.encodeTimeFrequencyChanges(&info)

	info.spread = e.chooseSpread(&info, normalized, spreadWeight, effectiveBytes, prefilterEnabled)
	e.prevSpreadDecision = info.spread
	e.encodeSpread(&info)
	totalBitsEighth := e.encodeDynamicAllocation(&info, offsets)
	// The reference settles the intensity band before both the trim and the VBR
	// target, and both read it (celt_encoder.c:2404). It also derives it from
	// the nominal rate, not from the post-VBR frame size.
	targetIntensity, targetDualStereo := e.computeIntensityAndDualStereo(&info, normalized, equivRate)
	e.encodeAllocationTrim(
		&info, analysis.logBandAmp, analysis.mdct, totalBitsEighth, tfEstimate, targetIntensity, equivRate)

	effectiveBytes, shapeBits := e.settleFrameBudget(
		&info, analysis.logBandAmp, dr, effectiveBytes, rateBytes, maxBytes, tfEstimate, targetIntensity)
	info.allocation = e.computeAllocationMono(&info, shapeBits, targetIntensity, targetDualStereo)
	e.encodeFineEnergy(&info, info.allocation.fineQuant, targetLogE)

	totalBits := (int(info.totalBits) << bitResolution) - info.antiCollapseRsv
	bandState := e.newBandState(&info, analysis.logBandAmp)
	shape0 := normalized[0]
	if info.channelCount == 2 {
		shape1 := normalized[1]
		_ = quantAllBandsStereo(
			&info, shape0, shape1, totalBits, &bandState,
			e.pvqY, e.pvqYFloat, e.pvqAbsX, e.pvqSign, e.cwrsScratch,
		)
	} else {
		_ = quantAllBandsMono(
			&info, shape0, totalBits, &bandState,
			e.pvqY[0], e.pvqYFloat[0], e.pvqAbsX[0], e.pvqSign[0], e.cwrsScratch,
		)
	}

	if info.antiCollapseRsv > 0 {
		// RFC 6716 §4.3.5 puts one raw tail bit here right after the band
		// residuals; the decoder reads it before finalizeFineEnergy. libopus
		// only keeps anti-collapse on for the first two transient frames in a
		// row (celt_encoder.c consec_transient), not every transient frame.
		antiCollapseOn := e.consecTransient < 2
		e.rangeEncoder.EncodeRawBits(1, uint32(boolIndex(antiCollapseOn)))
	}
	if info.transient {
		e.consecTransient++
	} else {
		e.consecTransient = 0
	}

	bitsLeft := int(info.totalBits) - int(e.rangeEncoder.Tell())
	e.finalizeFineEnergy(&info, info.allocation.fineQuant, info.allocation.finePriority, targetLogE, bitsLeft)

	e.prevLogBandAmp = analysis.logBandAmp

	e.rng = e.rangeEncoder.FinalRange()

	// Pad to the size the allocation was derived from (info.totalBits), so the
	// decoder re-derives the same allocation from the packet it receives. Under
	// VBR that size is the per-frame target rather than frameBytes, which is
	// what keeps packet sizes varying.
	return e.rangeEncoder.FlushIntoPadded(dst, effectiveBytes), nil
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

		// The Laplace coder clamps values its tail cannot represent, so the
		// energy state has to follow what the decoder will actually see.
		return e.rangeEncoder.EncodeLaplace(
			uint32(probModel[probIndex])<<7,
			uint32(probModel[probIndex+1])<<6,
			delta,
		)
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
		e.lastCodedBands,
		// Without the tonality analysis pipeline libopus falls back to end-1,
		// which makes the band<=signalBandwidth half of the skip test always
		// true and leaves the decision to the depth threshold alone.
		info.endBand-1,
	)
	state.balance = balance
	// Track the band count for the next frame's hysteresis, moving one step at
	// a time once seeded (libopus celt_encoder.c).
	if e.lastCodedBands != 0 {
		e.lastCodedBands = min(e.lastCodedBands+1, max(e.lastCodedBands-1, state.codedBands))
	} else {
		e.lastCodedBands = state.codedBands
	}

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
