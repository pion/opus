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
		predCoefQ12:      make([]int16, 2*maxLPCOrder),
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
