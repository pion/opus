// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

// l1Metric is libopus's l1_metric: the L1 norm of a band, biased so that a
// coarser time resolution has to win by a margin before it is chosen.
func l1Metric(x []float32, lm int, bias float32) float32 {
	var l1 float32
	for _, v := range x {
		if v < 0 {
			v = -v
		}
		l1 += v
	}

	return l1 + float32(lm)*bias*l1
}

// tfAnalysis picks the per-band time/frequency resolution and the global
// tf_select flag, mirroring libopus tf_analysis (celt_encoder.c). For each
// band it measures the L1 norm of the normalized spectrum under every
// resolution reachable by haar1, then runs a Viterbi pass whose transition
// cost (lambda) discourages switching resolution between neighboring bands.
//
// Getting this wrong does not change how many bits a band receives — it
// changes which time/frequency tile the quantization noise lands in, which
// is what decides whether the noise stays masked.
//
//nolint:cyclop,gocognit // Mirrors the reference tf_analysis metric sweep and Viterbi pass.
func tfAnalysis(
	normalized []float32,
	lm, startBand, endBand int,
	transient bool,
	lambda int,
	tfEstimate float32,
	tfChan, frameSize int,
	importance *[maxBands]int,
	tfRes *[maxBands]int,
) int {
	bias := 0.04 * maxFloat32(-0.25, 0.5-tfEstimate)

	widest := int(bandEdges[endBand]-bandEdges[endBand-1]) << lm
	tmp := make([]float32, widest)
	tmpOne := make([]float32, widest)

	var metric [maxBands]int
	for band := startBand; band < endBand; band++ {
		width := int(bandEdges[band+1] - bandEdges[band])
		n := width << lm
		// A one-bin band cannot be split all the way down to LM=-1.
		narrow := width == 1

		src := normalized[tfChan*frameSize+(int(bandEdges[band])<<lm):]
		copy(tmp[:n], src[:n])

		lmForMetric := 0
		if transient {
			lmForMetric = lm
		}
		bestL1 := l1Metric(tmp[:n], lmForMetric, bias)
		bestLevel := 0

		if transient && !narrow {
			copy(tmpOne[:n], tmp[:n])
			haar1(tmpOne[:n], n>>lm, 1<<lm)
			if l1 := l1Metric(tmpOne[:n], lm+1, bias); l1 < bestL1 {
				bestL1 = l1
				bestLevel = -1
			}
		}

		levels := lm
		if !transient && !narrow {
			levels++
		}
		for k := range levels {
			b := k + 1
			if transient {
				b = lm - k - 1
			}
			haar1(tmp[:n], n>>k, 1<<k)
			if l1 := l1Metric(tmp[:n], b, bias); l1 < bestL1 {
				bestL1 = l1
				bestLevel = k + 1
			}
		}

		// Q1 so the mid-point (-0.5) is representable for narrow bands.
		if transient {
			metric[band] = 2 * bestLevel
		} else {
			metric[band] = -2 * bestLevel
		}
		if narrow && (metric[band] == 0 || metric[band] == -2*lm) {
			metric[band]--
		}
	}

	table := tfSelectTable[lm]
	base := 4 * boolIndex(transient)
	tfSelect := 0

	var selCost [2]int
	for sel := range 2 {
		cost0 := importance[startBand] * abs(metric[startBand]-2*int(table[base+2*sel]))
		cost1 := importance[startBand] * abs(metric[startBand]-2*int(table[base+2*sel+1]))
		if !transient {
			cost1 += lambda
		}
		for band := startBand + 1; band < endBand; band++ {
			curr0 := min(cost0, cost1+lambda)
			curr1 := min(cost0+lambda, cost1)
			cost0 = curr0 + importance[band]*abs(metric[band]-2*int(table[base+2*sel]))
			cost1 = curr1 + importance[band]*abs(metric[band]-2*int(table[base+2*sel+1]))
		}
		selCost[sel] = min(cost0, cost1)
	}
	// libopus stays conservative and only allows tf_select=1 for transients.
	if selCost[1] < selCost[0] && transient {
		tfSelect = 1
	}

	var path0, path1 [maxBands]int
	cost0 := importance[startBand] * abs(metric[startBand]-2*int(table[base+2*tfSelect]))
	cost1 := importance[startBand] * abs(metric[startBand]-2*int(table[base+2*tfSelect+1]))
	if !transient {
		cost1 += lambda
	}
	for band := startBand + 1; band < endBand; band++ {
		curr0, curr1 := cost0, cost1
		if cost0 < cost1+lambda {
			path0[band] = 0
		} else {
			curr0 = cost1 + lambda
			path0[band] = 1
		}
		if cost0+lambda < cost1 {
			curr1 = cost0 + lambda
			path1[band] = 0
		} else {
			path1[band] = 1
		}
		cost0 = curr0 + importance[band]*abs(metric[band]-2*int(table[base+2*tfSelect]))
		cost1 = curr1 + importance[band]*abs(metric[band]-2*int(table[base+2*tfSelect+1]))
	}

	tfRes[endBand-1] = boolIndex(cost1 <= cost0)
	for band := endBand - 2; band >= startBand; band-- {
		if tfRes[band+1] == 1 {
			tfRes[band] = path1[band+1]
		} else {
			tfRes[band] = path0[band+1]
		}
	}

	return tfSelect
}

func abs(v int) int {
	if v < 0 {
		return -v
	}

	return v
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}

	return b
}
