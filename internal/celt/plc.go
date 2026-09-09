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
	ac         [plcLPCOrder + 1]float32
	fold       [shortBlockSampleCount]float32
}

func (d *Decoder) rememberPLC(samples []float32, channel int) {
	mem := d.plc.history[channel][:]
	copy(mem, mem[len(samples):plcHistorySize])
	copy(mem[plcHistorySize-len(samples):plcHistorySize], samples)
}

func (d *Decoder) finishPLCRecovery(info *frameSideInfo) {
	increase := float32(min(160, d.lossDuration+(1<<info.lm))) * 0.001
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
		pitchDownsample(channels[:info.outputChannelCount], scratch.low[:], plcHistorySize/2, 2, &scratch.pitch)
		d.plc.pitch = plcPitchMax - pitchSearch(scratch.low[plcPitchMax/2:], scratch.low[:],
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
			ac := celtAutocorr(scratch.windowed[:], plcLPCOrder, scratch.ac[:])
			ac[0] *= 1.0001
			for i := 1; i <= plcLPCOrder; i++ {
				ac[i] -= ac[i] * (0.008 * 0.008) * float32(i*i)
			}
			celtLPC(ac, plcLPCOrder, d.plc.lpc[channel][:])
		}
		coeff := d.plc.lpc[channel][:]
		start := plcLPCOrder + combFilterMaxPeriod - length
		for i := range length {
			v := exc[start+i]
			for j, a := range coeff {
				v += a * exc[start+i-j-1]
			}
			scratch.fir[i] = v
		}
		copy(exc[start:], scratch.fir[:length])
		e1, e2 := float32(1), float32(1)
		for i := 0; i < length/2; i++ {
			x := exc[plcLPCOrder+combFilterMaxPeriod-length/2+i]
			y := exc[plcLPCOrder+combFilterMaxPeriod-length+i]
			e1 += x * x
			e2 += y * y
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
			sourceEnergy += v * v
		}
		// In-place synthesis uses the pre-loss signal as its filter memory.
		for i := 0; i < n+shortBlockSampleCount; i++ {
			v := mem[base+i]
			for j, a := range coeff {
				v -= a * mem[base+i-j-1]
			}
			mem[base+i] = v
		}
		var synthesizedEnergy float32
		for _, v := range mem[base : base+n+shortBlockSampleCount] {
			synthesizedEnergy += v * v
		}
		if !(sourceEnergy > 0.2*synthesizedEnergy) {
			clear(mem[base : base+n+shortBlockSampleCount])
		} else if sourceEnergy < synthesizedEnergy {
			ratio := float32(math.Sqrt(float64((sourceEnergy + 1) / (synthesizedEnergy + 1))))
			for i := 0; i < n+shortBlockSampleCount; i++ {
				gain := ratio
				if i < shortBlockSampleCount {
					gain = 1 - celtWindow120[i]*(1-ratio)
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
	for i := range shortBlockSampleCount {
		base := plcHistorySize + i
		tmp[i] = mem[base] - d.postfilter.gain*(g[0]*mem[base-period]+
			g[1]*(mem[base-period-1]+mem[base-period+1])+g[2]*(mem[base-period-2]+mem[base-period+2]))
	}
	for i := range shortBlockSampleCount / 2 {
		j := shortBlockSampleCount - 1 - i
		folded := celtWindow120[i]*tmp[j] + celtWindow120[j]*tmp[i]
		d.overlap[channel][i] = celtWindow120[j] * folded
		d.overlap[channel][j] = celtWindow120[i] * folded
	}
}
