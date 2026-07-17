// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

const (
	findPitchWhiteNoiseFraction = 1e-3
	findPitchBandwidthExpansion = 0.99
	laPitchMS                   = 2
	findPitchLPCWinMS           = 20 + laPitchMS<<1 // FIND_PITCH_LPC_WIN_MS
	pitchEstLPCOrder            = 16
	pitchEstComplexity          = 2   // 0..2, highest = best
	pitchSearchThreshold        = 0.3 // first-stage candidate threshold
)

// findPitchLags whitens the signal and runs the pitch estimator (a port of
// silk_find_pitch_lags_FLP). analysisBuf holds the LTP-memory history followed
// by the current frame (length ltp_mem_length + frame_length). It returns
// whether the frame is voiced, the per-subframe pitch lags, the lag/contour
// indices, and the whitening residual (reused by the LTP analysis).
func (e *Encoder) findPitchLags(analysisBuf []float32, fsKHz, nbSubfr, speechActivityQ8 int, inputTiltQ15 int32) (voiced bool, pitchL []int, lagIndex int16, contourIndex int8, res []float32, predGain float32) {
	bufLen := len(analysisBuf)
	laPitch := laPitchMS * fsKHz
	winLength := findPitchLPCWinMS * fsKHz

	// Windowed signal: rising edge, flat middle, falling edge.
	wsig := make([]float32, winLength)
	start := bufLen - winLength
	applySineWindowFLP(wsig, analysisBuf[start:], 1, laPitch)
	copy(wsig[laPitch:winLength-laPitch], analysisBuf[start+laPitch:start+winLength-laPitch])
	applySineWindowFLP(wsig[winLength-laPitch:], analysisBuf[start+winLength-laPitch:], 2, laPitch)

	// Whitening LPC via autocorrelation + Schur.
	autoCorr := make([]float32, pitchEstLPCOrder+1)
	autocorrelationFLP(autoCorr, wsig, winLength, pitchEstLPCOrder+1)
	autoCorr[0] += autoCorr[0]*findPitchWhiteNoiseFraction + 1

	refl := make([]float32, pitchEstLPCOrder)
	resNrg := schurFLP(refl, autoCorr, pitchEstLPCOrder)
	predGain = autoCorr[0] / max(resNrg, 1.0)
	a := make([]float32, pitchEstLPCOrder)
	k2aFLP(a, refl, pitchEstLPCOrder)
	bwexpanderFLP(a, pitchEstLPCOrder, findPitchBandwidthExpansion)

	res = make([]float32, bufLen)
	lpcAnalysisFilterFLP(res, a, analysisBuf, bufLen, pitchEstLPCOrder)

	// Voicing threshold (search_thres2).
	prevVoiced := float32(0)
	if e.isPreviousFrameVoiced {
		prevVoiced = 1
	}
	thrhld := float32(0.6) -
		0.004*pitchEstLPCOrder -
		0.1*float32(speechActivityQ8)*(1.0/256.0) -
		0.15*prevVoiced -
		0.1*float32(inputTiltQ15)*(1.0/32768.0)

	pitchL = make([]int, nbSubfr)
	prevLag := 0
	if e.isPreviousFrameVoiced {
		prevLag = e.previousLag
	}
	lagIndex, contourIndex, voiced = pitchAnalysisCore(
		res, pitchL, &e.ltpCorr, prevLag, pitchSearchThreshold, thrhld, fsKHz, pitchEstComplexity, nbSubfr)

	return voiced, pitchL, lagIndex, contourIndex, res, predGain
}
