// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gocognit,gocyclo,gosec,lll,maintidx,nestif,varnamelen,wastedassign // Keep encoder recursion close to decode/RFC structure.
package celt

import (
	"math"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/pion/opus/internal/slicetools"
)

type bandEncodeState struct {
	rangeEncoder *rangecoding.Encoder
	seed         uint32
	tmpScratch   []float32
}

func (s *bandEncodeState) floatScratch(n int) []float32 {
	return slicetools.Resize(&s.tmpScratch, n)
}

func normaliseBandsForEncoding(
	info *frameSideInfo,
	mdct []float32,
	logBandAmp [maxBands]float32,
) []float32 {
	out := make([]float32, len(mdct))
	scale := 1 << info.lm

	for band := info.startBand; band < info.endBand; band++ {
		start := scale * int(bandEdges[band])
		end := scale * int(bandEdges[band+1])
		amp := float32(math.Pow(2, float64(logBandAmp[band]+energyMeans[band])))
		if amp <= 1e-15 {
			continue
		}

		invAmp := 1 / amp
		for i := start; i < end; i++ {
			out[i] = mdct[i] * invAmp
		}
		renormaliseVector(out[start:end], end-start, normScaling)
	}

	return out
}

func quantAllBandsMono(
	info *frameSideInfo,
	x []float32,
	totalBits int,
	state *bandEncodeState,
) []byte {
	blocks := 1
	if info.transient {
		blocks = 1 << info.lm
	}
	scale := 1 << info.lm
	frameBins := scale * int(bandEdges[maxBands])
	norm := make([]float32, frameBins)
	lowbandScratch := make([]float32, scale*int(bandEdges[maxBands]-bandEdges[maxBands-1]))
	collapseMasks := make([]byte, maxBands)
	lowbandOffset := 0
	updateLowband := true
	balance := info.allocation.balance
	for band := info.startBand; band < info.endBand; band++ {
		tell := int(state.rangeEncoder.TellFrac())
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
			}
		}
		effectiveLowband := -1
		fill := uint(0)
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
				fill |= uint(collapseMasks[fold])
			}
		} else {
			fill = (1 << blocks) - 1
		}
		var lowband []float32
		if effectiveLowband >= 0 {
			lowband = norm[effectiveLowband:]
		}
		mask := quantBandMono(
			band,
			x[bandStart:bandEnd],
			bandWidth,
			bandBits,
			info.spread,
			blocks,
			info.tfChange[band],
			lowband,
			&remainingBits,
			info.lm,
			norm[bandStart:],
			0,
			1,
			lowbandScratch,
			fill,
			state,
		)
		collapseMasks[band] = byte(mask)
		balance += info.allocation.pulses[band] + tell
		updateLowband = bandBits > bandWidth<<bitResolution
	}

	return collapseMasks
}

func quantBandMono(
	band int,
	x []float32,
	n int,
	bandBits int,
	spread int,
	blocks int,
	tfChange int,
	lowband []float32,
	remainingBits *int,
	lm int,
	lowbandOut []float32,
	level int,
	gain float32,
	lowbandScratch []float32,
	fill uint,
	state *bandEncodeState,
) uint {
	fullBand := x
	originalN := n
	nPerBlock := n / blocks
	originalBlocks := blocks
	longBlocks := blocks == 1
	timeDivide := 0
	recombine := 0
	collapseMask := uint(0)
	if n == 1 {
		sign := uint32(0)
		if x[0] < 0 {
			sign = 1
		}
		if *remainingBits >= 1<<bitResolution {
			state.rangeEncoder.EncodeRawBits(1, sign)
			*remainingBits -= 1 << bitResolution
		}
		x[0] = normScaling
		if sign != 0 {
			x[0] = -normScaling
		}
		if lowbandOut != nil {
			lowbandOut[0] = x[0]
		}

		return 1
	}
	if level == 0 {
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
	}
	if level == 0 && originalBlocks > 1 && lowband != nil {
		tmpState := bandDecodeState{tmpScratch: state.floatScratch(len(lowband))}
		deinterleaveHadamard(
			lowband,
			nPerBlock>>recombine,
			originalBlocks<<recombine,
			longBlocks,
			&tmpState,
		)
	}
	if lm != -1 && shouldSplitBand(band, lm, bandBits) && n > 2 {
		n >>= 1
		y := x[n:]
		x = x[:n]
		lm--
		if blocks == 1 {
			fill = (fill & 1) | (fill << 1)
		}
		blocks = (blocks + 1) >> 1
		pulseCap := logN400[band] + lm*(1<<bitResolution)
		qn := computeQN(n, bandBits, (pulseCap>>1)-qThetaOffset, pulseCap, false)
		tell := int(state.rangeEncoder.TellFrac())
		thetaSym := 0
		itheta := 0
		if qn != 1 {
			thetaSym = quantizeMonoSplitTheta(x, y, qn)
			encodeBandThetaMono(thetaSym, qn, blocks, state.rangeEncoder)
			itheta = thetaSym * 16384 / qn
		}
		qalloc := int(state.rangeEncoder.TellFrac()) - tell
		bandBits -= qalloc
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
		mid := float32(imid) / 32768
		side := float32(iside) / 32768
		if originalBlocks > 1 && itheta&0x3fff != 0 {
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
		if lowband != nil {
			nextLowband2 = lowband[n:]
		}
		nextLevel := level + 1
		collapseShift := originalBlocks >> 1
		rebalance := *remainingBits
		if midBits >= sideBits {
			collapseMask = quantBandMono(
				band,
				x,
				n,
				midBits,
				spread,
				blocks,
				tfChange,
				lowband,
				remainingBits,
				lm,
				nil,
				nextLevel,
				gain*mid,
				lowbandScratch,
				fill,
				state,
			)
			rebalance = midBits - (rebalance - *remainingBits)
			if rebalance > 3<<bitResolution && itheta != 0 {
				sideBits += rebalance - (3 << bitResolution)
			}
			collapseMask |= quantBandMono(
				band,
				y,
				n,
				sideBits,
				spread,
				blocks,
				tfChange,
				nextLowband2,
				remainingBits,
				lm,
				nil,
				nextLevel,
				gain*side,
				lowbandScratch,
				originalFill>>blocks,
				state,
			) << collapseShift
		} else {
			collapseMask = quantBandMono(
				band,
				y,
				n,
				sideBits,
				spread,
				blocks,
				tfChange,
				nextLowband2,
				remainingBits,
				lm,
				nil,
				nextLevel,
				gain*side,
				lowbandScratch,
				originalFill>>blocks,
				state,
			) << collapseShift
			rebalance = sideBits - (rebalance - *remainingBits)
			if rebalance > 3<<bitResolution && itheta != 16384 {
				midBits += rebalance - (3 << bitResolution)
			}
			collapseMask |= quantBandMono(
				band,
				x,
				n,
				midBits,
				spread,
				blocks,
				tfChange,
				lowband,
				remainingBits,
				lm,
				nil,
				nextLevel,
				gain*mid,
				lowbandScratch,
				fill,
				state,
			)
		}
	} else {
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
			collapseMask = algQuant(x, n, getPulses(q), spread, blocks, state.rangeEncoder, gain)
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
	if level == 0 {
		x = fullBand
		if originalBlocks > 1 {
			tmpState := bandDecodeState{tmpScratch: state.floatScratch(len(x))}
			interleaveHadamard(x, nPerBlock>>recombine, originalBlocks<<recombine, longBlocks, &tmpState)
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

func quantizeMonoSplitTheta(x []float32, y []float32, qn int) int {
	if qn <= 1 {
		return 0
	}

	var ex, ey float64
	for i := range x {
		ex += float64(x[i] * x[i])
		ey += float64(y[i] * y[i])
	}
	if ex+ey <= 1e-30 {
		return 0
	}

	theta := math.Atan2(math.Sqrt(ey), math.Sqrt(ex))
	symbol := int(math.Round(theta * float64(qn) / (0.5 * math.Pi)))

	return min(qn, max(0, symbol))
}

func encodeBandThetaMono(symbol int, qn int, blocks int, rangeEncoder *rangecoding.Encoder) {
	if blocks > 1 {
		rangeEncoder.EncodeUniform(uint32(qn+1), uint32(symbol))

		return
	}

	half := qn >> 1
	total := uint32((half + 1) * (half + 1))
	if symbol <= half {
		low := symbol * (symbol + 1) >> 1
		freq := symbol + 1
		rangeEncoder.EncodeCumulative(uint32(low), uint32(low+freq), total)

		return
	}

	freq := qn + 1 - symbol
	low := int(total) - (freq * (freq + 1) >> 1)
	rangeEncoder.EncodeCumulative(uint32(low), uint32(low+freq), total)
}
