// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// stage3Array holds the per-subframe, per-codebook, per-lag correlations or
// energies computed for the stage-3 refinement.
type stage3Array = [peMaxNBSubfr][peNBCbksStage3Max][peNBStage3Lags]float32

// stage3Params selects the codebook/range tables for the stage-3 search.
func stage3Params(nbSubfr, complexity int) (lagRange [][2]int8, lagCB [][]int8, nbCbkSearch int) {
	if nbSubfr == peMaxNBSubfr {
		rng := silkLagRangeStage3[complexity]
		lagRange = rng[:]
		lagCB = silkCBLagsStage3[:]

		return lagRange, lagCB, int(silkNbCbkSearchsStage3[complexity])
	}
	lagRange = silkLagRangeStage3_10ms[:]
	lagCB = silkCBLagsStage3_10ms[:]

	return lagRange, lagCB, peNBCbksStage3_10ms
}

// calcCorrST3 fills the stage-3 cross-correlation array
// (silk_P_Ana_calc_corr_st3).
func calcCorrST3(crossCorr *stage3Array, frame []float32, startLag, sfLength, nbSubfr, complexity int) {
	lagRange, lagCB, nbCbkSearch := stage3Params(nbSubfr, complexity)

	var scratch [scratchSizePitch]float32
	var xcorr [scratchSizePitch]float32
	targetOffset := sfLength << 2
	for k := range nbSubfr { //nolint:varnamelen // k indexes the subframe.
		lagLow := int(lagRange[k][0])
		lagHigh := int(lagRange[k][1])
		pitchXcorr(frame[targetOffset:], frame[targetOffset-startLag-lagHigh:], xcorr[:], sfLength, lagHigh-lagLow+1)

		lagCounter := 0
		for j := lagLow; j <= lagHigh; j++ {
			scratch[lagCounter] = xcorr[lagHigh-j]
			lagCounter++
		}

		delta := lagLow
		for i := range nbCbkSearch {
			idx := int(lagCB[k][i]) - delta
			for j := range peNBStage3Lags {
				crossCorr[k][i][j] = scratch[idx+j]
			}
		}
		targetOffset += sfLength
	}
}

// calcEnergyST3 fills the stage-3 energy array (silk_P_Ana_calc_energy_st3).
func calcEnergyST3(energies *stage3Array, frame []float32, startLag, sfLength, nbSubfr, complexity int) {
	lagRange, lagCB, nbCbkSearch := stage3Params(nbSubfr, complexity)

	var scratch [scratchSizePitch]float32
	targetOffset := sfLength << 2
	for k := range nbSubfr { //nolint:varnamelen // k indexes the subframe.
		lagCounter := 0
		basisOffset := targetOffset - (startLag + int(lagRange[k][0]))
		energy := energyFLP(frame[basisOffset:], sfLength) + 1e-3
		scratch[lagCounter] = float32(energy)
		lagCounter++

		lagDiff := int(lagRange[k][1]) - int(lagRange[k][0]) + 1
		for i := 1; i < lagDiff; i++ {
			energy -= float64(frame[basisOffset+sfLength-i]) * float64(frame[basisOffset+sfLength-i])
			energy += float64(frame[basisOffset-i]) * float64(frame[basisOffset-i])
			scratch[lagCounter] = float32(energy)
			lagCounter++
		}

		delta := int(lagRange[k][0])
		for i := range nbCbkSearch {
			idx := int(lagCB[k][i]) - delta
			for j := range peNBStage3Lags {
				energies[k][i][j] = scratch[idx+j]
			}
		}
		targetOffset += sfLength
	}
}

// pitchAnalysisCore estimates the pitch lag (silk_pitch_analysis_core_FLP).
// It returns the lag index, contour index, per-subframe lags in pitchOut, and
// whether the frame is voiced. ltpCorr is updated in place. Only 8 and 16 kHz
// are handled; 12 kHz (medium-band) is pending down2_3.
//
//nolint:gocognit,gocyclo,cyclop,maintidx,unparam // faithful port; complexity is fixed at 2 today.
func pitchAnalysisCore(
	frame []float32,
	pitchOut []int,
	ltpCorr *float32,
	prevLag int,
	searchThres1, searchThres2 float32,
	fsKHz, complexity, nbSubfr int,
) (lagIndex int16, contourIndex int8, voiced bool) {
	frameLength := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * fsKHz
	frameLength4kHz := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * 4
	frameLength8kHz := (peLTPMemLengthMS + nbSubfr*peSubfrLengthMS) * 8
	sfLength := peSubfrLengthMS * fsKHz
	sfLength4kHz := peSubfrLengthMS * 4
	sfLength8kHz := peSubfrLengthMS * 8
	minLag := peMinLagMS * fsKHz
	minLag4kHz := peMinLagMS * 4
	minLag8kHz := peMinLagMS * 8
	maxLag := peMaxLagMS*fsKHz - 1
	maxLag4kHz := peMaxLagMS * 4
	maxLag8kHz := peMaxLagMS*8 - 1

	// Resample to 8 kHz, then decimate to 4 kHz.
	frame8FIX := make([]int16, frameLength8kHz)
	if fsKHz == 16 {
		frame16FIX := make([]int16, frameLength)
		float2ShortArray(frame16FIX, frame[:frameLength])
		var state [2]int32
		resamplerDown2(&state, frame8FIX, frame16FIX)
	} else {
		float2ShortArray(frame8FIX, frame[:frameLength8kHz])
	}

	frame4FIX := make([]int16, frameLength4kHz)
	var state [2]int32
	resamplerDown2(&state, frame4FIX, frame8FIX)

	frame8kHz := make([]float32, frameLength8kHz)
	frame4kHz := make([]float32, frameLength4kHz)
	short2FloatArray(frame8kHz, frame8FIX)
	short2FloatArray(frame4kHz, frame4FIX)

	// Low-pass filter (differentiator's inverse), int16-saturating.
	for i := frameLength4kHz - 1; i > 0; i-- {
		frame4kHz[i] = float32(sat16(int32(frame4kHz[i]) + int32(frame4kHz[i-1])))
	}

	var c [peMaxNBSubfr][(peMaxLag >> 1) + 5]float32 //nolint:varnamelen // c is the correlation grid.
	xcorr := make([]float32, maxLag4kHz-minLag4kHz+1)

	// First stage at 4 kHz.
	targetOffset := sfLength4kHz << 2
	for k := 0; k < nbSubfr>>1; k++ {
		basisOffset := targetOffset - minLag4kHz
		pitchXcorr(
			frame4kHz[targetOffset:], frame4kHz[targetOffset-maxLag4kHz:], xcorr, sfLength8kHz, maxLag4kHz-minLag4kHz+1,
		)

		crossCorr := float64(xcorr[maxLag4kHz-minLag4kHz])
		normalizer := energyFLP(frame4kHz[targetOffset:], sfLength8kHz) +
			energyFLP(frame4kHz[basisOffset:], sfLength8kHz) +
			float64(sfLength8kHz)*4000.0
		c[0][minLag4kHz] += float32(2 * crossCorr / normalizer)

		for d := minLag4kHz + 1; d <= maxLag4kHz; d++ {
			basisOffset--
			crossCorr = float64(xcorr[maxLag4kHz-d])
			normalizer += float64(frame4kHz[basisOffset])*float64(frame4kHz[basisOffset]) -
				float64(frame4kHz[basisOffset+sfLength8kHz])*float64(frame4kHz[basisOffset+sfLength8kHz])
			c[0][d] += float32(2 * crossCorr / normalizer)
		}
		targetOffset += sfLength8kHz
	}

	// Short-lag bias.
	for i := maxLag4kHz; i >= minLag4kHz; i-- {
		c[0][i] -= c[0][i] * float32(i) / 4096.0
	}

	lengthDSrch := 4 + 2*complexity
	dSrch := make([]int, peDSrchLength)
	insertionSortDecreasingFLP(c[0][minLag4kHz:], dSrch, maxLag4kHz-minLag4kHz+1, lengthDSrch)

	cmax := c[0][minLag4kHz]
	if cmax < 0.2 {
		clearPitch(pitchOut, nbSubfr)
		*ltpCorr = 0

		return 0, 0, false
	}

	threshold := searchThres1 * cmax
	for i := range lengthDSrch {
		if c[0][minLag4kHz+i] > threshold {
			dSrch[i] = (dSrch[i] + minLag4kHz) << 1 //nolint:gosec // G602: i < lengthDSrch.
		} else {
			lengthDSrch = i

			break
		}
	}

	dComp := make([]int16, (peMaxLag>>1)+5)
	for i := 0; i < lengthDSrch; i++ {
		dComp[dSrch[i]] = 1
	}
	for i := maxLag8kHz + 3; i >= minLag8kHz; i-- {
		dComp[i] += dComp[i-1] + dComp[i-2]
	}
	lengthDSrch = 0
	for i := minLag8kHz; i < maxLag8kHz+1; i++ {
		if dComp[i+1] > 0 { //nolint:gosec // G602: i+1 <= maxLag8kHz+1 < len(dComp).
			dSrch[lengthDSrch] = i
			lengthDSrch++
		}
	}
	for i := maxLag8kHz + 3; i >= minLag8kHz; i-- {
		dComp[i] += dComp[i-1] + dComp[i-2] + dComp[i-3]
	}
	lengthDComp := 0
	for i := minLag8kHz; i < maxLag8kHz+4; i++ {
		if dComp[i] > 0 { //nolint:gosec // G602: i < maxLag8kHz+4 < len(dComp).
			dComp[lengthDComp] = int16(i - 2)
			lengthDComp++
		}
	}

	// Second stage at 8 kHz.
	for k := range c {
		for d := range c[k] { //nolint:gosec // G602: k < len(c).
			c[k][d] = 0 //nolint:gosec // G602: indices within c.
		}
	}
	src8kHz := frame8kHz
	if fsKHz == 8 {
		src8kHz = frame
	}
	targetOffset = peLTPMemLengthMS * 8
	for k := range nbSubfr {
		energyTmp := energyFLP(src8kHz[targetOffset:], sfLength8kHz) + 1.0
		for j := 0; j < lengthDComp; j++ {
			d := int(dComp[j])
			basisOffset := targetOffset - d
			crossCorr := innerProductFLP(src8kHz[basisOffset:], src8kHz[targetOffset:], sfLength8kHz)
			if crossCorr > 0 {
				energy := energyFLP(src8kHz[basisOffset:], sfLength8kHz)
				c[k][d] = float32(2 * crossCorr / (energy + energyTmp))
			} else {
				c[k][d] = 0
			}
		}
		targetOffset += sfLength8kHz
	}

	// Search the lag range and codebook.
	ccmax := float32(0)
	ccmaxB := float32(-1000)
	cbimax := 0
	lag := -1

	var prevLagLog2 float32
	if prevLag > 0 {
		if fsKHz == 16 {
			prevLag >>= 1
		}
		prevLagLog2 = silkLog2(float64(prevLag))
	}

	var lagCB [][]int8
	var nbCbkSearch int
	if nbSubfr == peMaxNBSubfr {
		lagCB = silkCBLagsStage2[:]
		if fsKHz == 8 && complexity > 0 {
			nbCbkSearch = peNBCbksStage2Ext
		} else {
			nbCbkSearch = peNBCbksStage2
		}
	} else {
		lagCB = silkCBLagsStage2_10ms[:]
		nbCbkSearch = peNBCbksStage2_10ms
	}

	cc := make([]float32, peNBCbksStage2Ext)
	for k := 0; k < lengthDSrch; k++ {
		d := dSrch[k] //nolint:varnamelen // d is the candidate lag, as in the C reference.
		for j := range nbCbkSearch {
			cc[j] = 0
			for i := range nbSubfr {
				cc[j] += c[i][d+int(lagCB[i][j])]
			}
		}
		ccmaxNew := float32(-1000)
		cbimaxNew := 0
		for i := range nbCbkSearch {
			if cc[i] > ccmaxNew {
				ccmaxNew = cc[i]
				cbimaxNew = i
			}
		}

		lagLog2 := silkLog2(float64(d))
		ccmaxNewB := ccmaxNew - peShortlagBias*float32(nbSubfr)*lagLog2
		if prevLag > 0 {
			deltaLagLog2Sqr := lagLog2 - prevLagLog2
			deltaLagLog2Sqr *= deltaLagLog2Sqr
			ccmaxNewB -= pePrevlagBias * float32(nbSubfr) * (*ltpCorr) * deltaLagLog2Sqr / (deltaLagLog2Sqr + 0.5)
		}

		if ccmaxNewB > ccmaxB && ccmaxNew > float32(nbSubfr)*searchThres2 {
			ccmaxB = ccmaxNewB
			ccmax = ccmaxNew
			lag = d
			cbimax = cbimaxNew
		}
	}

	if lag == -1 {
		clearPitch(pitchOut, nbSubfr)
		*ltpCorr = 0

		return 0, 0, false
	}

	*ltpCorr = ccmax / float32(nbSubfr)

	if fsKHz > 8 {
		lag <<= 1                                                  // fsKHz == 16
		lag = int(clamp(int32(minLag), int32(lag), int32(maxLag))) //nolint:gosec // G115
		startLag := max(lag-2, minLag)
		endLag := min(lag+2, maxLag)
		lagNew := lag
		cbimax = 0
		ccmax = -1000

		var crossCorrST3, energiesST3 stage3Array
		calcCorrST3(&crossCorrST3, frame, startLag, sfLength, nbSubfr, complexity)
		calcEnergyST3(&energiesST3, frame, startLag, sfLength, nbSubfr, complexity)

		contourBias := float32(peFlatcontourBias) / float32(lag)
		lagCB3, nbCbkSearch3, cbkSize3 := stage3Codebook(nbSubfr, complexity)
		_ = cbkSize3

		targetOffset := peLTPMemLengthMS * fsKHz
		energyTmp := energyFLP(frame[targetOffset:], nbSubfr*sfLength) + 1.0
		lagCounter := 0
		for d := startLag; d <= endLag; d++ { //nolint:varnamelen // d is the candidate lag, as in the C reference.
			for j := range nbCbkSearch3 { //nolint:varnamelen // j indexes the codebook.
				crossCorr := 0.0
				energy := energyTmp
				for k := range nbSubfr {
					crossCorr += float64(crossCorrST3[k][j][lagCounter])
					energy += float64(energiesST3[k][j][lagCounter])
				}
				var ccmaxNew float32
				if crossCorr > 0 {
					ccmaxNew = float32(2 * crossCorr / energy)
					ccmaxNew *= 1.0 - contourBias*float32(j)
				}
				if ccmaxNew > ccmax && d+int(silkCBLagsStage3[0][j]) <= maxLag {
					ccmax = ccmaxNew
					lagNew = d
					cbimax = j
				}
			}
			lagCounter++
		}

		for k := range nbSubfr {
			pitchOut[k] = lagNew + int(lagCB3[k][cbimax])
			pitchOut[k] = int(clamp(int32(minLag), int32(pitchOut[k]), int32(peMaxLagMS*fsKHz))) //nolint:gosec // G115
		}
		lagIndex = int16(lagNew - minLag) //nolint:gosec // G115
		contourIndex = int8(cbimax)
	} else {
		for k := range nbSubfr {
			pitchOut[k] = lag + int(lagCB[k][cbimax])
			pitchOut[k] = int(clamp(int32(minLag8kHz), int32(pitchOut[k]), int32(peMaxLagMS*8))) //nolint:gosec // G115
		}
		lagIndex = int16(lag - minLag8kHz) //nolint:gosec // G115
		contourIndex = int8(cbimax)
	}

	return lagIndex, contourIndex, true
}

// stage3Codebook returns the stage-3 lag codebook and search count.
func stage3Codebook(nbSubfr, complexity int) (lagCB [][]int8, nbCbkSearch, cbkSize int) {
	if nbSubfr == peMaxNBSubfr {
		return silkCBLagsStage3[:], int(silkNbCbkSearchsStage3[complexity]), peNBCbksStage3Max
	}

	return silkCBLagsStage3_10ms[:], peNBCbksStage3_10ms, peNBCbksStage3_10ms
}

// clearPitch zeroes the first nbSubfr pitch lags.
func clearPitch(pitchOut []int, nbSubfr int) {
	for k := range nbSubfr {
		pitchOut[k] = 0
	}
}
