// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

// Mode describes the static 48 kHz CELT mode from RFC 6716 Section 4.3.
type Mode struct {
	sampleRate            int
	shortBlockSampleCount int
	maxLM                 int
	bandEdges             []int16
}

var defaultMode = Mode{ //nolint:gochecknoglobals
	sampleRate:            sampleRate,
	shortBlockSampleCount: shortBlockSampleCount,
	maxLM:                 maxLM,
	bandEdges:             bandEdges[:],
}

// DefaultMode returns the static 48 kHz CELT mode used by Opus.
func DefaultMode() *Mode {
	return &defaultMode
}

// SampleRate returns the CELT synthesis sample rate.
func (m *Mode) SampleRate() int {
	return m.sampleRate
}

// BandCount returns the number of CELT energy bands.
func (m *Mode) BandCount() int {
	return len(m.bandEdges) - 1
}

// MaxLM returns the maximum CELT frame-size shift.
func (m *Mode) MaxLM() int {
	return m.maxLM
}

// ShortBlockSampleCount returns the CELT 2.5 ms block size at 48 kHz.
func (m *Mode) ShortBlockSampleCount() int {
	return m.shortBlockSampleCount
}

// FrameSampleCount returns the sample count per channel for a CELT LM.
// RFC 6716 Section 4.3.3 defines LM as log2(frame_size/120).
func (m *Mode) FrameSampleCount(lm int) (int, error) {
	if lm < 0 || lm > m.maxLM {
		return 0, errInvalidLM
	}

	return m.shortBlockSampleCount << lm, nil
}

// LMForFrameSampleCount maps a sample count per channel to a CELT LM.
func (m *Mode) LMForFrameSampleCount(frameSampleCount int) (int, error) {
	for lm := 0; lm <= m.maxLM; lm++ {
		if frameSampleCount == m.shortBlockSampleCount<<lm {
			return lm, nil
		}
	}

	return 0, errInvalidFrameSize
}

// BandEdges returns the RFC 6716 Table 55 MDCT bin edges scaled for a CELT LM.
func (m *Mode) BandEdges(lm int) ([]int, error) {
	if lm < 0 || lm > m.maxLM {
		return nil, errInvalidLM
	}

	edges := make([]int, len(m.bandEdges))
	for i, edge := range m.bandEdges {
		edges[i] = int(edge) << lm
	}

	return edges, nil
}

// BandWidth returns the number of MDCT bins in one energy band for a CELT LM.
func (m *Mode) BandWidth(band, lm int) (int, error) {
	if band < 0 || band >= m.BandCount() {
		return 0, errInvalidBand
	}

	edges, err := m.BandEdges(lm)
	if err != nil {
		return 0, err
	}

	return edges[band+1] - edges[band], nil
}

// BandRangeForSampleRate returns the coded CELT band range for an Opus bandwidth sample rate.
// The end bands come from the RFC 6716 Section 4.3/Table 55 band stop frequencies.
func (m *Mode) BandRangeForSampleRate(sampleRate int) (startBand, endBand int, err error) {
	switch sampleRate {
	case 8000:
		return 0, 13, nil
	case 16000:
		return 0, 17, nil
	case 24000:
		return 0, 19, nil
	case 48000:
		return 0, m.BandCount(), nil
	default:
		return 0, 0, errInvalidSampleRate
	}
}

// HybridBandRange returns the CELT band range used by hybrid modes.
// RFC 6716 Section 4.3 leaves bands below band 17 to SILK in hybrid modes.
func (m *Mode) HybridBandRange(sampleRate int) (startBand, endBand int, err error) {
	_, endBand, err = m.BandRangeForSampleRate(sampleRate)
	if err != nil {
		return 0, 0, err
	}
	if endBand <= hybridStartBand {
		return 0, 0, errInvalidSampleRate
	}

	return hybridStartBand, endBand, nil
}
