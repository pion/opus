// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:cyclop,gosec,lll,modernize // Synthesis follows the RFC/reference filter and anti-collapse structure.
package celt

import "math"

const (
	combFilterMinPeriod          = 15
	combFilterMaxPeriod          = 1024
	postfilterHistoryPad         = 2
	postfilterHistorySampleCount = combFilterMaxPeriod + postfilterHistoryPad
)

type postFilterState struct {
	period    int
	oldPeriod int
	gain      float32
	oldGain   float32
	tapset    int
	oldTapset int
}

type complex32 struct {
	r float32
	i float32
}

var energyMeans = [maxBands]float32{ //nolint:gochecknoglobals
	6.437500, 6.250000, 5.750000, 5.312500, 5.062500,
	4.812500, 4.500000, 4.375000, 4.875000, 4.687500,
	4.562500, 4.437500, 4.875000, 4.625000, 4.312500,
	4.500000, 4.375000, 4.625000, 4.750000, 4.437500,
	3.750000,
}

var celtWindow120 = [shortBlockSampleCount]float32{ //nolint:gochecknoglobals
	6.7286966e-05, 0.00060551348, 0.0016815970, 0.0032947962, 0.0054439943,
	0.0081276923, 0.011344001, 0.015090633, 0.019364886, 0.024163635,
	0.029483315, 0.035319905, 0.041668911, 0.048525347, 0.055883718,
	0.063737999, 0.072081616, 0.080907428, 0.090207705, 0.099974111,
	0.11019769, 0.12086883, 0.13197729, 0.14351214, 0.15546177,
	0.16781389, 0.18055550, 0.19367290, 0.20715171, 0.22097682,
	0.23513243, 0.24960208, 0.26436860, 0.27941419, 0.29472040,
	0.31026818, 0.32603788, 0.34200931, 0.35816177, 0.37447407,
	0.39092462, 0.40749142, 0.42415215, 0.44088423, 0.45766484,
	0.47447104, 0.49127978, 0.50806798, 0.52481261, 0.54149077,
	0.55807973, 0.57455701, 0.59090049, 0.60708841, 0.62309951,
	0.63891306, 0.65450896, 0.66986776, 0.68497077, 0.69980010,
	0.71433873, 0.72857055, 0.74248043, 0.75605424, 0.76927895,
	0.78214257, 0.79463430, 0.80674445, 0.81846456, 0.82978733,
	0.84070669, 0.85121779, 0.86131698, 0.87100183, 0.88027111,
	0.88912479, 0.89756398, 0.90559094, 0.91320904, 0.92042270,
	0.92723738, 0.93365955, 0.93969656, 0.94535671, 0.95064907,
	0.95558353, 0.96017067, 0.96442171, 0.96834849, 0.97196334,
	0.97527906, 0.97830883, 0.98106616, 0.98356480, 0.98581869,
	0.98784191, 0.98964856, 0.99125274, 0.99266849, 0.99390969,
	0.99499004, 0.99592297, 0.99672162, 0.99739874, 0.99796667,
	0.99843728, 0.99882195, 0.99913147, 0.99937606, 0.99956527,
	0.99970802, 0.99981248, 0.99988613, 0.99993565, 0.99996697,
	0.99998518, 0.99999457, 0.99999859, 0.99999982, 1.0000000,
}

// SmoothFade applies the RFC 6716 CELT transition window over one 2.5 ms
// overlap. The decoder mixes in 48 kHz CELT time, so the reference window
// increment is always one sample here.
func SmoothFade(in1, in2, out []float32, overlap int, channels int) {
	for channel := range channels {
		for i := range overlap {
			w := celtWindow120[i] * celtWindow120[i]
			index := i*channels + channel
			out[index] = w*in2[index] + (1-w)*in1[index]
		}
	}
}

func (d *Decoder) log2Amp(info *frameSideInfo) [2][maxBands]float32 {
	energy := [2][maxBands]float32{}
	for channel := range info.channelCount {
		for band := info.startBand; band < info.endBand; band++ {
			lg := minFloat32(32, d.previousLogE[channel][band]+energyMeans[band])
			energy[channel][band] = float32(math.Pow(2, float64(lg)))
		}
	}

	return energy
}

// denormaliseAndSynthesize follows RFC 6716 Sections 4.3.6 and 4.3.7:
// recover band amplitudes, convert to time-domain PCM, then apply the
// post-filter and de-emphasis stages.
func (d *Decoder) denormaliseAndSynthesize(
	info *frameSideInfo,
	x []float32,
	y []float32,
	bandEnergy [2][maxBands]float32,
	out []float32,
) {
	frameSampleCount := len(x)
	freqX := make([]float32, frameSampleCount)
	denormaliseBands(info, x, freqX, bandEnergy[0])
	var freqY []float32
	if info.channelCount == 2 {
		freqY = make([]float32, frameSampleCount)
		denormaliseBands(info, y, freqY, bandEnergy[1])
	}
	if info.outputChannelCount == 2 && info.channelCount == 1 {
		freqY = make([]float32, frameSampleCount)
		copy(freqY, freqX)
	}
	if info.outputChannelCount == 1 && info.channelCount == 2 {
		for i := range frameSampleCount {
			freqX[i] = 0.5 * (freqX[i] + freqY[i])
		}
		freqY = nil
	}

	timeX := d.inverseTransformChannel(freqX, 0, info)
	d.applyPostfilter(info, timeX, 0)
	if info.outputChannelCount == 1 {
		d.updatePostfilterState(info)
		d.deemphasisAndInterleave(timeX, nil, out, frameSampleCount, 1)

		return
	}
	timeY := d.inverseTransformChannel(freqY, 1, info)
	d.applyPostfilter(info, timeY, 1)
	d.updatePostfilterState(info)
	d.deemphasisAndInterleave(timeX, timeY, out, frameSampleCount, 2)
}

// antiCollapse implements RFC 6716 Section 4.3.5 by injecting low-energy
// noise into transient short blocks that received no PVQ pulses.
func (d *Decoder) antiCollapse(info *frameSideInfo, x []float32, y []float32, collapseMasks []byte, seed uint32) {
	channels := [][]float32{x}
	if info.channelCount == 2 {
		channels = append(channels, y)
	}
	for band := info.startBand; band < info.endBand; band++ {
		n0 := int(bandEdges[band+1] - bandEdges[band])
		n := n0 << info.lm
		depth := (1 + info.allocation.pulses[band]) / n
		threshold := 0.5 * math.Pow(2, -0.125*float64(depth))
		sqrtInv := 1 / math.Sqrt(float64(n))
		for channel, spectrum := range channels {
			prev1 := d.previousLogE1[channel][band]
			prev2 := d.previousLogE2[channel][band]
			if info.channelCount == 1 {
				prev1 = max(prev1, d.previousLogE1[1][band])
				prev2 = max(prev2, d.previousLogE2[1][band])
			}
			energyDiff := max(float32(0), d.previousLogE[channel][band]-minFloat32(prev1, prev2))
			radius := 2 * math.Pow(2, -float64(energyDiff))
			if info.lm == maxLM {
				radius *= math.Sqrt2
			}
			radius = math.Min(threshold, radius) * sqrtInv
			bandStart := int(bandEdges[band]) << info.lm
			mask := collapseMasks[band*info.channelCount+channel]
			renormalize := false
			for block := 0; block < 1<<info.lm; block++ {
				if mask&(1<<block) != 0 {
					continue
				}
				for j := range n0 {
					seed = lcgRand(seed)
					value := float32(radius)
					if seed&0x8000 == 0 {
						value = -value
					}
					spectrum[bandStart+(j<<info.lm)+block] = value
				}
				renormalize = true
			}
			if renormalize {
				renormaliseVector(spectrum[bandStart:], n, normScaling)
			}
		}
	}
}

func (d *Decoder) updateLogEHistory(info *frameSideInfo) {
	// RFC 6716 Section 4.3.5 uses the two previous log-energy frames when it
	// sizes anti-collapse noise for later transient frames.
	if info.channelCount == 1 {
		copy(d.previousLogE[1][:], d.previousLogE[0][:])
	}
	if !info.transient {
		for channel := range d.previousLogE {
			copy(d.previousLogE2[channel][:], d.previousLogE1[channel][:])
			copy(d.previousLogE1[channel][:], d.previousLogE[channel][:])
		}

		return
	}
	for channel := range d.previousLogE {
		for band := range d.previousLogE[channel] {
			d.previousLogE1[channel][band] = minFloat32(d.previousLogE1[channel][band], d.previousLogE[channel][band])
		}
	}
}

func (d *Decoder) resetInactiveBandState(info *frameSideInfo) {
	// Start/end bands can change across CELT configurations, so history outside
	// the coded range must not influence later prediction or anti-collapse work.
	for channel := range d.previousLogE {
		for band := 0; band < info.startBand; band++ {
			d.previousLogE[channel][band] = 0
			d.previousLogE1[channel][band] = -28
			d.previousLogE2[channel][band] = -28
		}
		for band := info.endBand; band < maxBands; band++ {
			d.previousLogE[channel][band] = 0
			d.previousLogE1[channel][band] = -28
			d.previousLogE2[channel][band] = -28
		}
	}
}

// applyPostfilter implements the two-stage transition described by RFC 6716
// Section 4.3.7.1: the overlap transitions from the previous frame state,
// then the remaining samples transition to this frame's decoded state.
func (d *Decoder) applyPostfilter(info *frameSideInfo, time []float32, channel int) {
	if cap(d.postfilterMem[channel]) < postfilterHistorySampleCount {
		d.postfilterMem[channel] = make([]float32, postfilterHistorySampleCount)
	}
	mem := d.postfilterMem[channel][:postfilterHistorySampleCount]
	buf := make([]float32, postfilterHistorySampleCount+len(time))
	copy(buf, mem)
	copy(buf[postfilterHistorySampleCount:], time)

	period := max(d.postfilter.period, combFilterMinPeriod)
	oldPeriod := max(d.postfilter.oldPeriod, combFilterMinPeriod)
	combFilter(
		buf,
		postfilterHistorySampleCount,
		oldPeriod,
		period,
		min(shortBlockSampleCount, len(time)),
		d.postfilter.oldGain,
		d.postfilter.gain,
		d.postfilter.oldTapset,
		d.postfilter.tapset,
	)
	if info.lm != 0 && len(time) > shortBlockSampleCount {
		current := currentPostfilter(info)
		combFilter(
			buf,
			postfilterHistorySampleCount+shortBlockSampleCount,
			period,
			max(current.period, combFilterMinPeriod),
			len(time)-shortBlockSampleCount,
			d.postfilter.gain,
			current.gain,
			d.postfilter.tapset,
			current.tapset,
		)
	}
	copy(time, buf[postfilterHistorySampleCount:postfilterHistorySampleCount+len(time)])
	copy(mem, buf[len(time):len(time)+postfilterHistorySampleCount])
}

func (d *Decoder) updatePostfilterState(info *frameSideInfo) {
	current := currentPostfilter(info)
	d.postfilter.oldPeriod = d.postfilter.period
	d.postfilter.oldGain = d.postfilter.gain
	d.postfilter.oldTapset = d.postfilter.tapset
	d.postfilter.period = current.period
	d.postfilter.gain = current.gain
	d.postfilter.tapset = current.tapset
	if info.lm != 0 {
		d.postfilter.oldPeriod = d.postfilter.period
		d.postfilter.oldGain = d.postfilter.gain
		d.postfilter.oldTapset = d.postfilter.tapset
	}
}

func currentPostfilter(info *frameSideInfo) postFilterState {
	if !info.postFilter.enabled {
		return postFilterState{}
	}

	return postFilterState{
		period: info.postFilter.period,
		gain:   info.postFilter.gain,
		tapset: info.postFilter.tapset,
	}
}

// combFilter applies the RFC 6716 Section 4.3.7.1 pitch post-filter taps,
// cross-fading from the previous filter state over the overlap window.
func combFilter(buf []float32, start int, period0 int, period1 int, n int, gain0 float32, gain1 float32, tapset0 int, tapset1 int) {
	gains := [3][3]float32{
		{0.3066406250, 0.2170410156, 0.1296386719},
		{0.4638671875, 0.2680664062, 0},
		{0.7998046875, 0.1000976562, 0},
	}
	g00 := gain0 * gains[tapset0][0]
	g01 := gain0 * gains[tapset0][1]
	g02 := gain0 * gains[tapset0][2]
	g10 := gain1 * gains[tapset1][0]
	g11 := gain1 * gains[tapset1][1]
	g12 := gain1 * gains[tapset1][2]
	overlap := min(shortBlockSampleCount, n)
	for i := 0; i < overlap; i++ {
		window := celtWindow(i)
		fade := window * window
		index := start + i
		buf[index] = buf[index] +
			(1-fade)*g00*buf[index-period0] +
			(1-fade)*g01*buf[index-period0-1] +
			(1-fade)*g01*buf[index-period0+1] +
			(1-fade)*g02*buf[index-period0-2] +
			(1-fade)*g02*buf[index-period0+2] +
			fade*g10*buf[index-period1] +
			fade*g11*buf[index-period1-1] +
			fade*g11*buf[index-period1+1] +
			fade*g12*buf[index-period1-2] +
			fade*g12*buf[index-period1+2]
	}
	for i := overlap; i < n; i++ {
		index := start + i
		buf[index] = buf[index] +
			g10*buf[index-period1] +
			g11*buf[index-period1-1] +
			g11*buf[index-period1+1] +
			g12*buf[index-period1-2] +
			g12*buf[index-period1+2]
	}
}

// denormaliseBands implements RFC 6716 Section 4.3.6 by restoring each
// normalized band with the square root of its decoded energy.
func denormaliseBands(info *frameSideInfo, x []float32, freq []float32, bandEnergy [maxBands]float32) {
	scale := 1 << info.lm
	for band := info.startBand; band < info.endBand; band++ {
		start := scale * int(bandEdges[band])
		end := scale * int(bandEdges[band+1])
		for i := start; i < end; i++ {
			freq[i] = x[i] * bandEnergy[band]
		}
	}
}

// inverseTransformChannel performs the RFC 6716 Section 4.3.7 IMDCT path for
// one channel and carries the weighted overlap-add tail into the next frame.
func (d *Decoder) inverseTransformChannel(freq []float32, channel int, info *frameSideInfo) []float32 {
	frameSampleCount := len(freq)
	accumulated := make([]float32, frameSampleCount+shortBlockSampleCount)
	blockCount := 1
	blockSampleCount := frameSampleCount
	stride := 1
	if info.transient {
		blockCount = 1 << info.lm
		blockSampleCount = shortBlockSampleCount
		stride = blockCount
	}
	// Transient spectra are interleaved short MDCTs; non-transient frames are
	// one long transform. Accumulate either form into a single time buffer.
	for block := range blockCount {
		blockFreq := make([]float32, blockSampleCount)
		if info.transient {
			for i := range blockSampleCount {
				blockFreq[i] = freq[block+i*stride]
			}
		} else {
			copy(blockFreq, freq)
		}
		blockTime := inverseMDCT(blockFreq)
		for i := range blockSampleCount + shortBlockSampleCount {
			accumulated[block*blockSampleCount+i] += blockTime[i]
		}
	}

	time := make([]float32, frameSampleCount)
	for i := range shortBlockSampleCount {
		time[i] = accumulated[i] + d.overlap[channel][i]
	}
	copy(time[shortBlockSampleCount:], accumulated[shortBlockSampleCount:frameSampleCount])
	copy(d.overlap[channel], accumulated[frameSampleCount:frameSampleCount+shortBlockSampleCount])

	return time
}

// inverseMDCT follows the RFC 6716 Section 4.3.7 low-overlap IMDCT shape:
// N frequency samples become 2*N time samples plus the CELT overlap tail.
func inverseMDCT(freq []float32) []float32 {
	n2 := len(freq)
	n := 2 * n2
	n4 := n >> 2
	sine := float32(2 * math.Pi * 0.125 / float64(n))
	preRotated := make([]complex32, n4)
	// Pack the MDCT input into the complex half-size transform domain before
	// the inverse complex step, matching the reference mdct_backward staging.
	for i := range n4 {
		xp1 := freq[2*i]
		xp2 := freq[n2-1-2*i]
		cosine := float32(math.Cos(2 * math.Pi * float64(i) / float64(n)))
		sineQuarter := float32(math.Cos(2 * math.Pi * float64(n4-i) / float64(n)))
		yr := -xp2*cosine + xp1*sineQuarter
		yi := -xp2*sineQuarter - xp1*cosine
		preRotated[i] = complex32{r: yr - yi*sine, i: yi + yr*sine}
	}

	fftOut := inverseComplexDFT(preRotated)
	postRotated := make([]float32, n2)
	// Rotate back out of the complex domain and restore the packed even/odd
	// ordering expected by the time-domain mirror step.
	for i, value := range fftOut {
		re := value.r
		im := value.i
		cosine := float32(math.Cos(2 * math.Pi * float64(i) / float64(n)))
		sineQuarter := float32(math.Cos(2 * math.Pi * float64(n4-i) / float64(n)))
		yr := re*cosine - im*sineQuarter
		yi := im*cosine + re*sineQuarter
		postRotated[2*i] = yr - yi*sine
		postRotated[2*i+1] = yi + yr*sine
	}

	deshuffled := make([]float32, n2)
	for i := range n4 {
		deshuffled[2*i] = -postRotated[2*i]
		deshuffled[2*i+1] = postRotated[n2-1-2*i]
	}

	overlap := shortBlockSampleCount
	out := make([]float32, n2+overlap)
	leftPlain := n4 - overlap/2
	// Apply the low-overlap window from RFC 6716 Section 4.3.7. The middle
	// region is unwindowed; the edges are mirrored for TDAC overlap-add.
	for i := 0; i < leftPlain; i++ {
		out[n4+overlap/2-1-i] = float32(deshuffled[n4-1-i])
	}
	for i := leftPlain; i < n4; i++ {
		x1 := deshuffled[n4-1-i]
		windowIndex := i - leftPlain
		out[windowIndex] += -celtWindow(windowIndex) * x1
		out[n4+overlap/2-1-i] += celtWindow(overlap-1-windowIndex) * x1
	}

	for i := 0; i < leftPlain; i++ {
		out[n4+overlap/2+i] = deshuffled[n4+i]
	}
	for i := leftPlain; i < n4; i++ {
		x2 := deshuffled[n4+i]
		windowIndex := i - leftPlain
		out[n2+overlap-1-windowIndex] = celtWindow(windowIndex) * x2
		out[n4+overlap/2+i] = celtWindow(overlap-1-windowIndex) * x2
	}

	return out
}

// inverseComplexDFT is the complex inverse transform used by the current IMDCT
// implementation. It is kept separate so a later FFT implementation can replace
// this step without changing the surrounding RFC 6716 Section 4.3.7 mapping.
func inverseComplexDFT(in []complex32) []complex32 {
	n := len(in)
	out := make([]complex32, n)
	for k := range n {
		sumR := float32(0)
		sumI := float32(0)
		for m, value := range in {
			angle := 2 * math.Pi * float64(k*m) / float64(n)
			cosine := float32(math.Cos(angle))
			sine := float32(math.Sin(angle))
			sumR += value.r*cosine - value.i*sine
			sumI += value.r*sine + value.i*cosine
		}
		out[k] = complex32{r: sumR, i: sumI}
	}

	return out
}

func celtWindow(i int) float32 {
	return celtWindow120[i]
}

// deemphasisAndInterleave applies the decoder-side pre-emphasis inversion after
// RFC 6716 synthesis and writes interleaved PCM samples for the caller.
func (d *Decoder) deemphasisAndInterleave(x []float32, y []float32, out []float32, frameSampleCount int, channelCount int) {
	for sample := range frameSampleCount {
		left := x[sample] + d.preemphasisMem[0]
		d.preemphasisMem[0] = 0.85000610 * left
		out[sample*channelCount] = left / 32768
		if channelCount == 2 {
			right := y[sample] + d.preemphasisMem[1]
			d.preemphasisMem[1] = 0.85000610 * right
			out[sample*channelCount+1] = right / 32768
		}
	}
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}

	return b
}
