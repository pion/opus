// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-FileCopyrightText: 2007-2008 CSIRO
// SPDX-FileCopyrightText: 2007-2010 Xiph.Org Foundation
// SPDX-FileCopyrightText: 2008 Gregory Maxwell
// SPDX-License-Identifier: MIT AND BSD-2-Clause

package celt

import "math"

// Standard floating-point CELT PLC, matching libopus 1.6.1
// 22244de5a79bd1d6d623c32e72bf1954b56235be, celt_decoder.c.
const (
	plcLPCOrder = 24
	plcPitchMax = 720
	plcPitchMin = 100
)

type plcState struct {
	history    [2][plcHistorySize + shortBlockSampleCount]float32
	lpc        [2][plcLPCOrder]float32
	background [2][maxBands]float32
	pitch      int
	periodic   bool
	skip       bool
	fold       bool
}

type plcScratch struct {
	pitch      pitchScratch
	low        [plcHistorySize / 2]float32
	excitation [combFilterMaxPeriod + plcLPCOrder]float32
	windowed   [combFilterMaxPeriod]float32
	fir        [combFilterMaxPeriod]float32
	iir        [maxFrameSampleCount + shortBlockSampleCount + plcLPCOrder]float32
	ac         [plcLPCOrder + 1]float32
	fold       [shortBlockSampleCount]float32
}

// celtPLCFIR mirrors the generic floating-point celt_fir_c accumulation order.
// libopus reverses the LPC coefficients before feeding its four-output
// correlation kernel, so every output accumulates the oldest contribution
// first. The order matters once repeated PLC reuses the generated excitation.
func celtPLCFIR(input []float32, start int, coefficients, output []float32) {
	for i := range output {
		sum := input[start+i]
		for j := len(coefficients) - 1; j >= 0; j-- {
			sum += decoderRoundedProduct(coefficients[j], input[start+i-j-1])
		}
		output[i] = sum
	}
}

func celtPLCFir5(values []float32, coefficients [5]float32) {
	var mem0, mem1, mem2, mem3, mem4 float32
	for i := range values {
		sum := values[i]
		sum += decoderRoundedProduct(coefficients[0], mem0)
		sum += decoderRoundedProduct(coefficients[1], mem1)
		sum += decoderRoundedProduct(coefficients[2], mem2)
		sum += decoderRoundedProduct(coefficients[3], mem3)
		sum += decoderRoundedProduct(coefficients[4], mem4)
		mem4, mem3, mem2, mem1, mem0 = mem3, mem2, mem1, mem0, values[i]
		values[i] = sum
	}
}

// celtPLCAutocorr keeps the generic libopus opus_val32 accumulation width.
// The encoder intentionally retains its existing higher-precision primitive;
// this decoder-specific path avoids changing encoded packets as a PLC side
// effect.
func celtPLCAutocorr(input []float32, lag int, output []float32) []float32 {
	output = output[:lag+1]
	fastLength := len(input) - lag
	celtPLCPitchXcorr(input, input, output, fastLength, lag+1)
	for k := 0; k <= lag; k++ {
		var sum float32
		for i := k + fastLength; i < len(input); i++ {
			sum += decoderRoundedProduct(input[i], input[i-k])
		}
		output[k] += sum
	}

	return output
}

func celtPLCPitchDownsample(input [][]float32, output []float32, length, factor int, scratch *pitchScratch) {
	offset := factor / 2
	for i := 1; i < length; i++ {
		value := decoderRoundedProduct(0.25, input[0][factor*i-offset])
		value += decoderRoundedProduct(0.25, input[0][factor*i+offset])
		value += decoderRoundedProduct(0.5, input[0][factor*i])
		output[i] = value
	}
	output[0] = decoderRoundedProduct(0.25, input[0][offset]) + decoderRoundedProduct(0.5, input[0][0])
	for _, channel := range input[1:] {
		for i := 1; i < length; i++ {
			value := decoderRoundedProduct(0.25, channel[factor*i-offset])
			value += decoderRoundedProduct(0.25, channel[factor*i+offset])
			value += decoderRoundedProduct(0.5, channel[factor*i])
			output[i] += value
		}
		output[0] += decoderRoundedProduct(0.25, channel[offset]) + decoderRoundedProduct(0.5, channel[0])
	}

	autocorrelation := celtPLCAutocorr(output[:length], pitchLPCOrder, scratch.autocorr[:])
	autocorrelation[0] *= 1.0001
	for i := 1; i <= pitchLPCOrder; i++ {
		factor := 0.008 * float32(i)
		correction := decoderRoundedProduct(autocorrelation[i], factor)
		correction = decoderRoundedProduct(correction, factor)
		autocorrelation[i] -= correction
	}
	coefficients := decoderCELTLPC(autocorrelation, pitchLPCOrder, scratch.lpc[:])
	attenuation := float32(1)
	for i := range pitchLPCOrder {
		attenuation *= 0.9
		coefficients[i] *= attenuation
	}
	const zero = float32(0.8)
	celtPLCFir5(output[:length], [5]float32{
		coefficients[0] + zero,
		coefficients[1] + decoderRoundedProduct(zero, coefficients[0]),
		coefficients[2] + decoderRoundedProduct(zero, coefficients[1]),
		coefficients[3] + decoderRoundedProduct(zero, coefficients[2]),
		zero * coefficients[3],
	})
}

func celtPLCPitchXcorr(input, history, correlation []float32, length, maxPitch int) {
	for lag := range maxPitch {
		var sum float32
		for i := range length {
			sum += decoderRoundedProduct(input[i], history[lag+i])
		}
		correlation[lag] = sum
	}
}

func celtPLCRefineXcorr(input, history, correlation []float32, length, maxPitch int, coarse [2]int) {
	for lag := range maxPitch >> 1 {
		correlation[lag] = 0
		if absInt(lag-2*coarse[0]) > 2 && absInt(lag-2*coarse[1]) > 2 {
			continue
		}
		var sum float32
		for i := range length >> 1 {
			sum += decoderRoundedProduct(input[i], history[lag+i])
		}
		correlation[lag] = max32(-1, sum)
	}
}

// decoderFindBestPitch intentionally keeps PLC's normalized-correlation energy
// updates in reference-ordered float32 arithmetic. The bounded pitchScratch is
// still shared; encoder search retains the higher-precision shared helper.
//
//nolint:dupl // Decoder-only arithmetic prevents contraction without changing encoded packets.
func decoderFindBestPitch(correlation, history []float32, length, maxPitch int) [2]int {
	bestPitch := [2]int{0, 1}
	bestNum := [2]float32{-1, -1}
	bestDen := [2]float32{0, 0}

	historyEnergy := float32(1)
	for j := range length {
		historyEnergy += decoderRoundedProduct(history[j], history[j])
	}
	for i := range maxPitch {
		if correlation[i] > 0 {
			// Scaling before squaring avoids both underflow and overflow.
			scaled := correlation[i] * 1e-12
			numerator := scaled * scaled
			if numerator*bestDen[1] > bestNum[1]*historyEnergy {
				if numerator*bestDen[0] > bestNum[0]*historyEnergy {
					bestNum[1], bestDen[1], bestPitch[1] = bestNum[0], bestDen[0], bestPitch[0]
					bestNum[0], bestDen[0], bestPitch[0] = numerator, historyEnergy, i
				} else {
					bestNum[1], bestDen[1], bestPitch[1] = numerator, historyEnergy, i
				}
			}
		}
		entering := decoderRoundedProduct(history[i+length], history[i+length])
		leaving := decoderRoundedProduct(history[i], history[i])
		historyEnergy += entering - leaving
		historyEnergy = max32(1, historyEnergy)
	}

	return bestPitch
}

//nolint:dupl // Decoder PLC intentionally mirrors pitchSearch with float32-only arithmetic.
func celtPLCPitchSearch(input, history []float32, length, maxPitch int, scratch *pitchScratch) int {
	lag := length + maxPitch
	input4 := scratch.pitchX[:length>>2]
	history4 := scratch.pitchY[:lag>>2]
	correlation := scratch.pitchXC[:maxPitch>>1]
	clear(correlation)
	for i := range input4 {
		input4[i] = input[2*i]
	}
	for i := range history4 {
		history4[i] = history[2*i]
	}
	celtPLCPitchXcorr(input4, history4, correlation, length>>2, maxPitch>>2)
	best := decoderFindBestPitch(correlation, history4, length>>2, maxPitch>>2)
	celtPLCRefineXcorr(input, history, correlation, length, maxPitch, best)
	best = decoderFindBestPitch(correlation, history, length>>1, maxPitch>>1)

	offset := 0
	if best[0] > 0 && best[0] < (maxPitch>>1)-1 {
		a, b, c := correlation[best[0]-1], correlation[best[0]], correlation[best[0]+1]
		switch {
		case c-a > 0.7*(b-a):
			offset = 1
		case a-c > 0.7*(b-c):
			offset = -1
		}
	}

	return 2*best[0] - offset
}

// celtPLCIIR mirrors the generic non-SMALL_FOOTPRINT celt_iir four-sample
// kernel, including the reference's accumulation and within-block patch order.
// values aliases the input and output just as it does in celt_decode_lost.
//
//nolint:varnamelen // y is the conventional libopus IIR scratch/history vector.
func celtPLCIIR(values, history, coefficients []float32, scratch []float32) {
	order := len(coefficients)
	y := scratch[:len(values)+order]
	for i := range order {
		y[i] = -history[len(history)-order+i]
	}
	clear(y[order:])

	i := 0
	for ; i+3 < len(values); i += 4 {
		sums := [4]float32{values[i], values[i+1], values[i+2], values[i+3]}
		for j := range order {
			coefficient := coefficients[order-1-j]
			sums[0] += decoderRoundedProduct(coefficient, y[i+j])
			sums[1] += decoderRoundedProduct(coefficient, y[i+j+1])
			sums[2] += decoderRoundedProduct(coefficient, y[i+j+2])
			sums[3] += decoderRoundedProduct(coefficient, y[i+j+3])
		}
		y[i+order] = -sums[0]
		values[i] = sums[0]
		sums[1] += decoderRoundedProduct(y[i+order], coefficients[0])
		y[i+order+1] = -sums[1]
		values[i+1] = sums[1]
		sums[2] += decoderRoundedProduct(y[i+order+1], coefficients[0])
		sums[2] += decoderRoundedProduct(y[i+order], coefficients[1])
		y[i+order+2] = -sums[2]
		values[i+2] = sums[2]
		sums[3] += decoderRoundedProduct(y[i+order+2], coefficients[0])
		sums[3] += decoderRoundedProduct(y[i+order+1], coefficients[1])
		sums[3] += decoderRoundedProduct(y[i+order], coefficients[2])
		y[i+order+3] = -sums[3]
		values[i+3] = sums[3]
	}
	// PLC frame plus overlap lengths are multiples of four. Keep the generic
	// reference tail for completeness if that invariant changes.
	for ; i < len(values); i++ {
		sum := values[i]
		for j := range order {
			sum -= decoderRoundedProduct(coefficients[order-1-j], y[i+j])
		}
		y[i+order] = sum
		values[i] = sum
	}
}

func (d *Decoder) rememberPLC(samples []float32, channel int) {
	mem := d.plc.history[channel][:]
	copy(mem, mem[len(samples):plcHistorySize])
	copy(mem[plcHistorySize-len(samples):plcHistorySize], samples)
}

func (d *Decoder) finishPLCRecovery(info *frameSideInfo) {
	increase := decoderRoundedProduct(float32(min(160, d.lossDuration+(1<<info.lm))), 0.001)
	for channel := range d.plc.background {
		for band := range maxBands {
			d.plc.background[channel][band] = min(d.plc.background[channel][band]+increase, d.previousLogE[channel][band])
		}
	}
	d.lossDuration = 0
	d.plc.periodic = false
	d.plc.fold = false
}

//nolint:cyclop,gocognit // Keep excitation, synthesis and energy checks in reference order.
func (d *Decoder) decodePeriodicPLC(info *frameSideInfo, out []float32) {
	n := infoFrameSampleCount(info)
	scratch := &d.scratchBuffer().plc
	if !d.plc.periodic {
		channels := [2][]float32{d.plc.history[0][:plcHistorySize], d.plc.history[1][:plcHistorySize]}
		celtPLCPitchDownsample(channels[:info.outputChannelCount], scratch.low[:], plcHistorySize/2, 2, &scratch.pitch)
		d.plc.pitch = plcPitchMax - celtPLCPitchSearch(scratch.low[plcPitchMax/2:], scratch.low[:],
			plcHistorySize-plcPitchMax, plcPitchMax-plcPitchMin, &scratch.pitch)
	}
	pitch := d.plc.pitch
	length := min(2*pitch, combFilterMaxPeriod)
	fade := float32(1)
	if d.plc.periodic {
		fade = 0.8
	}
	for channel := range info.outputChannelCount {
		mem := d.plc.history[channel][:]
		exc := scratch.excitation[:]
		copy(exc, mem[plcHistorySize-combFilterMaxPeriod-plcLPCOrder:plcHistorySize])
		if !d.plc.periodic {
			copy(scratch.windowed[:], exc[plcLPCOrder:])
			for i := range shortBlockSampleCount {
				scratch.windowed[i] *= celtWindow120[i]
				scratch.windowed[combFilterMaxPeriod-1-i] *= celtWindow120[i]
			}
			ac := celtPLCAutocorr(scratch.windowed[:], plcLPCOrder, scratch.ac[:])
			ac[0] *= 1.0001
			lagWindow := float32(0.008)
			lagWindow *= lagWindow
			for i := 1; i <= plcLPCOrder; i++ {
				correction := decoderRoundedProduct(ac[i], lagWindow)
				correction = decoderRoundedProduct(correction, float32(i))
				correction = decoderRoundedProduct(correction, float32(i))
				ac[i] -= correction
			}
			decoderCELTLPC(ac, plcLPCOrder, d.plc.lpc[channel][:])
		}
		coeff := d.plc.lpc[channel][:]
		start := plcLPCOrder + combFilterMaxPeriod - length
		celtPLCFIR(exc, start, coeff, scratch.fir[:length])
		copy(exc[start:], scratch.fir[:length])
		e1, e2 := float32(1), float32(1)
		for i := 0; i < length/2; i++ {
			x := exc[plcLPCOrder+combFilterMaxPeriod-length/2+i]
			y := exc[plcLPCOrder+combFilterMaxPeriod-length+i]
			e1 += decoderRoundedProduct(x, x)
			e2 += decoderRoundedProduct(y, y)
		}
		decay := float32(math.Sqrt(float64(min(e1, e2) / e2)))
		copy(mem, mem[n:plcHistorySize])
		base := plcHistorySize - n
		attenuation := fade * decay
		var sourceEnergy float32
		for i, j := 0, 0; i < n+shortBlockSampleCount; i, j = i+1, j+1 {
			if j >= pitch {
				j -= pitch
				attenuation *= decay
			}
			mem[base+i] = attenuation * exc[plcLPCOrder+combFilterMaxPeriod-pitch+j]
			v := mem[base-pitch+j]
			sourceEnergy += decoderRoundedProduct(v, v)
		}
		// In-place synthesis uses the pre-loss signal as its filter memory.
		celtPLCIIR(mem[base:base+n+shortBlockSampleCount], mem[:base], coeff, scratch.iir[:])
		var synthesizedEnergy float32
		for _, v := range mem[base : base+n+shortBlockSampleCount] {
			synthesizedEnergy += decoderRoundedProduct(v, v)
		}
		if !(sourceEnergy > 0.2*synthesizedEnergy) {
			clear(mem[base : base+n+shortBlockSampleCount])
		} else if sourceEnergy < synthesizedEnergy {
			ratio := float32(math.Sqrt(float64((sourceEnergy + 1) / (synthesizedEnergy + 1))))
			for i := 0; i < n+shortBlockSampleCount; i++ {
				gain := ratio
				if i < shortBlockSampleCount {
					gain = 1 - decoderRoundedProduct(celtWindow120[i], 1-ratio)
				}
				mem[base+i] *= gain
			}
		}
		// This signal is already postfiltered. Only synchronize the history;
		// the next MDCT frame will undo that filter on its overlap first.
		copy(d.postfilterMem[channel], mem[plcHistorySize-postfilterHistorySampleCount:plcHistorySize])
	}
	x := d.plc.history[0][plcHistorySize-n : plcHistorySize]
	var y []float32
	if info.outputChannelCount == 2 {
		y = d.plc.history[1][plcHistorySize-n : plcHistorySize]
	}
	d.deemphasisAndInterleave(x, y, out, n, info.outputChannelCount, info.outputSampleRate)
	d.plc.periodic = true
	d.plc.fold = true
}

// foldPLCOverlap translates libopus's folded half-window into this decoder's
// weighted overlap-add representation. libopus keeps an unwindowed decode_mem
// tail and applies opposite window halves in the next MDCT; overlap stores the
// already-folded value, so both contributions are combined here. The unused
// second half is intentionally left clear. It runs only on leaving periodic
// synthesis.
func (d *Decoder) foldPLCOverlap(channel int) {
	mem := d.plc.history[channel][:]
	tmp := d.scratchBuffer().plc.fold[:]
	period := max(combFilterMinPeriod, d.postfilter.period)
	gains := [3][3]float32{
		{0.306640625, 0.2170410156, 0.1296386719},
		{0.4638671875, 0.2680664062, 0},
		{0.7998046875, 0.1000976562, 0},
	}
	g := gains[d.postfilter.tapset]
	g10 := -d.postfilter.gain * g[0]
	g11 := -d.postfilter.gain * g[1]
	g12 := -d.postfilter.gain * g[2]
	for i := range shortBlockSampleCount {
		base := plcHistorySize + i
		value := mem[base]
		value += decoderRoundedProduct(g10, mem[base-period])
		value += decoderRoundedProduct(g11, mem[base-period+1]+mem[base-period-1])
		value += decoderRoundedProduct(g12, mem[base-period+2]+mem[base-period-2])
		tmp[i] = value
	}
	for i := range shortBlockSampleCount / 2 {
		j := shortBlockSampleCount - 1 - i
		folded := decoderRoundedProduct(celtWindow120[i], tmp[j]) +
			decoderRoundedProduct(celtWindow120[j], tmp[i])
		d.overlap[channel][i] = folded
	}
}
