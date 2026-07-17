// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// Long-term-predictor gain quantization (silk_quant_LTP_gains + silk_VQ_WMat_EC).
// Given the find_LTP correlation matrices it searches the three LTP codebooks
// for the periodicity index and per-subframe filter indices that minimize a
// weighted quantization error plus rate cost.

const (
	maxSumLogGainDBQ7 = 5333 // round(MAX_SUM_LOG_GAIN_DB/6 * 128), MAX_SUM_LOG_GAIN_DB=250
	ltpGainSafetyQ7   = 51   // round(0.4 * 128)
	nLTPCodebooks     = 3
)

//nolint:gochecknoglobals // LTP codebook effective gains and bit costs (tables_LTP.c).
var (
	ltpVQSizes = [nLTPCodebooks]int{8, 16, 32}

	ltpGainVQ0 = []uint8{46, 2, 90, 87, 93, 91, 82, 98}
	ltpGainVQ1 = []uint8{109, 120, 118, 12, 113, 115, 117, 119, 99, 59, 87, 111, 63, 111, 112, 80}
	ltpGainVQ2 = []uint8{
		126, 124, 125, 124, 129, 121, 126, 23, 132, 127, 127, 127, 126, 127, 122, 133,
		130, 134, 101, 118, 119, 145, 126, 86, 124, 120, 123, 119, 170, 173, 107, 109,
	}

	ltpBitsQ50 = []uint8{15, 131, 138, 138, 155, 155, 173, 173}
	ltpBitsQ51 = []uint8{69, 93, 115, 118, 131, 138, 141, 138, 150, 150, 155, 150, 155, 160, 166, 160}
	ltpBitsQ52 = []uint8{
		131, 128, 134, 141, 141, 141, 145, 145, 145, 150, 155, 155, 155, 155, 160, 160,
		160, 160, 166, 166, 173, 173, 182, 192, 182, 192, 192, 192, 205, 192, 205, 224,
	}
)

func ltpCodebook(k int) [][]int8 {
	switch k {
	case 1:
		return codebookLTPFilterPeriodicityIndex1
	case 2:
		return codebookLTPFilterPeriodicityIndex2
	default:
		return codebookLTPFilterPeriodicityIndex0
	}
}

func ltpGainTable(k int) []uint8 {
	switch k {
	case 1:
		return ltpGainVQ1
	case 2:
		return ltpGainVQ2
	default:
		return ltpGainVQ0
	}
}

func ltpBitsTable(k int) []uint8 {
	switch k {
	case 1:
		return ltpBitsQ51
	case 2:
		return ltpBitsQ52
	default:
		return ltpBitsQ50
	}
}

// log2lin approximates 2^(inLogQ7/128) (silk_log2lin).
func log2lin(inLogQ7 int32) int32 {
	if inLogQ7 < 0 {
		return 0
	}
	if inLogQ7 >= 3967 {
		return math.MaxInt32
	}
	out := int32(1) << (inLogQ7 >> 7)
	frac := inLogQ7 & 0x7F
	adj := smlawb(frac, smulbb(frac, 128-frac), -174)
	if inLogQ7 < 2048 {
		return out + ((out * adj) >> 7)
	}

	return out + (out>>7)*adj
}

// vqWMatEC finds the codebook vector minimizing the weighted quantization error
// plus rate (silk_VQ_WMat_EC). XX_Q17 is the 5x5 correlation matrix (row-major),
// xX_Q17 the correlation vector, both Q17.
func vqWMatEC(
	xxQ17, xXQ17 []int32, cb [][]int8, cbGain, clQ5 []uint8, subfrLen int, maxGainQ7 int32, l int,
) (ind int, resNrgQ15, rateDistQ8, gainQ7 int32) {
	var negXX [ltpOrder]int32
	for i := range ltpOrder {
		negXX[i] = -(xXQ17[i] << 7)
	}

	rateDistQ8 = math.MaxInt32
	resNrgQ15 = math.MaxInt32
	for k := range l { //nolint:varnamelen // k indexes the codebook, as in the C reference.
		row := cb[k]
		gainTmp := int32(cbGain[k])
		sum1 := int32(32801) // 1.001 in Q15
		penalty := max(gainTmp-maxGainQ7, 0) << 11

		sum2 := negXX[0] + xxQ17[1]*int32(row[1]) + xxQ17[2]*int32(row[2]) + xxQ17[3]*int32(row[3]) + xxQ17[4]*int32(row[4])
		sum2 = (sum2 << 1) + xxQ17[0]*int32(row[0])
		sum1 = smlawb(sum1, sum2, int32(row[0]))

		sum2 = negXX[1] + xxQ17[7]*int32(row[2]) + xxQ17[8]*int32(row[3]) + xxQ17[9]*int32(row[4])
		sum2 = (sum2 << 1) + xxQ17[6]*int32(row[1])
		sum1 = smlawb(sum1, sum2, int32(row[1]))

		sum2 = negXX[2] + xxQ17[13]*int32(row[3]) + xxQ17[14]*int32(row[4])
		sum2 = (sum2 << 1) + xxQ17[12]*int32(row[2])
		sum1 = smlawb(sum1, sum2, int32(row[2]))

		sum2 = negXX[3] + xxQ17[19]*int32(row[4])
		sum2 = (sum2 << 1) + xxQ17[18]*int32(row[3])
		sum1 = smlawb(sum1, sum2, int32(row[3]))

		sum2 = (negXX[4] << 1) + xxQ17[24]*int32(row[4])
		sum1 = smlawb(sum1, sum2, int32(row[4]))

		if sum1 >= 0 {
			bitsResQ8 := smulbb(int32(subfrLen), lin2log(sum1+penalty)-(15<<7)) //nolint:gosec // G115
			bitsTotQ8 := addLShift32(bitsResQ8, int32(clQ5[k]), 2)
			if bitsTotQ8 <= rateDistQ8 {
				rateDistQ8 = bitsTotQ8
				resNrgQ15 = sum1 + penalty
				ind = k
				gainQ7 = gainTmp
			}
		}
	}

	return ind, resNrgQ15, rateDistQ8, gainQ7
}

// quantLTPGains quantizes the LTP taps for all subframes, returning the Q14
// filter coefficients, per-subframe codebook indices, the periodicity index,
// and the LTP prediction gain in dB. sumLogGainQ7 carries the cumulative gain
// limit across frames.
func (e *Encoder) quantLTPGains(
	xx, xX []float32, subfrLen, nbSubfr int,
) (ltpCoefQ14 []int16, cbkIndex []int8, periodicityIndex int, predGainDB float32) {
	xxQ17 := make([]int32, nbSubfr*ltpMatrixSize)
	xXQ17 := make([]int32, nbSubfr*ltpOrder)
	for i := range xxQ17 {
		xxQ17[i] = int32(math.RoundToEven(float64(xx[i]) * 131072.0))
	}
	for i := range xXQ17 {
		xXQ17[i] = int32(math.RoundToEven(float64(xX[i]) * 131072.0))
	}

	ltpCoefQ14 = make([]int16, nbSubfr*ltpOrder)
	cbkIndex = make([]int8, nbSubfr)
	tempIdx := make([]int8, nbSubfr)

	minRateDist := int32(math.MaxInt32)
	bestSumLogGain := int32(0)
	var lastResNrg int32
	for k := range nLTPCodebooks { //nolint:varnamelen // k indexes the codebook, as in the C reference.
		cb := ltpCodebook(k)
		cbGain := ltpGainTable(k)
		clQ5 := ltpBitsTable(k)
		size := ltpVQSizes[k]

		resNrg := int32(0)
		rateDist := int32(0)
		sumLogGainTmp := e.sumLogGainQ7
		for j := range nbSubfr {
			maxGainQ7 := log2lin((maxSumLogGainDBQ7-sumLogGainTmp)+(7<<7)) - ltpGainSafetyQ7
			ind, resNrgSubfr, rateDistSubfr, gainQ7 := vqWMatEC(
				xxQ17[j*ltpMatrixSize:], xXQ17[j*ltpOrder:], cb, cbGain, clQ5, subfrLen, maxGainQ7, size)
			tempIdx[j] = int8(ind) //nolint:gosec // G115
			resNrg = addPosSat32(resNrg, resNrgSubfr)
			rateDist = addPosSat32(rateDist, rateDistSubfr)
			sumLogGainTmp = max(0, sumLogGainTmp+lin2log(ltpGainSafetyQ7+gainQ7)-(7<<7))
		}
		if rateDist <= minRateDist {
			minRateDist = rateDist
			periodicityIndex = k
			copy(cbkIndex, tempIdx)
			bestSumLogGain = sumLogGainTmp
		}
		lastResNrg = resNrg
	}

	cb := ltpCodebook(periodicityIndex)
	for j := range nbSubfr {
		for k := range ltpOrder {
			ltpCoefQ14[j*ltpOrder+k] = int16(int32(cb[cbkIndex[j]][k]) << 7)
		}
	}

	if nbSubfr == 2 {
		lastResNrg >>= 1
	} else {
		lastResNrg >>= 2
	}
	e.sumLogGainQ7 = bestSumLogGain
	predGainDB = float32(smulbb(-3, lin2log(lastResNrg)-(15<<7))) / 128.0

	return ltpCoefQ14, cbkIndex, periodicityIndex, predGainDB
}
