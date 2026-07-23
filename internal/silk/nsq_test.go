// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNSQUnvoicedSmoke exercises the NSQ end to end for an unvoiced frame:
// it must run without panicking and produce a bounded, non-trivial pulse train.
// Bit-exact correctness is validated later end-to-end via opus_compare.
func TestNSQUnvoicedSmoke(t *testing.T) {
	const (
		fsKHz           = 16
		nbSubfr         = 4
		subfrLength     = 5 * fsKHz
		frameLength     = nbSubfr * subfrLength
		ltpMemLength    = 20 * fsKHz
		predictLPCOrder = 16
		shapingLPCOrder = 16
	)

	x16 := make([]int16, frameLength)
	for i := range x16 {
		x16[i] = int16(3000 * math.Sin(2*math.Pi*float64(i)/37))
	}
	pulses := make([]int8, frameLength)

	gains := make([]int32, nbSubfr)
	for k := range gains {
		gains[k] = 100 * 65536 // gain ~ signal scale so the excitation is non-degenerate
	}

	p := &nsqParams{ //nolint:varnamelen // p is the NSQ parameter block.
		predCoefQ12:      make([]int16, 2*maxPredictLPCOrder),
		ltpCoefQ14:       make([]int16, ltpOrder*nbSubfr),
		arQ13:            make([]int16, nbSubfr*maxShapeLPCOrder),
		harmShapeGainQ14: make([]int32, nbSubfr),
		tiltQ14:          make([]int32, nbSubfr),
		lfShpQ14:         make([]int32, nbSubfr),
		gainsQ16:         gains,
		pitchL:           make([]int, nbSubfr),
		lambdaQ10:        1024,
		ltpScaleQ14:      15565,
		seed:             1,
		signalType:       frameSignalTypeUnvoiced,
		quantOffsetType:  frameQuantizationOffsetTypeLow,
		nlsfInterpCoefQ2: 4,
		ltpMemLength:     ltpMemLength,
		frameLength:      frameLength,
		subfrLength:      subfrLength,
		nbSubfr:          nbSubfr,
		predictLPCOrder:  predictLPCOrder,
		shapingLPCOrder:  shapingLPCOrder,
	}

	nsq := newNSQState()
	require.NotPanics(t, func() {
		nsq.quantize(x16, pulses, p)
	})

	nonZero := 0
	for _, v := range pulses {
		if v != 0 {
			nonZero++
		}
	}
	assert.Positive(t, nonZero, "expected a non-trivial pulse train")
}

// TestNSQVoicedSmoke exercises the voiced signal path: the rewhitening
// re-filter (lpcAnalysisFilterFixed, otherwise never called), long-term
// prediction (ltpPredQ13), harmonic shaping (lag>0), the high quantizer
// offset, and the aggressive-RDO branch (lambdaQ10>2048) — none of which
// TestNSQUnvoicedSmoke reaches, since it's unvoiced with lag==0 and a low
// lambda.
func TestNSQVoicedSmoke(t *testing.T) {
	const (
		fsKHz           = 16
		nbSubfr         = 4
		subfrLength     = 5 * fsKHz
		frameLength     = nbSubfr * subfrLength
		ltpMemLength    = 20 * fsKHz
		predictLPCOrder = 16
		shapingLPCOrder = 16
		pitchLag        = 100
	)

	x16 := make([]int16, frameLength)
	for i := range x16 {
		x16[i] = int16(3000 * math.Sin(2*math.Pi*float64(i)/37))
	}
	pulses := make([]int8, frameLength)

	// Gains vary per subframe (not just from the initial state) so that the
	// gain-change rescale in scaleStates also fires on subframe 1, which is
	// voiced but not a rewhite subframe (only k==0 rewhites here) — the only
	// combination that exercises the sLTPQ15 rescale loop.
	gains := []int32{100 * 65536, 150 * 65536, 150 * 65536, 150 * 65536}
	ltpCoefQ14 := make([]int16, ltpOrder*nbSubfr)
	for i := range ltpCoefQ14 {
		ltpCoefQ14[i] = int16(2000 + 100*i%500) //nolint:gosec // G115: small bounded synthetic LTP taps.
	}
	pitchL := make([]int, nbSubfr)
	for k := range pitchL {
		pitchL[k] = pitchLag
	}
	harmShapeGainQ14 := make([]int32, nbSubfr)
	for k := range harmShapeGainQ14 {
		harmShapeGainQ14[k] = 8000
	}

	p := &nsqParams{ //nolint:varnamelen // p is the NSQ parameter block.
		predCoefQ12:      make([]int16, 2*maxPredictLPCOrder),
		ltpCoefQ14:       ltpCoefQ14,
		arQ13:            make([]int16, nbSubfr*maxShapeLPCOrder),
		harmShapeGainQ14: harmShapeGainQ14,
		tiltQ14:          make([]int32, nbSubfr),
		lfShpQ14:         make([]int32, nbSubfr),
		gainsQ16:         gains,
		pitchL:           pitchL,
		lambdaQ10:        4096, // >2048: exercises the aggressive-RDO branch.
		ltpScaleQ14:      15565,
		seed:             1,
		signalType:       frameSignalTypeVoiced,
		quantOffsetType:  frameQuantizationOffsetTypeHigh,
		nlsfInterpCoefQ2: 4,
		ltpMemLength:     ltpMemLength,
		frameLength:      frameLength,
		subfrLength:      subfrLength,
		nbSubfr:          nbSubfr,
		predictLPCOrder:  predictLPCOrder,
		shapingLPCOrder:  shapingLPCOrder,
	}

	nsq := newNSQState()
	require.NotPanics(t, func() {
		nsq.quantize(x16, pulses, p)
	})

	nonZero := 0
	for _, v := range pulses {
		if v != 0 {
			nonZero++
		}
	}
	assert.Positive(t, nonZero, "expected a non-trivial pulse train")
	assert.Equal(t, pitchLag, nsq.lagPrev, "lagPrev should track the last subframe's pitch lag")
}
