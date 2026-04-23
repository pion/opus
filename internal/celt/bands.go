// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gocognit,gocyclo,gosec,lll,maintidx,nestif,varnamelen,wastedassign // Keeps the PVQ band recursion close to the RFC/C reference.
package celt

import (
	"math"
	"math/bits"

	"github.com/pion/opus/internal/rangecoding"
)

const (
	qThetaOffset         = 4
	qThetaOffsetTwoPhase = 16
)

var orderyTable = [...]int{ //nolint:gochecknoglobals
	1, 0,
	3, 0, 2, 1,
	7, 0, 4, 3, 6, 1, 5, 2,
	15, 0, 8, 7, 12, 3, 11, 4, 14, 1, 9, 6, 13, 2, 10, 5,
}

type bandDecodeState struct {
	rangeDecoder *rangecoding.Decoder
	seed         uint32
	pulseScratch []int
	tmpScratch   []float32
}

// quantAllBands drives RFC 6716 Section 4.3.4 shape decoding across the coded
// band range. It keeps the allocation balance, lowband folding source, and
// per-band collapse masks needed by later anti-collapse and synthesis stages.
func quantAllBands(info *frameSideInfo, x []float32, y []float32, totalBits int, state *bandDecodeState) []byte {
	channelCount := 1
	if y != nil {
		channelCount = 2
	}
	blocks := 1
	if info.transient {
		blocks = 1 << info.lm
	}
	scale := 1 << info.lm
	frameBins := scale * int(bandEdges[maxBands])
	norm := make([]float32, channelCount*frameBins)
	norm2 := norm[frameBins:]
	lowbandScratch := make([]float32, scale*int(bandEdges[maxBands]-bandEdges[maxBands-1]))
	collapseMasks := make([]byte, channelCount*maxBands)

	lowbandOffset := 0
	updateLowband := true
	balance := info.allocation.balance
	dualStereo := channelCount == 2 && info.allocation.dualStereo != 0
	for band := info.startBand; band < info.endBand; band++ {
		tell := int(state.rangeDecoder.TellFrac())
		if band != info.startBand {
			balance -= tell
		}
		remainingBits := totalBits - tell - 1
		bandBits := 0
		if band <= info.allocation.codedBands-1 {
			currentBalance := balance / min(3, info.allocation.codedBands-band)
			bandBits = max(0, min(16383, min(remainingBits+1, info.allocation.pulses[band]+currentBalance)))
		}

		bandStart := scale * int(bandEdges[band])
		bandEnd := scale * int(bandEdges[band+1])
		bandWidth := bandEnd - bandStart
		// Shape folding reuses an earlier decoded band with matching width when
		// the current band has too few pulses to code independently.
		if bandStart-bandWidth >= scale*int(bandEdges[info.startBand]) || band == info.startBand+1 {
			if updateLowband || lowbandOffset == 0 {
				lowbandOffset = band
			}
		}
		if band == info.startBand+1 && info.startBand+2 <= maxBands {
			n1 := scale * int(bandEdges[info.startBand+1]-bandEdges[info.startBand])
			n2 := scale * int(bandEdges[info.startBand+2]-bandEdges[info.startBand+1])
			offset := scale * int(bandEdges[info.startBand])
			if n2 > n1 {
				copy(norm[offset+n1:offset+n2], norm[offset+2*n1-n2:offset+n1])
				if channelCount == 2 {
					copy(norm2[offset+n1:offset+n2], norm2[offset+2*n1-n2:offset+n1])
				}
			}
		}

		effectiveLowband := -1
		xMask := uint(0)
		yMask := uint(0)
		if lowbandOffset != 0 && (info.spread != spreadAggressive || blocks > 1 || info.tfChange[band] < 0) {
			effectiveLowband = max(scale*int(bandEdges[info.startBand]), scale*int(bandEdges[lowbandOffset])-bandWidth)
			foldStart := lowbandOffset
			for {
				foldStart--
				if scale*int(bandEdges[foldStart]) <= effectiveLowband {
					break
				}
			}
			foldEnd := lowbandOffset - 1
			for {
				foldEnd++
				if foldEnd >= band || scale*int(bandEdges[foldEnd]) >= effectiveLowband+bandWidth {
					break
				}
			}
			for fold := foldStart; fold < foldEnd; fold++ {
				xMask |= uint(collapseMasks[fold*channelCount])
				yMask |= uint(collapseMasks[fold*channelCount+channelCount-1])
			}
		} else {
			xMask = (1 << blocks) - 1
			yMask = xMask
		}

		if dualStereo && band == info.allocation.intensity {
			dualStereo = false
			for i := scale * int(bandEdges[info.startBand]); i < bandStart; i++ {
				norm[i] = 0.5 * (norm[i] + norm2[i])
			}
		}

		var lowband []float32
		if effectiveLowband >= 0 {
			lowband = norm[effectiveLowband:]
		}
		if dualStereo {
			xMask = quantBand(
				band,
				x[bandStart:bandEnd],
				nil,
				bandWidth,
				bandBits/2,
				info.spread,
				blocks,
				info.allocation.intensity,
				info.tfChange[band],
				lowband,
				&remainingBits,
				info.lm,
				norm[bandStart:],
				0,
				1,
				lowbandScratch,
				xMask,
				state,
			)
			var lowbandY []float32
			if effectiveLowband >= 0 {
				lowbandY = norm2[effectiveLowband:]
			}
			yMask = quantBand(
				band,
				y[bandStart:bandEnd],
				nil,
				bandWidth,
				bandBits/2,
				info.spread,
				blocks,
				info.allocation.intensity,
				info.tfChange[band],
				lowbandY,
				&remainingBits,
				info.lm,
				norm2[bandStart:],
				0,
				1,
				lowbandScratch,
				yMask,
				state,
			)
		} else {
			xMask = quantBand(
				band,
				x[bandStart:bandEnd],
				yBandSlice(y, bandStart, bandEnd),
				bandWidth,
				bandBits,
				info.spread,
				blocks,
				info.allocation.intensity,
				info.tfChange[band],
				lowband,
				&remainingBits,
				info.lm,
				norm[bandStart:],
				0,
				1,
				lowbandScratch,
				xMask|yMask,
				state,
			)
			yMask = xMask
		}
		collapseMasks[band*channelCount] = byte(xMask)
		collapseMasks[band*channelCount+channelCount-1] = byte(yMask)
		balance += info.allocation.pulses[band] + tell
		updateLowband = bandBits > bandWidth<<bitResolution
	}

	return collapseMasks
}

func yBandSlice(y []float32, start, end int) []float32 {
	if y == nil {
		return nil
	}

	return y[start:end]
}

// quantBand is the recursive RFC 6716 Section 4.3.4 shape decoder for one band
// or sub-band. It handles the base one-bin sign case, time-frequency changes,
// split decoding, PVQ pulse decoding, and lowband folding.
func quantBand(
	band int,
	x []float32,
	y []float32,
	n int,
	bandBits int,
	spread int,
	blocks int,
	intensity int,
	tfChange int,
	lowband []float32,
	remainingBits *int,
	lm int,
	lowbandOut []float32,
	level int,
	gain float32,
	lowbandScratch []float32,
	fill uint,
	state *bandDecodeState,
) uint {
	fullBand := x
	stereo := y != nil
	split := stereo
	originalN := n
	nPerBlock := n / blocks
	originalBlocks := blocks
	longBlocks := blocks == 1
	timeDivide := 0
	recombine := 0
	invert := false
	mid := float32(0)
	side := float32(0)
	collapseMask := uint(0)

	if n == 1 {
		// A single coefficient has no shape left to decode; only its sign is
		// coded as a raw tail bit when budget remains.
		channels := 1
		if stereo {
			channels = 2
		}
		for channel := range channels {
			sign := uint32(0)
			if *remainingBits >= 1<<bitResolution {
				sign = state.rangeDecoder.DecodeRawBits(1)
				*remainingBits -= 1 << bitResolution
				bandBits -= 1 << bitResolution
			}
			value := float32(normScaling)
			if sign != 0 {
				value = -value
			}
			if channel == 0 {
				x[0] = value
			} else {
				y[0] = value
			}
		}
		if lowbandOut != nil {
			lowbandOut[0] = x[0]
		}

		return 1
	}

	if !stereo && level == 0 {
		// Section 4.3.4.5 changes time/frequency resolution by applying
		// Hadamard recombination before recursive shape decoding.
		if tfChange > 0 {
			recombine = tfChange
		}
		if lowband != nil && (recombine != 0 || (nPerBlock&1) == 0 && tfChange < 0 || originalBlocks > 1) {
			copy(lowbandScratch[:n], lowband[:n])
			lowband = lowbandScratch[:n]
		}
		for k := range recombine {
			if lowband != nil {
				haar1(lowband, n>>k, 1<<k)
			}
			fill = bitInterleave(fill)
		}
		blocks >>= recombine
		nPerBlock <<= recombine
		for (nPerBlock&1) == 0 && tfChange < 0 {
			if lowband != nil {
				haar1(lowband, nPerBlock, blocks)
			}
			fill |= fill << blocks
			blocks <<= 1
			nPerBlock >>= 1
			timeDivide++
			tfChange++
		}
		originalBlocks = blocks
		if originalBlocks > 1 {
			if lowband != nil {
				deinterleaveHadamard(
					lowband,
					nPerBlock>>recombine,
					originalBlocks<<recombine,
					longBlocks,
					state,
				)
			}
		}
	}

	if !stereo && lm != -1 && shouldSplitBand(band, lm, bandBits) && n > 2 {
		// Section 4.3.4.4 splits oversized codebooks recursively so PVQ
		// indices stay within the range coder's bounded integer coding.
		n >>= 1
		y = x[n:]
		x = x[:n]
		split = true
		lm--
		if blocks == 1 {
			fill = (fill & 1) | (fill << 1)
		}
		blocks = (blocks + 1) >> 1
	}

	if split {
		pulseCap := logN400[band] + lm*(1<<bitResolution)
		thetaOffset := qThetaOffset
		if stereo && n == 2 {
			thetaOffset = qThetaOffsetTwoPhase
		}
		qn := computeQN(n, bandBits, (pulseCap>>1)-thetaOffset, pulseCap, stereo)
		if stereo && band >= intensity {
			qn = 1
		}
		tell := int(state.rangeDecoder.TellFrac())
		itheta := 0
		if qn != 1 {
			itheta = decodeBandTheta(qn, n, stereo, originalBlocks, state.rangeDecoder)
			itheta = itheta * 16384 / qn
		} else if stereo {
			if bandBits > 2<<bitResolution && *remainingBits > 2<<bitResolution {
				invert = state.rangeDecoder.DecodeSymbolLogP(2) != 0
			}
		}
		qalloc := int(state.rangeDecoder.TellFrac()) - tell
		bandBits -= qalloc

		// Decode the split angle into mid/side gains. Extreme angles collapse
		// one side and restrict the collapse mask to the surviving blocks.
		originalFill := fill
		delta := 0
		imid := 0
		iside := 0
		switch itheta {
		case 0:
			imid = 32767
			fill &= (1 << blocks) - 1
			delta = -16384
		case 16384:
			iside = 32767
			fill &= ((1 << blocks) - 1) << blocks
			delta = 16384
		default:
			imid = bitexactCos(itheta)
			iside = bitexactCos(16384 - itheta)
			delta = fracMul16((n-1)<<7, bitexactLog2Tan(iside, imid))
		}
		mid = float32(imid) / 32768
		side = float32(iside) / 32768

		if n == 2 && stereo {
			midBits := bandBits
			sideBits := 0
			if itheta != 0 && itheta != 16384 {
				sideBits = 1 << bitResolution
			}
			midBits -= sideBits
			*remainingBits -= qalloc + sideBits
			x2 := x
			y2 := y
			if itheta > 8192 {
				x2, y2 = y, x
			}
			sign := uint32(0)
			if sideBits != 0 {
				sign = state.rangeDecoder.DecodeRawBits(1)
			}
			signScale := float32(1)
			if sign != 0 {
				signScale = -1
			}
			collapseMask = quantBand(band, x2, nil, n, midBits, spread, blocks, intensity, tfChange, lowband, remainingBits, lm, lowbandOut, level, gain, lowbandScratch, originalFill, state)
			y2[0] = -signScale * x2[1]
			y2[1] = signScale * x2[0]
			x0 := mid * x[0]
			x1 := mid * x[1]
			y0 := side * y[0]
			y1 := side * y[1]
			x[0] = x0 - y0
			y[0] = x0 + y0
			x[1] = x1 - y1
			y[1] = x1 + y1
		} else {
			if originalBlocks > 1 && !stereo && itheta&0x3fff != 0 {
				if itheta > 8192 {
					delta -= delta >> (4 - lm)
				} else {
					delta = min(0, delta+(n<<bitResolution>>(5-lm)))
				}
			}
			midBits := max(0, min(bandBits, (bandBits-delta)/2))
			sideBits := bandBits - midBits
			*remainingBits -= qalloc
			var nextLowband2 []float32
			if lowband != nil && !stereo {
				nextLowband2 = lowband[n:]
			}
			var nextLowbandOut1 []float32
			nextLevel := 0
			if stereo {
				nextLowbandOut1 = lowbandOut
			} else {
				nextLevel = level + 1
			}
			collapseShift := 0
			if !stereo {
				collapseShift = originalBlocks >> 1
			}
			rebalance := *remainingBits
			if midBits >= sideBits {
				midGain := gain * mid
				if stereo {
					midGain = 1
				}
				collapseMask = quantBand(band, x, nil, n, midBits, spread, blocks, intensity, tfChange, lowband, remainingBits, lm, nextLowbandOut1, nextLevel, midGain, lowbandScratch, fill, state)
				rebalance = midBits - (rebalance - *remainingBits)
				if rebalance > 3<<bitResolution && itheta != 0 {
					sideBits += rebalance - (3 << bitResolution)
				}
				collapseMask |= quantBand(band, y, nil, n, sideBits, spread, blocks, intensity, tfChange, nextLowband2, remainingBits, lm, nil, nextLevel, gain*side, nil, fill>>blocks, state) << collapseShift
			} else {
				collapseMask = quantBand(band, y, nil, n, sideBits, spread, blocks, intensity, tfChange, nextLowband2, remainingBits, lm, nil, nextLevel, gain*side, nil, fill>>blocks, state) << collapseShift
				rebalance = sideBits - (rebalance - *remainingBits)
				if rebalance > 3<<bitResolution && itheta != 16384 {
					midBits += rebalance - (3 << bitResolution)
				}
				midGain := gain * mid
				if stereo {
					midGain = 1
				}
				collapseMask |= quantBand(band, x, nil, n, midBits, spread, blocks, intensity, tfChange, lowband, remainingBits, lm, nextLowbandOut1, nextLevel, midGain, lowbandScratch, fill, state)
			}
		}
	} else {
		// Non-split bands convert the 1/8-bit allocation to a pulse count
		// (Section 4.3.4.1), then decode PVQ pulses or fold from a lowband.
		q := bitsToPulses(band, lm, bandBits)
		currentBits := pulsesToBits(band, lm, q)
		*remainingBits -= currentBits
		for *remainingBits < 0 && q > 0 {
			*remainingBits += currentBits
			q--
			currentBits = pulsesToBits(band, lm, q)
			*remainingBits -= currentBits
		}
		if q != 0 {
			collapseMask = algUnquant(x, n, getPulses(q), spread, blocks, state.rangeDecoder, gain, state)
		} else {
			mask := uint(1<<blocks) - 1
			fill &= mask
			if fill == 0 {
				for i := range n {
					x[i] = 0
				}
			} else {
				if lowband == nil {
					for i := range n {
						state.seed = lcgRand(state.seed)
						x[i] = float32(int32(state.seed) >> 20)
					}
					collapseMask = mask
				} else {
					for i := range n {
						state.seed = lcgRand(state.seed)
						noise := float32(1.0 / 256)
						if state.seed&0x8000 == 0 {
							noise = -noise
						}
						x[i] = lowband[i] + noise
					}
					collapseMask = fill
				}
				renormaliseVector(x, n, gain)
			}
		}
	}

	if stereo {
		if n != 2 {
			stereoMerge(x, y, mid, n)
		}
		if invert {
			for i := range n {
				y[i] = -y[i]
			}
		}
	} else if level == 0 {
		x = fullBand
		if originalBlocks > 1 {
			interleaveHadamard(x, nPerBlock>>recombine, originalBlocks<<recombine, longBlocks, state)
		}
		nPerBlock = originalN / originalBlocks
		blocks = originalBlocks
		for range timeDivide {
			blocks >>= 1
			nPerBlock <<= 1
			collapseMask |= collapseMask >> blocks
			haar1(x, nPerBlock, blocks)
		}
		for k := range recombine {
			collapseMask = bitDeinterleave(collapseMask)
			haar1(x, originalN>>k, 1<<k)
		}
		blocks <<= recombine
		if lowbandOut != nil {
			scale := float32(math.Sqrt(float64(originalN)))
			for i := range originalN {
				lowbandOut[i] = scale * x[i]
			}
		}
		collapseMask &= (1 << blocks) - 1
	}

	return collapseMask
}

// shouldSplitBand mirrors the reference codebook-size guard for Section
// 4.3.4.4 split decoding.
func shouldSplitBand(band int, lm int, bandBits int) bool {
	cacheStart := int(pulseCacheIndex[(lm+1)*maxBands+band])
	if cacheStart < 0 {
		return false
	}
	cache := pulseCacheBits[cacheStart:]

	return bandBits > int(cache[cache[0]])+12
}

// decodeBandTheta decodes the split angle used for mono split bands and stereo
// mid/side coupling in RFC 6716 Section 4.3.4.4.
func decodeBandTheta(qn int, n int, stereo bool, blocks int, rangeDecoder *rangecoding.Decoder) int {
	if stereo && n > 2 {
		p0 := uint32(3)
		x0 := uint32(qn / 2)
		total := p0*(x0+1) + x0
		fs := rangeDecoder.DecodeCumulative(total)
		x := uint32(0)
		if fs < (x0+1)*p0 {
			x = fs / p0
		} else {
			x = x0 + 1 + (fs - (x0+1)*p0)
		}
		var low, high uint32
		if x <= x0 {
			low = p0 * x
			high = p0 * (x + 1)
		} else {
			low = (x - 1 - x0) + (x0+1)*p0
			high = (x - x0) + (x0+1)*p0
		}
		rangeDecoder.UpdateCumulative(low, high, total)

		return int(x)
	}
	if blocks > 1 || stereo {
		value, _ := rangeDecoder.DecodeUniform(uint32(qn + 1))

		return int(value)
	}

	half := qn >> 1
	total := uint32((half + 1) * (half + 1))
	fm := rangeDecoder.DecodeCumulative(total)
	var itheta, symbolFrequency, low int
	if fm < uint32(half*(half+1)>>1) {
		itheta = (int(isqrt32(8*fm+1)) - 1) >> 1
		symbolFrequency = itheta + 1
		low = itheta * (itheta + 1) >> 1
	} else {
		itheta = (2*(qn+1) - int(isqrt32(8*(total-fm-1)+1))) >> 1
		symbolFrequency = qn + 1 - itheta
		low = int(total) - ((qn + 1 - itheta) * (qn + 2 - itheta) >> 1)
	}
	rangeDecoder.UpdateCumulative(uint32(low), uint32(low+symbolFrequency), total)

	return itheta
}

func computeQN(n int, bitsValue int, offset int, pulseCap int, stereo bool) int {
	exp2Table8 := [...]int{16384, 17866, 19483, 21247, 23170, 25267, 27554, 30048}
	n2 := 2*n - 1
	if stereo && n == 2 {
		n2--
	}
	qb := min(bitsValue-pulseCap-(4<<bitResolution), (bitsValue+n2*offset)/n2)
	qb = min(8<<bitResolution, qb)
	if qb < 1<<bitResolution>>1 {
		return 1
	}

	return ((exp2Table8[qb&0x7] >> (14 - (qb >> bitResolution))) + 1) >> 1 << 1
}

func bitexactCos(x int) int {
	tmp := (4096 + x*x) >> 13
	x2 := tmp
	x2 = (32767 - x2) + fracMul16(x2, -7651+fracMul16(x2, 8277+fracMul16(-626, x2)))

	return 1 + x2
}

func bitexactLog2Tan(isin int, icos int) int {
	lc := bits.Len(uint(icos))
	ls := bits.Len(uint(isin))
	icos <<= 15 - lc
	isin <<= 15 - ls

	return (ls-lc)*(1<<11) +
		fracMul16(isin, fracMul16(isin, -2597)+7932) -
		fracMul16(icos, fracMul16(icos, -2597)+7932)
}

func fracMul16(a int, b int) int {
	return (16384 + int(int16(a))*int(int16(b))) >> 15
}

func isqrt32(value uint32) uint32 {
	if value == 0 {
		return 0
	}
	g := uint32(0)
	bShift := (bits.Len32(value) - 1) >> 1
	b := uint32(1) << bShift
	for {
		t := ((g << 1) + b) << bShift
		if t <= value {
			g += b
			value -= t
		}
		if bShift == 0 {
			break
		}
		b >>= 1
		bShift--
	}

	return g
}

func lcgRand(seed uint32) uint32 {
	return 1664525*seed + 1013904223
}

func stereoMerge(x []float32, y []float32, mid float32, n int) {
	cross := float32(0)
	sideEnergy := float32(0)
	for i := range n {
		cross += x[i] * y[i]
		sideEnergy += y[i] * y[i]
	}
	cross *= mid
	leftEnergy := mid*mid + sideEnergy - 2*cross
	rightEnergy := mid*mid + sideEnergy + 2*cross
	if leftEnergy < 6e-4 || rightEnergy < 6e-4 {
		copy(y[:n], x[:n])

		return
	}
	leftScale := float32(1 / math.Sqrt(float64(leftEnergy)))
	rightScale := float32(1 / math.Sqrt(float64(rightEnergy)))
	for i := range n {
		left := mid*x[i] - y[i]
		right := mid*x[i] + y[i]
		x[i] = left * leftScale
		y[i] = right * rightScale
	}
}

func haar1(x []float32, n0 int, stride int) {
	n0 >>= 1
	for i := range stride {
		for j := range n0 {
			index0 := stride*2*j + i
			index1 := stride*(2*j+1) + i
			tmp0 := float32(math.Sqrt(0.5)) * x[index0]
			tmp1 := float32(math.Sqrt(0.5)) * x[index1]
			x[index0] = tmp0 + tmp1
			x[index1] = tmp0 - tmp1
		}
	}
}

func deinterleaveHadamard(x []float32, n0 int, stride int, hadamard bool, state *bandDecodeState) {
	tmp := state.floatScratch(n0 * stride)
	if hadamard {
		ordery := orderyTable[stride-2:]
		for i := range stride {
			for j := range n0 {
				tmp[ordery[i]*n0+j] = x[j*stride+i]
			}
		}
	} else {
		for i := range stride {
			for j := range n0 {
				tmp[i*n0+j] = x[j*stride+i]
			}
		}
	}
	copy(x, tmp)
}

func interleaveHadamard(x []float32, n0 int, stride int, hadamard bool, state *bandDecodeState) {
	tmp := state.floatScratch(n0 * stride)
	if hadamard {
		ordery := orderyTable[stride-2:]
		for i := range stride {
			for j := range n0 {
				tmp[j*stride+i] = x[ordery[i]*n0+j]
			}
		}
	} else {
		for i := range stride {
			for j := range n0 {
				tmp[j*stride+i] = x[i*n0+j]
			}
		}
	}
	copy(x, tmp)
}

func (s *bandDecodeState) intScratch(n int) []int {
	if cap(s.pulseScratch) < n {
		s.pulseScratch = make([]int, n)
	}
	s.pulseScratch = s.pulseScratch[:n]
	clear(s.pulseScratch)

	return s.pulseScratch
}

func (s *bandDecodeState) floatScratch(n int) []float32 {
	if cap(s.tmpScratch) < n {
		s.tmpScratch = make([]float32, n)
	}
	s.tmpScratch = s.tmpScratch[:n]

	return s.tmpScratch
}

func bitInterleave(fill uint) uint {
	table := [...]uint{0, 1, 1, 1, 2, 3, 3, 3, 2, 3, 3, 3, 2, 3, 3, 3}

	return table[fill&0xF] | table[fill>>4]<<2
}

func bitDeinterleave(fill uint) uint {
	table := [...]uint{
		0x00, 0x03, 0x0C, 0x0F, 0x30, 0x33, 0x3C, 0x3F,
		0xC0, 0xC3, 0xCC, 0xCF, 0xF0, 0xF3, 0xFC, 0xFF,
	}

	return table[fill&0xF]
}
