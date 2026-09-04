// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gocognit,gocyclo,gosec,lll,maintidx,nestif,nlreturn,wastedassign // Mirrors RFC 6716 allocation flow and bounded entropy-code arithmetic.
package celt

import (
	"math"

	"github.com/pion/opus/internal/rangecoding"
)

const (
	allocationSteps = 6
	maxFineBits     = 8
	fineOffset      = 21
)

// lsbDepth is the assumed bit depth of the input PCM. libopus uses 24 for
// float input and 16 for int16. pion always receives float32, so 24.
const lsbDepth = 24

type allocationState struct {
	pulses       [maxBands]int // Shape budget in 1/8-bit units; PVQ converts this to a pulse count later.
	fineQuant    [maxBands]int
	finePriority [maxBands]int
	intensity    int
	dualStereo   int
	balance      int
	codedBands   int
	bits         int
}

type dynallocResult struct {
	offsets      [maxBands]int
	spreadWeight [maxBands]int
	// importance weights each band in tfAnalysis's Viterbi cost, so bands
	// that stand out from their neighbors dominate the resolution choice.
	importance   [maxBands]int
	maxDepth     float32
	totBoostBits int
}

// computeAllocation derives the per-band shape and fine-energy budgets in
// 1/8-bit units.  The implementation follows the RFC 6716 Section 4.3.3
// structure: reserve later side-symbol budgets, choose and interpolate static
// allocation vectors, decide coded bands and stereo side data, then split each
// coded-band budget between fine energy and shape coding.
func (d *Decoder) computeAllocation(info *frameSideInfo, bits int) allocationState {
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
		&d.rangeDecoder,
		nil,
		0,
		0,
		// The decoder reads the skip bits the encoder wrote, so it needs
		// neither the hysteresis state nor the signal bandwidth.
		0,
		0,
	)
	state.balance = balance

	return state
}

func computeAllocation(
	start, end int,
	offsets []int,
	caps []int,
	allocationTrim int,
	intensity *int,
	dualStereo *int,
	total int,
	balance *int,
	pulses []int,
	fineQuant []int,
	finePriority []int,
	channelCount int,
	lm int,
	rangeDecoder *rangecoding.Decoder,
	rangeEncoder *rangecoding.Encoder,
	targetIntensity int,
	targetDualStereo int,
	prevCodedBands int,
	signalBandwidth int,
) int {
	if total < 0 {
		total = 0
	}

	// Section 4.3.3 recovers the exact budget the encoder saw.  The symbols
	// decoded after allocation still need their entropy space reserved here.
	skipReserved := 0
	if total >= 1<<bitResolution {
		skipReserved = 1 << bitResolution
	}
	total -= skipReserved

	intensityReserved := 0
	dualStereoReserved := 0
	if channelCount == 2 {
		intensityReserved = log2FracTable[end-start]
		if intensityReserved > total {
			intensityReserved = 0
		} else {
			total -= intensityReserved
			if total >= 1<<bitResolution {
				dualStereoReserved = 1 << bitResolution
			}
			total -= dualStereoReserved
		}
	}

	bits1 := make([]int, maxBands)
	bits2 := make([]int, maxBands)
	threshold := make([]int, maxBands)
	trimOffset := make([]int, maxBands)
	// Thresholds determine whether a band receives shape bits or is folded;
	// trim offsets tilt the static allocation table toward low or high bands.
	for band := start; band < end; band++ {
		bandWidth := int(bandEdges[band+1] - bandEdges[band])
		threshold[band] = max(channelCount<<bitResolution, (3*bandWidth<<lm<<bitResolution)>>4)
		trimOffset[band] = channelCount * bandWidth * (allocationTrim - defaultAllocationTrim - lm) *
			(end - band - 1) * (1 << (lm + bitResolution)) >> 6
		if bandWidth<<lm == 1 {
			trimOffset[band] -= channelCount << bitResolution
		}
	}

	// Find the highest static allocation vector whose capped sum fits in the
	// available total.  The next vector becomes the interpolation target.
	lo := 1
	hi := len(bandAllocation) - 1
	for lo <= hi {
		mid := (lo + hi) >> 1
		psum := 0
		done := false
		for band := end - 1; band >= start; band-- {
			bandWidth := int(bandEdges[band+1] - bandEdges[band])
			bits := channelCount * bandWidth * int(bandAllocation[mid][band]) << lm >> 2
			if bits > 0 {
				bits = max(0, bits+trimOffset[band])
			}
			bits += offsets[band]
			if bits >= threshold[band] || done {
				done = true
				psum += min(bits, caps[band])
			} else if bits >= channelCount<<bitResolution {
				psum += channelCount << bitResolution
			}
		}
		if psum > total {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}

	hi = lo
	lo--
	skipStart := start
	// Convert the bracketing allocation vectors into a base budget plus delta
	// per band.  Boosted bands become the lower bound for band skipping.
	for band := start; band < end; band++ {
		bandWidth := int(bandEdges[band+1] - bandEdges[band])
		bits1Band := channelCount * bandWidth * int(bandAllocation[lo][band]) << lm >> 2
		bits2Band := 0
		if hi >= len(bandAllocation) {
			bits2Band = caps[band]
		} else {
			bits2Band = channelCount * bandWidth * int(bandAllocation[hi][band]) << lm >> 2
		}
		if bits1Band > 0 {
			bits1Band = max(0, bits1Band+trimOffset[band])
		}
		if bits2Band > 0 {
			bits2Band = max(0, bits2Band+trimOffset[band])
		}
		if lo > 0 {
			bits1Band += offsets[band]
		}
		bits2Band += offsets[band]
		if offsets[band] > 0 {
			skipStart = band
		}
		bits2[band] = max(0, bits2Band-bits1Band)
		bits1[band] = bits1Band
	}

	return interpolateBitsToPulses(
		start,
		end,
		skipStart,
		bits1,
		bits2,
		threshold,
		caps,
		total,
		balance,
		skipReserved,
		intensity,
		intensityReserved,
		dualStereo,
		dualStereoReserved,
		pulses,
		fineQuant,
		finePriority,
		channelCount,
		lm,
		rangeDecoder,
		rangeEncoder,
		targetIntensity,
		targetDualStereo,
		prevCodedBands,
		signalBandwidth,
	)
}

// interpolateBitsToPulses completes the RFC 6716 Section 4.3.3 allocation
// computation after the static table search.  The bits slice leaves this
// function as the per-band shape budget in 1/8-bit units, after fine-energy
// bits have been removed.
func interpolateBitsToPulses(
	start, end, skipStart int,
	bits1 []int,
	bits2 []int,
	threshold []int,
	caps []int,
	total int,
	balance *int,
	skipReserved int,
	intensity *int,
	intensityReserved int,
	dualStereo *int,
	dualStereoReserved int,
	bits []int,
	fineQuant []int,
	finePriority []int,
	channelCount int,
	lm int,
	rangeDecoder *rangecoding.Decoder,
	rangeEncoder *rangecoding.Encoder,
	targetIntensity int,
	targetDualStereo int,
	prevCodedBands int,
	signalBandwidth int,
) int {
	allocationFloor := channelCount << bitResolution
	stereo := boolIndex(channelCount > 1)

	// Interpolate between the two neighboring static allocation vectors with
	// six fractional steps, matching the reference CELT allocation search.
	lo := 0
	hi := 1 << allocationSteps
	for range allocationSteps {
		mid := (lo + hi) >> 1
		psum := 0
		done := false
		for band := end - 1; band >= start; band-- {
			tmp := bits1[band] + (mid * bits2[band] >> allocationSteps)
			if tmp >= threshold[band] || done {
				done = true
				psum += min(tmp, caps[band])
			} else if tmp >= allocationFloor {
				psum += allocationFloor
			}
		}
		if psum > total {
			hi = mid
		} else {
			lo = mid
		}
	}

	// Apply the interpolation point, thresholding, and per-band caps to get
	// the provisional allocation sum.
	psum := 0
	done := false
	for band := end - 1; band >= start; band-- {
		tmp := bits1[band] + (lo * bits2[band] >> allocationSteps)
		if tmp < threshold[band] && !done {
			if tmp >= allocationFloor {
				tmp = allocationFloor
			} else {
				tmp = 0
			}
		} else {
			done = true
		}
		tmp = min(tmp, caps[band])
		bits[band] = tmp
		psum += tmp
	}

	// Walk backward to find the coded band boundary.  The optional skip bit
	// lets the stream keep a high band coded when enough bits remain.
	codedBands := end
	for {
		codedBands--
		band := codedBands
		if band <= skipStart {
			total += skipReserved
			codedBands++
			break
		}

		left := total - psum
		perCoeff := left / (int(bandEdges[codedBands+1]) - int(bandEdges[start]))
		left -= (int(bandEdges[codedBands+1]) - int(bandEdges[start])) * perCoeff
		rem := max(left-(int(bandEdges[band])-int(bandEdges[start])), 0)
		bandWidth := int(bandEdges[codedBands+1] - bandEdges[band])
		bandBits := bits[band] + perCoeff*bandWidth + rem
		if bandBits >= max(threshold[band], allocationFloor+(1<<bitResolution)) {
			var skipBit bool
			if rangeDecoder != nil {
				skipBit = rangeDecoder.DecodeSymbolLogP(1) != 0
			} else {
				// This decision is the only part of the allocation that is not
				// mandated by the bitstream: the encoder is free to skip bands
				// as long as it signals them (libopus celt/rate.c). Skipping a
				// band whose budget is too thin to code meaningfully frees its
				// bits for the bands below, which fold into it instead.
				//
				// Index mapping against the reference: this loop already
				// decremented codedBands, so band is the reference's j and
				// codedBands+1 is its codedBands.
				depthThreshold := 0
				if codedBands+1 > 17 {
					// Hysteresis on the previous frame's band count keeps bands
					// from oscillating in and out of the stream.
					depthThreshold = 9
					if band < prevCodedBands {
						depthThreshold = 7
					}
				}
				keep := codedBands+1 <= start+2 ||
					(bandBits > (depthThreshold*bandWidth<<lm<<bitResolution)>>4 && band <= signalBandwidth)
				skipBit = keep
				if keep {
					rangeEncoder.EncodeSymbolLogP(1, 1)
				} else {
					rangeEncoder.EncodeSymbolLogP(1, 0)
				}
			}
			if skipBit {
				codedBands++
				break
			}
			psum += 1 << bitResolution
			bandBits -= 1 << bitResolution
		}

		psum -= bits[band] + intensityReserved
		if intensityReserved > 0 {
			intensityReserved = log2FracTable[band-start]
		}
		psum += intensityReserved
		if bandBits >= allocationFloor {
			psum += allocationFloor
			bits[band] = allocationFloor
		} else {
			bits[band] = 0
		}
	}

	// Intensity and dual-stereo symbols are decoded only after codedBands is
	// known, because their alphabets depend on the surviving band range.
	if intensityReserved > 0 {
		var value uint32
		if rangeDecoder != nil {
			value, _ = rangeDecoder.DecodeUniform(uint32(codedBands + 1 - start))
		} else {
			relTarget := codedBands - start
			if targetIntensity > 0 {
				relTarget = min(targetIntensity-start, codedBands-start)
			}
			value = uint32(max(relTarget, 0))
			rangeEncoder.EncodeUniform(uint32(codedBands+1-start), value)
		}
		*intensity = start + int(value)
	} else {
		*intensity = 0
	}
	if *intensity <= start {
		total += dualStereoReserved
		dualStereoReserved = 0
	}
	if dualStereoReserved > 0 {
		if rangeDecoder != nil {
			*dualStereo = int(rangeDecoder.DecodeSymbolLogP(1))
		} else {
			dualStereoValue := uint32(0)
			if targetDualStereo > 0 && *intensity > start {
				dualStereoValue = 1
			}
			rangeEncoder.EncodeSymbolLogP(1, dualStereoValue)
			*dualStereo = int(dualStereoValue)
		}
	} else {
		*dualStereo = 0
	}

	// Distribute any slack bits uniformly over coded MDCT bins before
	// separating each band into fine-energy and shape budgets.
	left := total - psum
	perCoeff := left / (int(bandEdges[codedBands]) - int(bandEdges[start]))
	left -= (int(bandEdges[codedBands]) - int(bandEdges[start])) * perCoeff
	for band := start; band < codedBands; band++ {
		bits[band] += perCoeff * int(bandEdges[band+1]-bandEdges[band])
	}
	for band := start; band < codedBands; band++ {
		tmp := min(left, int(bandEdges[band+1]-bandEdges[band]))
		bits[band] += tmp
		left -= tmp
	}

	currentBalance := 0
	band := start
	for ; band < codedBands; band++ {
		// Fine energy gets whole raw bits per channel first; the remainder is
		// carried forward as shape budget for PVQ in the next implementation slice.
		width := int(bandEdges[band+1] - bandEdges[band])
		n := width << lm
		bits[band] += currentBalance
		excess := 0
		if n > 1 {
			excess = max(bits[band]-caps[band], 0)
			bits[band] -= excess
			den := channelCount * n
			if channelCount == 2 && n > 2 && *dualStereo == 0 && band < *intensity {
				den++
			}
			ncLogN := den * (logN400[band] + (lm << bitResolution))
			offset := (ncLogN >> 1) - den*fineOffset
			if n == 2 {
				offset += den << bitResolution >> 2
			}
			if bits[band]+offset < den*2<<bitResolution {
				offset += ncLogN >> 2
			} else if bits[band]+offset < den*3<<bitResolution {
				offset += ncLogN >> 3
			}
			fineQuant[band] = max(0, (bits[band]+offset+(den<<(bitResolution-1)))/(den<<bitResolution))
			if channelCount*fineQuant[band] > bits[band]>>bitResolution {
				fineQuant[band] = bits[band] >> stereo >> bitResolution
			}
			fineQuant[band] = min(fineQuant[band], maxFineBits)
			finePriority[band] = boolIndex(fineQuant[band]*(den<<bitResolution) >= bits[band]+offset)
			bits[band] -= channelCount * fineQuant[band] << bitResolution
		} else {
			excess = max(0, bits[band]-(channelCount<<bitResolution))
			bits[band] -= excess
			fineQuant[band] = 0
			finePriority[band] = 1
		}
		if excess > 0 {
			extraFine := min(excess>>(stereo+bitResolution), maxFineBits-fineQuant[band])
			fineQuant[band] += extraFine
			extraBits := extraFine * channelCount << bitResolution
			finePriority[band] = boolIndex(extraBits >= excess-currentBalance)
			excess -= extraBits
		}
		currentBalance = excess
	}
	*balance = currentBalance

	// Bands above codedBands do not get shape bits, but may retain final
	// fine-energy priority bookkeeping if they had enough provisional budget.
	for ; band < end; band++ {
		fineQuant[band] = bits[band] >> stereo >> bitResolution
		bits[band] = 0
		finePriority[band] = boolIndex(fineQuant[band] < 1)
	}

	return codedBands
}

// getPulses expands the compact pulse-count cache indices used by the RFC
// 6716 Section 4.3.3 allocation tables.
func getPulses(index int) int {
	if index < 8 {
		return index
	}

	return (8 + (index & 7)) << ((index >> 3) - 1)
}

// bitsToPulses implements the RFC 6716 Section 4.3.4.1 cache search from a
// 1/8-bit shape budget to the nearest allowed integer pulse count.
func bitsToPulses(band, lm, bits int) int {
	if bits <= 0 {
		return 0
	}
	lm++
	cacheStart := int(pulseCacheIndex[lm*maxBands+band])
	if cacheStart < 0 {
		return 0
	}
	cache := pulseCacheBits[cacheStart:]
	lo := 0
	hi := int(cache[0])
	bits--
	for range 6 {
		mid := (lo + hi + 1) >> 1
		if int(cache[mid]) >= bits {
			hi = mid
		} else {
			lo = mid
		}
	}
	loBits := -1
	if lo != 0 {
		loBits = int(cache[lo])
	}
	if bits-loBits <= int(cache[hi])-bits {
		return lo
	}

	return hi
}

// pulsesToBits maps an allowed pulse count back to its cached 1/8-bit cost.
func pulsesToBits(band, lm, pulses int) int {
	if pulses == 0 {
		return 0
	}
	lm++
	cacheStart := int(pulseCacheIndex[lm*maxBands+band])

	return int(pulseCacheBits[cacheStart+pulses]) + 1
}

// decodeFineEnergy applies the first RFC 6716 Section 4.3.2.2 fine-energy
// refinement, using the number of raw bits assigned by Section 4.3.3.
func (d *Decoder) decodeFineEnergy(info *frameSideInfo, fineQuant [maxBands]int) {
	for band := info.startBand; band < info.endBand; band++ {
		if fineQuant[band] <= 0 {
			continue
		}
		for channel := range info.channelCount {
			// Fine energy uses raw tail bits so refinement does not perturb the
			// range coder state used by the main CELT symbols.
			q2 := d.rangeDecoder.DecodeRawBits(uint(fineQuant[band]))
			offset := (float32(q2)+0.5)*float32(uint(1)<<(14-fineQuant[band]))/16384 - 0.5
			d.previousLogE[channel][band] += offset
		}
	}
}

// finalizeFineEnergy consumes the RFC 6716 Section 4.3.2.2 final fine-energy
// priority bits that refine bands after PVQ decoding.
func (d *Decoder) finalizeFineEnergy(
	info *frameSideInfo,
	fineQuant [maxBands]int,
	finePriority [maxBands]int,
	bitsLeft int,
) {
	for priority := range 2 {
		for band := info.startBand; band < info.endBand && bitsLeft >= info.channelCount; band++ {
			if fineQuant[band] >= maxFineBits || finePriority[band] != priority {
				continue
			}
			for channel := range info.channelCount {
				q2 := d.rangeDecoder.DecodeRawBits(1)
				offset := (float32(q2) - 0.5) * float32(uint(1)<<(14-fineQuant[band]-1)) / 16384
				d.previousLogE[channel][band] += offset
				bitsLeft--
			}
		}
	}
}

// chooseDualStereo implements the L1-norm stereo decision from libopus
// (celt_encoder.c:stereo_analysis). It returns true when dual stereo
// (independent L/R) is preferred over mid/side coupling.
//
// The criterion compares the L1 norm of the mid/side spectrum (scaled by
// 1/sqrt(2)) against the L1 norm of the L/R spectrum, both taken over the
// normalised bands. Dual stereo wins when the M/S representation is
// relatively expensive — i.e. when the channels are uncorrelated.
func chooseDualStereo(normalized [2][]float32, lm int) bool {
	if lm == 0 {
		return false
	}

	scale := 1 << lm
	var sumLR, sumMS float64

	mdctLen := min(len(normalized[0]), len(normalized[1]))
	for band := 0; band < 13 && band+1 < len(bandEdges); band++ {
		bandStart := scale * int(bandEdges[band])
		bandEnd := min(scale*int(bandEdges[band+1]), mdctLen)
		if bandStart >= mdctLen {
			break
		}

		for i := bandStart; i < bandEnd; i++ {
			l := float64(normalized[0][i])
			r := float64(normalized[1][i])
			sumLR += math.Abs(l) + math.Abs(r)
			sumMS += math.Abs(l+r) + math.Abs(l-r)
		}
	}

	sumMS *= 0.707107

	thetas := 13
	// The lower bands do not need thetas at LM<=1.
	if lm <= 1 {
		thetas -= 8
	}

	// The weight is eBands[13]<<(LM+1), twice the bin count of the range
	// scanned above — that factor is what sets how much cheaper M/S has to be
	// before dual stereo wins.
	bins := float64(int(bandEdges[13]) << (lm + 1))

	return (bins+float64(thetas))*sumMS > bins*sumLR
}

// chooseAllocationTrim returns the allocation trim [0,10] based on bitrate,
// inter-channel correlation, and spectral tilt. Mirrors libopus
// celt_encoder.c:alloc_trim_analysis. Components that depend on unported
// modules (surround_trim, tf_estimate, tonality_slope) contribute zero.
//
// pion's logBandAmp is 0.5*log2(energy)-energyMeans, while libopus's bandLogE
// is log2(energy). The 0.5 factor is corrected with *2 in the tilt formula;
// the energyMeans offset cancels in the weighted difference.
func chooseAllocationTrim(
	logBandAmp [2][maxBands]float32,
	mdct [2][]float32,
	channelCount, lm, endBand int,
	tfEstimate float32, intensity int, stereoSaving *float32, equivRate int,
) int {
	// bitrate base, trim=5 default, 4 at low bitrate, interpolated 64-80kbps.
	trim := float32(5.0)
	if equivRate < 64000 {
		trim = 4.0
	} else if equivRate < 80000 {
		frac := float32(equivRate-64000) / 1024
		trim = 4.0 + float32(1.0/16.0)*frac
	}

	// --- Stereo correlation over the first 8 bands. libopus uses inner
	// products of normalised bands; cosine similarity of raw MDCT is the same
	// thing and scale-invariant. Correlated channels → logXC < 0 → trim
	// decreases → more bits to lows. The per-band cosines are summed signed
	// and only the average is taken absolute (celt_encoder.c:790), so bands
	// that are out of phase cancel instead of piling on.
	if channelCount == 2 {
		scale := 1 << lm
		var corrSum float32
		for band := 0; band < 8 && band < endBand; band++ {
			corrSum += bandCorrelation(mdct, scale, band)
		}
		avgCorr := abs32(corrSum / 8.0)
		if avgCorr > 1.0 {
			avgCorr = 1.0
		}
		// minXC is the weakest correlation across the intensity-coded range;
		// a single decorrelated band there means mid/side is not saving much,
		// however redundant the low bands look.
		minXC := avgCorr
		for band := 8; band < intensity && band < endBand; band++ {
			minXC = min32(minXC, abs32(bandCorrelation(mdct, scale, band)))
		}
		if minXC > 1.0 {
			minXC = 1.0
		}
		logXC := float32(math.Log2(1.001 - float64(avgCorr*avgCorr)))
		logXC2 := max32(0.5*logXC, float32(math.Log2(1.001-float64(minXC*minXC))))
		*stereoSaving = min32(*stereoSaving+0.25, -0.5*logXC2)
		trim += max32(-4.0, 0.75*logXC)
	}

	// --- Spectral tilt: weighted sum of bandLogE.
	// (2+2*i-end) is negative for low bands, positive for high.
	// Positive diff (high-freq heavy) → trim decreases → more bits to highs.
	// logBandAmp is log2(amplitude)-energyMeans, the same quantity libopus
	// calls bandLogE (amp2Log2 takes the log of the amplitude, not the
	// energy), so it goes in unscaled: the Q-domain shifts around this in
	// celt_encoder.c:817 are all no-ops in the float build.
	var diff float32
	for ch := range channelCount {
		for i := range endBand - 1 {
			diff += logBandAmp[ch][i] * float32(2+2*i-endBand)
		}
	}
	diff /= float32(channelCount * (endBand - 1))
	tiltContrib := max32(-2.0, min32(2.0, (diff+1.0)/6.0))
	trim -= tiltContrib

	// A frame that wants finer time resolution wants its bits spread up the
	// spectrum, so the reference backs the trim off by twice tf_estimate
	// (celt_encoder.c:820). The Q shift there is a no-op in the float build.
	trim -= 2 * tfEstimate

	// Round and clamp to [0, 10].
	trimIndex := int(trim + 0.5)
	if trimIndex < 0 {
		trimIndex = 0
	} else if trimIndex > 10 {
		trimIndex = 10
	}
	return trimIndex
}

// bandCorrelation returns the cosine similarity of the two channels over one
// band, which is what the reference gets from the inner product of the
// normalised spectrum.
func bandCorrelation(mdct [2][]float32, scale, band int) float32 {
	start := scale * int(bandEdges[band])
	end := scale * int(bandEdges[band+1])
	var dot, l2, r2 float32
	for i := start; i < end; i++ {
		dot += mdct[0][i] * mdct[1][i]
		l2 += mdct[0][i] * mdct[0][i]
		r2 += mdct[1][i] * mdct[1][i]
	}
	if l2 <= 1e-30 || r2 <= 1e-30 {
		return 0
	}

	return dot / sqrtf(l2*r2)
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}

	return b
}

func sqrtf(x float32) float32 { return float32(math.Sqrt(float64(x))) }

// medianOf3 returns the median of three float32 values. Port of libopus
// celt_encoder.c:median_of_3.
func medianOf3(vals [3]float32) float32 {
	if vals[0] > vals[1] {
		if vals[1] > vals[2] {
			return vals[1]
		} else if vals[0] > vals[2] {
			return vals[2]
		}

		return vals[0]
	}

	if vals[0] > vals[2] {
		return vals[0]
	} else if vals[1] > vals[2] {
		return vals[2]
	}

	return vals[1]
}

// medianOf5 returns the median of five float32 values. Port of libopus
// celt_encoder.c:median_of_5.
func medianOf5(vals [5]float32) float32 {
	lo0, hi0 := vals[0], vals[1]
	if lo0 > hi0 {
		lo0, hi0 = hi0, lo0
	}
	mid := vals[2]
	lo1, hi1 := vals[3], vals[4]
	if lo1 > hi1 {
		lo1, hi1 = hi1, lo1
	}
	if lo0 > lo1 {
		_, hi0, lo1, hi1 = lo1, hi1, lo0, hi0
	}
	if mid > hi0 {
		if hi0 < lo1 {
			return min32(mid, lo1)
		}

		return min32(hi1, hi0)
	}
	if mid < lo1 {
		return min32(hi0, lo1)
	}

	return min32(mid, hi1)
}

// dynallocAnalysis computes per-band boost offsets and spread weights.
// Mirrors libopus celt_encoder.c:dynalloc_analysis.
//
// logBandAmp is libopus's bandLogE as-is: amp2Log2 takes the log of the band
// amplitude, not the energy, and subtracts the same eMeans table. The noise
// floor and follower thresholds below live in that domain.
func dynallocAnalysis(
	logBandAmp [2][maxBands]float32,
	prevLogBandAmp [2][maxBands]float32,
	lm, startBand, endBand, channelCount int,
	effectiveBytes int,
	isTransient, vbr, constrainedVBR bool,
) dynallocResult {
	var offsets [maxBands]int
	var spreadWeight [maxBands]int

	var noiseFloor [maxBands]float32
	for band := range endBand {
		noiseFloor[band] = 0.0625*float32(logN400[band]) +
			0.5 + float32(9-lsbDepth) -
			energyMeans[band] +
			0.0062*float32((band+5)*(band+5))
	}

	bandLogE := logBandAmp

	maxDepth := float32(-31.9)
	for ch := range channelCount {
		for band := range endBand {
			depth := bandLogE[ch][band] - noiseFloor[band]
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	{
		var mask, sig [maxBands]float32
		for band := range endBand {
			mask[band] = bandLogE[0][band] - noiseFloor[band]
			if channelCount == 2 {
				d1 := bandLogE[1][band] - noiseFloor[band]
				if d1 > mask[band] {
					mask[band] = d1
				}
			}
			sig[band] = mask[band]
		}
		// The reference spreads the mask by 2 and 3 in the same log2 domain as
		// bandLogE, not in dB (celt_encoder.c: dynalloc_analysis, GCONST is the
		// identity in the float build).
		for band := 1; band < endBand; band++ {
			if mask[band-1]-2.0 > mask[band] {
				mask[band] = mask[band-1] - 2.0
			}
		}
		for band := endBand - 2; band >= 0; band-- {
			if mask[band+1]-3.0 > mask[band] {
				mask[band] = mask[band+1] - 3.0
			}
		}
		// SMR → shift → spread_weight = 32 >> shift.
		for band := range endBand {
			masked := max32(0, maxDepth-12.0)
			if mask[band] > masked {
				masked = mask[band]
			}
			smr := sig[band] - masked
			shift := 0
			if smr < 0 {
				shift = int(-smr + 0.5)
			}
			if shift > 5 {
				shift = 5
			}
			if shift < 0 {
				shift = 0
			}
			spreadWeight[band] = 32 >> shift
		}
	}

	if effectiveBytes < 30+5*lm {
		var flat [maxBands]int
		for band := range flat {
			flat[band] = 13
		}

		return dynallocResult{
			offsets:      offsets,
			spreadWeight: spreadWeight,
			importance:   flat,
			maxDepth:     maxDepth,
			totBoostBits: 0,
		}
	}

	// bandLogE3 is libopus's bandLogE2, which below complexity 8 is just a
	// copy of the current frame's bandLogE (celt_encoder.c:2211). logBandAmp
	// already is that quantity — amp2Log2 takes the log of the amplitude —
	// so it goes in as-is, mean-subtracted like the rest of the energies.
	var follower [2][maxBands]float32
	for ch := range channelCount {
		var bandLogE3 [maxBands]float32
		for band := range endBand {
			bandLogE3[band] = bandLogE[ch][band]
		}

		// For LM==0, first 8 bands have 1 bin → unreliable. Take max with the
		// previous frame so at least 2 "bins" of context are used.
		if lm == 0 {
			for band := 0; band < endBand && band < 8; band++ {
				if prevLogBandAmp[ch][band] > bandLogE3[band] {
					bandLogE3[band] = prevLogBandAmp[ch][band]
				}
			}
		}

		follow := &follower[ch]
		follow[0] = bandLogE3[0]
		last := 0
		for band := 1; band < endBand; band++ {
			if bandLogE3[band] > bandLogE3[band-1]+0.5 {
				last = band
			}
			prev := follow[band-1] + 1.5
			follow[band] = bandLogE3[band]
			if prev < follow[band] {
				follow[band] = prev
			}
		}
		for band := last - 1; band >= 0; band-- {
			next := follow[band+1] + 2.0
			if next < follow[band] {
				follow[band] = next
			}
			if bandLogE3[band] < follow[band] {
				follow[band] = bandLogE3[band]
			}
		}

		// Median filter with 1.0 dB offset (≈ 0.166 log2) to avoid
		// triggering on smooth spectral tilts.
		const offset = 1.0
		for band := 2; band < endBand-2; band++ {
			m := medianOf5([5]float32{
				bandLogE3[band-2], bandLogE3[band-1], bandLogE3[band],
				bandLogE3[band+1], bandLogE3[band+2],
			}) - offset
			if m > follow[band] {
				follow[band] = m
			}
		}

		if endBand >= 3 {
			tmp := medianOf3([3]float32{bandLogE3[0], bandLogE3[1], bandLogE3[2]}) - offset
			if tmp > follow[0] {
				follow[0] = tmp
			}
			if tmp > follow[1] {
				follow[1] = tmp
			}
			tmp = medianOf3([3]float32{
				bandLogE3[endBand-3], bandLogE3[endBand-2], bandLogE3[endBand-1],
			}) - offset
			if tmp > follow[endBand-2] {
				follow[endBand-2] = tmp
			}
			if tmp > follow[endBand-1] {
				follow[endBand-1] = tmp
			}
		}

		for band := range endBand {
			if noiseFloor[band] > follow[band] {
				follow[band] = noiseFloor[band]
			}
		}
	}

	var combined [maxBands]float32
	if channelCount == 2 {
		for band := startBand; band < endBand; band++ {
			if follower[1][band] < follower[0][band]-4.0 {
				follower[1][band] = follower[0][band] - 4.0
			}
			if follower[0][band] < follower[1][band]-4.0 {
				follower[0][band] = follower[1][band] - 4.0
			}
			d0 := bandLogE[0][band] - follower[0][band]
			if d0 < 0 {
				d0 = 0
			}
			d1 := bandLogE[1][band] - follower[1][band]
			if d1 < 0 {
				d1 = 0
			}
			combined[band] = 0.5 * (d0 + d1)
		}
	} else {
		for band := startBand; band < endBand; band++ {
			d := bandLogE[0][band] - follower[0][band]
			if d < 0 {
				d = 0
			}
			combined[band] = d
		}
	}

	// importance mirrors libopus dynalloc_analysis: bands whose energy sits
	// well above their masking follower matter more when tfAnalysis weighs
	// its Viterbi cost. celt_exp2_db is celt_exp2 in the float build, so this
	// stays in log2 units like the rest of pion's energies.
	var importance [maxBands]int
	for band := range importance {
		importance[band] = 13
	}
	for band := startBand; band < endBand; band++ {
		importance[band] = int(math.Floor(0.5 +
			13*math.Exp2(float64(minFloat32(combined[band], 4)))))
	}

	// CBR and constrained VBR halve the dynalloc contribution on a steady
	// frame; unconstrained VBR keeps it whole, because there the boost also
	// raises the frame's own target instead of competing for a fixed budget.
	if (!vbr || constrainedVBR) && !isTransient {
		for band := startBand; band < endBand; band++ {
			combined[band] *= 0.5
		}
	}
	for band := startBand; band < endBand; band++ {
		if band < 8 {
			combined[band] *= 2
		}
		if band >= 12 {
			combined[band] *= 0.5
		}
	}

	// Extra boost for band 0 at very high bitrate.
	if effectiveBytes > 320 {
		extra := float32(0.001) * float32(effectiveBytes-320)
		if extra > 1.5 {
			extra = 1.5
		}
		combined[0] += extra
	}

	// The two SHR32s libopus applies here (celt_encoder.c:1240,1243) are the
	// fixed-point Q-domain conversion — both no-ops in the float build — so the
	// follower feeds the boost quanta unscaled.
	totBoostBits := 0
	for band := startBand; band < endBand; band++ {
		if combined[band] > 4.0 {
			combined[band] = 4.0
		}
		width := channelCount * int(bandEdges[band+1]-bandEdges[band]) << lm

		var boost int
		var boostBits int
		switch {
		case width < 6:
			boost = int(combined[band])
			boostBits = boost * width << bitResolution
		case width > 48:
			boost = int(combined[band] * 8)
			boostBits = (boost * width << bitResolution) / 8
		default:
			boost = int(combined[band] * float32(width) / 6.0)
			boostBits = boost * 6 << bitResolution
		}

		// CBR — and a constrained-VBR frame that is not a transient — keeps
		// dynalloc under 2/3 of the frame. Unconstrained VBR has no such cap:
		// there the boost total also raises the frame's own target, so capping
		// it here would quietly hold the whole frame below the asked rate.
		capped := !vbr || (constrainedVBR && !isTransient)
		if capped && (totBoostBits+boostBits)>>bitResolution>>3 > 2*effectiveBytes/3 {
			capBits := (2 * effectiveBytes / 3) << bitResolution << 3
			offsets[band] = capBits - totBoostBits
			totBoostBits = capBits

			break
		}
		offsets[band] = boost
		totBoostBits += boostBits
	}

	return dynallocResult{
		offsets:      offsets,
		spreadWeight: spreadWeight,
		importance:   importance,
		maxDepth:     maxDepth,
		totBoostBits: totBoostBits,
	}
}
