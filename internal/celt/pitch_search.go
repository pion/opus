// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // Short names mirror libopus celt/pitch.c, which this file ports.
package celt

import "math"

// Port of libopus celt/pitch.c. The search runs on a 2x decimated, LPC-whitened
// copy of the pre-emphasized signal: whitening flattens the spectral envelope so
// the correlation measures periodicity rather than overall spectral shape, which
// is what keeps long lags from winning on tonal material.

const pitchLPCOrder = 4

// secondCheckTable is libopus's second_check: for each sub-multiple k it names
// the companion lag whose correlation is averaged in, so a true fundamental has
// to show up at two places rather than one.
var secondCheckTable = [16]int{ //nolint:gochecknoglobals
	0, 0, 3, 2, 3, 2, 5, 2, 3, 2, 3, 2, 5, 2, 3, 2,
}

// celtFir5 applies the 5-tap FIR whitening filter in place, zero state.
func celtFir5(x []float32, num [5]float32) {
	var mem0, mem1, mem2, mem3, mem4 float32
	for i := range x {
		sum := x[i] +
			num[0]*mem0 + num[1]*mem1 + num[2]*mem2 + num[3]*mem3 + num[4]*mem4
		mem4, mem3, mem2, mem1, mem0 = mem3, mem2, mem1, mem0, x[i]
		x[i] = sum
	}
}

// celtAutocorr returns lag+1 autocorrelation values of x, written into ac.
func celtAutocorr(x []float32, lag int, ac []float32) []float32 {
	ac = ac[:lag+1]
	for k := 0; k <= lag; k++ {
		var d float64
		for i := k; i < len(x); i++ {
			d += float64(x[i]) * float64(x[i-k])
		}
		ac[k] = float32(d)
	}

	return ac
}

// celtLPC solves for p LPC coefficients by Levinson-Durbin recursion.
// Port of libopus _celt_lpc (celt/celt_lpc.c).
func celtLPC(ac []float32, p int, lpc []float32) []float32 {
	lpc = lpc[:p]
	clear(lpc)
	if ac[0] <= 1e-10 {
		return lpc
	}

	nErr := ac[0]
	for i := range p {
		var rr float32
		for j := range i {
			rr += lpc[j] * ac[i-j]
		}
		rr += ac[i+1]
		r := -rr / nErr
		lpc[i] = r
		for j := range (i + 1) >> 1 {
			tmp1, tmp2 := lpc[j], lpc[i-1-j]
			lpc[j] = tmp1 + r*tmp2
			lpc[i-1-j] = tmp2 + r*tmp1
		}
		nErr -= r * r * nErr
		// Bail out once we get 30 dB gain.
		if nErr <= 0.001*ac[0] {
			break
		}
	}

	return lpc
}

// pitchDownsample decimates the channels by factor into xLP, sums them, then
// whitens the result with a 4th-order LPC filter plus a fixed zero.
func pitchDownsample(x [][]float32, xLP []float32, length, factor int, scratch *encoderScratch) {
	offset := factor / 2
	for i := 1; i < length; i++ {
		xLP[i] = 0.25*x[0][factor*i-offset] + 0.25*x[0][factor*i+offset] + 0.5*x[0][factor*i]
	}
	xLP[0] = 0.25*x[0][offset] + 0.5*x[0][0]
	for _, right := range x[1:] {
		for i := 1; i < length; i++ {
			xLP[i] += 0.25*right[factor*i-offset] + 0.25*right[factor*i+offset] + 0.5*right[factor*i]
		}
		xLP[0] += 0.25*right[offset] + 0.5*right[0]
	}

	ac := celtAutocorr(xLP[:length], pitchLPCOrder, scratch.autocorr[:])
	// Noise floor at -40 dB, then lag windowing: both keep the LPC solve from
	// chasing a near-singular autocorrelation on quiet or very tonal frames.
	ac[0] *= 1.0001
	for i := 1; i <= pitchLPCOrder; i++ {
		ac[i] -= ac[i] * (0.008 * float32(i)) * (0.008 * float32(i))
	}

	lpc := celtLPC(ac, pitchLPCOrder, scratch.lpc[:])
	tmp := float32(1.0)
	for i := range pitchLPCOrder {
		tmp *= 0.9
		lpc[i] *= tmp
	}

	// Add a zero at 0.8 to tilt the whitening away from DC.
	const c1 = 0.8
	num := [5]float32{
		lpc[0] + c1,
		lpc[1] + c1*lpc[0],
		lpc[2] + c1*lpc[1],
		lpc[3] + c1*lpc[2],
		c1 * lpc[3],
	}
	celtFir5(xLP[:length], num)
}

// celtPitchXcorr fills xcorr[i] with the inner product of x and y shifted by i.
//
// Eight lags are processed per outer iteration, each with its own independent
// float64 accumulator. Every accumulator is a chain over j = 0..length-1 in the
// same order and with the same precision as the single-lag version, so the
// result is bit-identical: each lag's sum is untouched, we just interleave the
// independent chains to break the serial dependency that keeps the single-lag
// loop running at ADD latency.
func celtPitchXcorr(x, y, xcorr []float32, length, maxPitch int) {
	i := 0
	for ; i+7 < maxPitch; i += 8 {
		var s0, s1, s2, s3, s4, s5, s6, s7 float64
		yb := y[i : i+length+7]
		for j := range length {
			xj := float64(x[j])
			s0 += xj * float64(yb[j])
			s1 += xj * float64(yb[j+1])
			s2 += xj * float64(yb[j+2])
			s3 += xj * float64(yb[j+3])
			s4 += xj * float64(yb[j+4])
			s5 += xj * float64(yb[j+5])
			s6 += xj * float64(yb[j+6])
			s7 += xj * float64(yb[j+7])
		}
		xcorr[i], xcorr[i+1], xcorr[i+2], xcorr[i+3] = float32(s0), float32(s1), float32(s2), float32(s3)
		xcorr[i+4], xcorr[i+5], xcorr[i+6], xcorr[i+7] = float32(s4), float32(s5), float32(s6), float32(s7)
	}
	for ; i < maxPitch; i++ {
		var sum float64
		for j := range length {
			sum += float64(x[j]) * float64(y[i+j])
		}
		xcorr[i] = float32(sum)
	}
}

// findBestPitch returns the two lags with the highest normalized correlation.
// The running Syy keeps the normalisation honest as the window slides, which is
// what stops long lags from winning just because they overlap less.
func findBestPitch(xcorr, y []float32, length, maxPitch int) [2]int {
	bestPitch := [2]int{0, 1}
	bestNum := [2]float32{-1, -1}
	bestDen := [2]float32{0, 0}

	syy := float32(1)
	for j := range length {
		syy += y[j] * y[j]
	}
	for i := range maxPitch {
		if xcorr[i] > 0 {
			// Scaling before squaring avoids both underflow and overflow.
			xcorr16 := xcorr[i] * 1e-12
			num := xcorr16 * xcorr16
			if num*bestDen[1] > bestNum[1]*syy {
				if num*bestDen[0] > bestNum[0]*syy {
					bestNum[1], bestDen[1], bestPitch[1] = bestNum[0], bestDen[0], bestPitch[0]
					bestNum[0], bestDen[0], bestPitch[0] = num, syy, i
				} else {
					bestNum[1], bestDen[1], bestPitch[1] = num, syy, i
				}
			}
		}
		syy += y[i+length]*y[i+length] - y[i]*y[i]
		syy = max32(1, syy)
	}

	return bestPitch
}

// refineXcorr recomputes the correlation at 2x decimation, but only near the
// two lags the coarse pass liked, which is where the reference spends its
// budget too.
func refineXcorr(xLP, y, xcorr []float32, length, maxPitch int, coarse [2]int) {
	for i := range maxPitch >> 1 {
		xcorr[i] = 0
		if absInt(i-2*coarse[0]) > 2 && absInt(i-2*coarse[1]) > 2 {
			continue
		}
		var sum float64
		for j := range length >> 1 {
			sum += float64(xLP[j]) * float64(y[i+j])
		}
		xcorr[i] = max32(-1, float32(sum))
	}
}

// pitchSearch finds the lag of the strongest correlation between xLP and y.
// Port of libopus pitch_search: a coarse pass on a further 2x decimation, then
// a finer pass restricted to the neighborhood of the two best coarse lags.
func pitchSearch(xLP, y []float32, length, maxPitch int, scratch *encoderScratch) int {
	lag := length + maxPitch

	xLP4 := scratch.pitchX[:length>>2]
	yLP4 := scratch.pitchY[:lag>>2]
	xcorr := scratch.pitchXC[:maxPitch>>1]
	clear(xcorr)

	// Downsample by 2 again.
	for j := range xLP4 {
		xLP4[j] = xLP[2*j]
	}
	for j := range yLP4 {
		yLP4[j] = y[2*j]
	}

	celtPitchXcorr(xLP4, yLP4, xcorr, length>>2, maxPitch>>2)
	bestPitch := findBestPitch(xcorr, yLP4, length>>2, maxPitch>>2)

	refineXcorr(xLP, y, xcorr, length, maxPitch, bestPitch)
	bestPitch = findBestPitch(xcorr, y, length>>1, maxPitch>>1)

	// Refine by pseudo-interpolation against the neighboring lags.
	offset := 0
	if bestPitch[0] > 0 && bestPitch[0] < (maxPitch>>1)-1 {
		a, b, c := xcorr[bestPitch[0]-1], xcorr[bestPitch[0]], xcorr[bestPitch[0]+1]
		switch {
		case c-a > 0.7*(b-a):
			offset = 1
		case a-c > 0.7*(b-c):
			offset = -1
		}
	}

	return 2*bestPitch[0] - offset
}

func computePitchGain(xy, xx, yy float32) float32 {
	return xy / float32(math.Sqrt(float64(1+xx*yy)))
}

// innerProdLag returns sum(x[base+j] * x[base+j-lag]) over n samples.
func innerProdLag(x []float32, base, n, lag int) float32 {
	var s float64
	for j := range n {
		s += float64(x[base+j]) * float64(x[base+j-lag])
	}

	return float32(s)
}

func dualInnerProdLag(x []float32, base, n, lagA, lagB int) (a, b float32) {
	var sa, sb float64
	for j := range n {
		v := float64(x[base+j])
		sa += v * float64(x[base+j-lagA])
		sb += v * float64(x[base+j-lagB])
	}

	return float32(sa), float32(sb)
}

// removeDoubling corrects the pitch period when the search locked onto a
// multiple of the true fundamental, and returns the gain for the chosen lag.
// Port of libopus remove_doubling (celt/pitch.c). x is the decimated, whitened
// buffer; period is in and out in undecimated units.
//
//nolint:cyclop,gocognit // Mirrors the reference sub-multiple scan.
func removeDoubling(
	x []float32, maxPeriod, minPeriod, n int, period *int, prevPeriod int, prevGain float32,
	scratch *encoderScratch,
) float32 {
	minPeriod0 := minPeriod
	maxPeriod /= 2
	minPeriod /= 2
	*period /= 2
	prevPeriod /= 2
	n /= 2
	// libopus advances the pointer by maxperiod and indexes backwards from
	// there; base plays that role.
	base := maxPeriod
	if *period >= maxPeriod {
		*period = maxPeriod - 1
	}

	t0 := *period
	t := t0

	xx := innerProdLag(x, base, n, 0)
	xy := innerProdLag(x, base, n, t0)

	// yyLookup[i] is the energy of the window ending i samples back, updated
	// incrementally so each candidate lag costs one inner product, not two.
	yyLookup := scratch.yyLookup[:maxPeriod+1]
	yyLookup[0] = xx
	yy := xx
	for i := 1; i <= maxPeriod; i++ {
		yy = yy + x[base-i]*x[base-i] - x[base+n-i]*x[base+n-i]
		yyLookup[i] = max32(0, yy)
	}
	yy = yyLookup[t0]
	bestXY, bestYY := xy, yy
	g0 := computePitchGain(xy, xx, yy)
	g := g0

	for k := 2; k <= 15; k++ {
		t1 := (2*t0 + k) / (2 * k)
		if t1 < minPeriod {
			break
		}
		var t1b int
		if k == 2 {
			if t1+t0 > maxPeriod {
				t1b = t0
			} else {
				t1b = t0 + t1
			}
		} else {
			t1b = (2*secondCheckTable[k]*t0 + k) / (2 * k)
		}
		xyA, xyB := dualInnerProdLag(x, base, n, t1, t1b)
		xy = 0.5 * (xyA + xyB)
		yy = 0.5 * (yyLookup[t1] + yyLookup[t1b])
		g1 := computePitchGain(xy, xx, yy)

		var cont float32
		switch {
		case absInt(t1-prevPeriod) <= 1:
			cont = prevGain
		case absInt(t1-prevPeriod) <= 2 && 5*k*k < t0:
			cont = 0.5 * prevGain
		}
		// Bias against very short periods, where short-term correlation alone
		// can look like pitch.
		thresh := max32(0.3, 0.7*g0-cont)
		if t1 < 3*minPeriod {
			thresh = max32(0.4, 0.85*g0-cont)
		}
		if t1 < 2*minPeriod {
			thresh = max32(0.5, 0.9*g0-cont)
		}
		if g1 > thresh {
			bestXY, bestYY = xy, yy
			t = t1
			g = g1
		}
	}

	if t < minPeriod*2 {
		t1 := t * 5 / 8
		t2 := t * 6 / 8
		xy1, xy2 := dualInnerProdLag(x, base, n, t1, t2)
		if computePitchGain(xy1, xx, yyLookup[t1]) >= g ||
			computePitchGain(xy2, xx, yyLookup[t2]) >= g {
			g = 0
		}
	}

	bestXY = max32(0, bestXY)
	pg := float32(1.0)
	if bestYY > bestXY {
		pg = bestXY / (bestYY + 1)
	}

	var xcorr [3]float32
	for k := range 3 {
		xcorr[k] = innerProdLag(x, base, n, t+k-1)
	}
	offset := 0
	switch {
	case xcorr[2]-xcorr[0] > 0.7*(xcorr[1]-xcorr[0]):
		offset = 1
	case xcorr[0]-xcorr[2] > 0.7*(xcorr[1]-xcorr[2]):
		offset = -1
	}
	pg = min32(pg, g)

	*period = max(2*t+offset, minPeriod0)

	return pg
}
