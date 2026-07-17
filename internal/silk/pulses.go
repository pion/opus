// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

const (
	shellCodecFrameLength     = 16
	log2ShellCodecFrameLength = 4
	silkMaxPulses             = 16
	nRateLevels               = 10
)

// silkMaxPulsesTable bounds the pulse sum per partition size (2, 4, 8, 16)
// before the block is downscaled by an extra LSB (gain_quant.c thresholds).
var silkMaxPulsesTable = [4]int32{8, 10, 12, 16} //nolint:gochecknoglobals // per-partition pulse-sum bounds.

// silkRateLevelsBITSQ5[signalType>>1] and silkPulsesPerBlockBITSQ5 give the Q5
// bit cost of each rate level, used to pick the cheapest one.
var silkRateLevelsBITSQ5 = [2][9]int32{ //nolint:gochecknoglobals // Q5 rate-level bit costs.
	{131, 74, 141, 79, 80, 138, 95, 104, 134},
	{95, 99, 91, 125, 93, 76, 123, 115, 123},
}

var silkPulsesPerBlockBITSQ5 = [9][18]int32{ //nolint:gochecknoglobals // Q5 pulses-per-block bit costs.
	{31, 57, 107, 160, 205, 205, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
	{69, 47, 67, 111, 166, 205, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
	{82, 74, 79, 95, 109, 128, 145, 160, 173, 205, 205, 205, 224, 255, 255, 224, 255, 224},
	{125, 74, 59, 69, 97, 141, 182, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
	{173, 115, 85, 73, 76, 92, 115, 145, 173, 205, 224, 224, 255, 255, 255, 255, 255, 255},
	{166, 134, 113, 102, 101, 102, 107, 118, 125, 138, 145, 155, 166, 182, 192, 192, 205, 150},
	{224, 182, 134, 101, 83, 79, 85, 97, 120, 145, 173, 205, 224, 255, 255, 255, 255, 255},
	{255, 224, 192, 150, 120, 101, 92, 89, 93, 102, 118, 134, 160, 182, 192, 224, 224, 224},
	{255, 224, 224, 182, 155, 134, 118, 109, 104, 102, 106, 111, 118, 131, 145, 160, 173, 131},
}

// silkSignICDF holds the excitation sign thresholds, indexed by
// 7*(quantOffsetType + 2*signalType) + min(pulseCount, 6). The decoder's named
// sign tables are the same values in {256, 256-threshold, 256} form.
var silkSignICDF = [42]int32{ //nolint:gochecknoglobals // excitation sign thresholds.
	254, 49, 67, 77, 82, 93, 99,
	198, 11, 18, 24, 31, 36, 45,
	255, 46, 66, 78, 87, 94, 104,
	208, 14, 21, 32, 42, 51, 66,
	255, 94, 104, 109, 112, 115, 118,
	248, 53, 69, 80, 88, 95, 102,
}

// encodePulses range-encodes the excitation quantization indices, inverting the
// decoder's excitation path (RFC 6716 Section 4.2.7.8). pulses holds one signed
// index per sample; frameLength is padded up to a whole number of 16-sample
// shell blocks.
//
//nolint:gocognit,gocyclo,cyclop,maintidx // faithful port of silk_encode_pulses.
func (e *Encoder) encodePulses(
	signalType frameSignalType,
	quantOffsetType frameQuantizationOffsetType,
	pulses []int8,
	frameLength int,
) {
	iter := frameLength >> log2ShellCodecFrameLength
	if iter*shellCodecFrameLength < frameLength {
		iter++
	}

	absPulses := make([]int32, iter*shellCodecFrameLength)
	for i := range absPulses {
		if i < len(pulses) {
			absPulses[i] = absInt32(int32(pulses[i]))
		}
	}

	// Sum the pulses per block, downscaling by an LSB whenever any partition
	// exceeds its silkMaxPulsesTable limit.
	sumPulses := make([]int32, iter)
	nRshifts := make([]int32, iter)
	var comb [8]int32
	for i := range iter {
		block := absPulses[i*shellCodecFrameLength : (i+1)*shellCodecFrameLength]
		for {
			scaleDown := combineAndCheck(comb[:8], block, silkMaxPulsesTable[0], 8)
			scaleDown += combineAndCheck(comb[:4], comb[:8], silkMaxPulsesTable[1], 4)
			scaleDown += combineAndCheck(comb[:2], comb[:4], silkMaxPulsesTable[2], 2)
			scaleDown += combineAndCheck(sumPulses[i:i+1], comb[:2], silkMaxPulsesTable[3], 1)
			if scaleDown == 0 {
				break
			}
			nRshifts[i]++
			for k := range block {
				block[k] >>= 1
			}
		}
	}

	// Pick the rate level with the fewest bits for the pulse-count symbols.
	sigTypeIndex := 0
	if signalType == frameSignalTypeVoiced {
		sigTypeIndex = 1
	}
	minSumBits := int32(math.MaxInt32)
	rateLevel := 0
	for k := range nRateLevels - 1 {
		sumBits := silkRateLevelsBITSQ5[sigTypeIndex][k]
		for i := range iter {
			if nRshifts[i] > 0 {
				sumBits += silkPulsesPerBlockBITSQ5[k][silkMaxPulses+1]
			} else {
				sumBits += silkPulsesPerBlockBITSQ5[k][sumPulses[i]]
			}
		}
		if sumBits < minSumBits {
			minSumBits = sumBits
			rateLevel = k
		}
	}
	if signalType == frameSignalTypeVoiced {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfRateLevelVoiced, uint32(rateLevel)) //nolint:gosec // G115
	} else {
		e.rangeEncoder.EncodeSymbolWithICDF(icdfRateLevelUnvoiced, uint32(rateLevel)) //nolint:gosec // G115
	}

	// Pulse counts, with the value 17 escaping to extra LSBs.
	for i := range iter {
		if nRshifts[i] == 0 {
			e.rangeEncoder.EncodeSymbolWithICDF(icdfPulseCount[rateLevel], uint32(sumPulses[i])) //nolint:gosec // G115
		} else {
			e.rangeEncoder.EncodeSymbolWithICDF(icdfPulseCount[rateLevel], silkMaxPulses+1)
			for range nRshifts[i] - 1 {
				e.rangeEncoder.EncodeSymbolWithICDF(icdfPulseCount[nRateLevels-1], silkMaxPulses+1)
			}
			e.rangeEncoder.EncodeSymbolWithICDF(icdfPulseCount[nRateLevels-1], uint32(sumPulses[i])) //nolint:gosec // G115
		}
	}

	// Pulse locations (shell code) for blocks that have pulses.
	for i := range iter {
		if sumPulses[i] > 0 {
			e.encodePulseLocation(absPulses[i*shellCodecFrameLength:(i+1)*shellCodecFrameLength], sumPulses[i])
		}
	}

	// LSBs shifted out during downscaling, most significant first.
	for i := range iter {
		if nRshifts[i] == 0 {
			continue
		}
		nLS := nRshifts[i] - 1
		for k := range shellCodecFrameLength {
			// LSBs come from the original magnitude, not the downscaled block.
			absQ := int32(0)
			if idx := i*shellCodecFrameLength + k; idx < len(pulses) {
				absQ = absInt32(int32(pulses[idx]))
			}
			for j := nLS; j > 0; j-- {
				e.rangeEncoder.EncodeSymbolWithICDF(icdfExcitationLSB, uint32((absQ>>j)&1)) //nolint:gosec // G115
			}
			e.rangeEncoder.EncodeSymbolWithICDF(icdfExcitationLSB, uint32(absQ&1)) //nolint:gosec // G115
		}
	}

	e.encodeExcitationSigns(pulses, frameLength, signalType, quantOffsetType, sumPulses)
}

// combineAndCheck sums adjacent pairs of in into out and reports whether any
// sum exceeds maxPulses (in which case the block must be downscaled).
func combineAndCheck(out, in []int32, maxPulses int32, length int) int32 {
	scaleDown := int32(0)
	for k := range length {
		sum := in[2*k] + in[2*k+1]
		if sum > maxPulses {
			scaleDown = 1
		}
		out[k] = sum
	}

	return scaleDown
}

// encodePulseLocation is the shell encoder: it recursively splits a 16-sample
// block and codes the left-child pulse count at each split, inverting
// Decoder.decodePulseLocation.
func (e *Encoder) encodePulseLocation(block []int32, total int32) {
	var s2 [8]int32
	for t := range 8 {
		s2[t] = block[2*t] + block[2*t+1]
	}
	var s4 [4]int32
	for u := range 4 {
		s4[u] = s2[2*u] + s2[2*u+1]
	}
	var s8 [2]int32
	for v := range 2 {
		s8[v] = s4[2*v] + s4[2*v+1]
	}

	e.encodeSplit(icdfPulseCountSplit16SamplePartitions, total, s8[0])
	for j := range 2 {
		e.encodeSplit(icdfPulseCountSplit8SamplePartitions, s8[j], s4[2*j])
		for k := range 2 {
			idx4 := 2*j + k
			e.encodeSplit(icdfPulseCountSplit4SamplePartitions, s4[idx4], s2[2*idx4])
			for l := range 2 {
				idx2 := 2*idx4 + l
				e.encodeSplit(icdfPulseCountSplit2SamplePartitions, s2[idx2], block[2*idx2])
			}
		}
	}
}

// encodeSplit codes how many of a partition's pulses fall in its first half.
func (e *Encoder) encodeSplit(icdf [][]uint, block, leftChild int32) {
	if block > 0 {
		e.rangeEncoder.EncodeSymbolWithICDF(icdf[block-1], uint32(leftChild)) //nolint:gosec // G115
	}
}

// encodeExcitationSigns codes a sign for every non-zero pulse, using the PDF
// selected by signal type, quantization offset type and block pulse count.
func (e *Encoder) encodeExcitationSigns(
	pulses []int8,
	frameLength int,
	signalType frameSignalType,
	quantOffsetType frameQuantizationOffsetType,
	sumPulses []int32,
) {
	blocks := (frameLength + shellCodecFrameLength/2) >> log2ShellCodecFrameLength
	offsetIndex := int(quantOffsetType) - int(frameQuantizationOffsetTypeLow)
	signalIndex := int(signalType) - int(frameSignalTypeInactive)
	base := 7 * (offsetIndex + 2*signalIndex)
	for i := range blocks {
		p := sumPulses[i]
		if p <= 0 {
			continue
		}
		count := int(p)
		count = min(count, 6)
		icdf := []uint{256, uint(256 - silkSignICDF[base+count]), 256} //nolint:gosec // G115
		for j := range shellCodecFrameLength {
			idx := i*shellCodecFrameLength + j
			if idx >= len(pulses) || pulses[idx] == 0 {
				continue
			}
			sym := uint32(0)
			if pulses[idx] > 0 {
				sym = 1
			}
			e.rangeEncoder.EncodeSymbolWithICDF(icdf, sym)
		}
	}
}
