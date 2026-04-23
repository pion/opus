// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gocognit,gocyclo,gosec,lll,maintidx,nestif,nlreturn,wastedassign // Mirrors RFC 6716 allocation flow and bounded entropy-code arithmetic.
package celt

import "github.com/pion/opus/internal/rangecoding"

const (
	allocationSteps = 6
	maxFineBits     = 8
	fineOffset      = 21
)

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
			if rangeDecoder.DecodeSymbolLogP(1) != 0 {
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
		value, _ := rangeDecoder.DecodeUniform(uint32(codedBands + 1 - start))
		*intensity = start + int(value)
	} else {
		*intensity = 0
	}
	if *intensity <= start {
		total += dualStereoReserved
		dualStereoReserved = 0
	}
	if dualStereoReserved > 0 {
		*dualStereo = int(rangeDecoder.DecodeSymbolLogP(1))
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
