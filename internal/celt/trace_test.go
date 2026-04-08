// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"testing"

	"github.com/pion/opus/internal/rangecoding"
	"github.com/stretchr/testify/require"
)

type rangeCheckpoint struct {
	name          string
	tell          uint
	tellFrac      uint
	remainingBits int
	finalRange    uint32
}

type rangeTrace struct {
	t            testing.TB
	rangeDecoder *rangecoding.Decoder
}

func newRangeTrace(tb testing.TB, decoder *Decoder) rangeTrace {
	tb.Helper()

	return rangeTrace{
		t:            tb,
		rangeDecoder: &decoder.rangeDecoder,
	}
}

func (r rangeTrace) require(expected rangeCheckpoint) {
	r.t.Helper()

	got := rangeCheckpoint{
		name:          expected.name,
		tell:          r.rangeDecoder.Tell(),
		tellFrac:      r.rangeDecoder.TellFrac(),
		remainingBits: r.rangeDecoder.RemainingBits(),
		finalRange:    r.rangeDecoder.FinalRange(),
	}

	require.Equal(r.t, expected, got)
}
