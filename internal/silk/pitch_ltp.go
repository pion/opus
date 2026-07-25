// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// encodePitchLags codes the primary pitch lag and the subframe pitch contour
// index, inverting Decoder.decodePitchLags. It is only called for voiced
// frames. lag is the primary pitch lag; contourIndex selects the contour VQ.
func (e *Encoder) encodePitchLags(
	lag int,
	contourIndex uint32,
	bandwidth Bandwidth,
	nanoseconds int,
	isFirstSilkFrameInOpusFrame bool,
) {
	lagAbsolute := isFirstSilkFrameInOpusFrame || !e.isPreviousFrameVoiced
	lowPartICDF, lagScale, lagMin, _ := pitchLagCodebooks(bandwidth)

	switch {
	case lagAbsolute:
		e.encodeAbsolutePitchLag(lag, lowPartICDF, lagScale, lagMin)
	default:
		delta := lag - e.previousLag + 9
		if delta >= 1 && delta <= len(icdfPrimaryPitchLagChange)-2 {
			e.rangeEncoder.EncodeSymbolWithICDF(icdfPrimaryPitchLagChange, uint32(delta)) //nolint:gosec // G115
		} else {
			e.rangeEncoder.EncodeSymbolWithICDF(icdfPrimaryPitchLagChange, 0)
			e.encodeAbsolutePitchLag(lag, lowPartICDF, lagScale, lagMin)
		}
	}
	e.previousLag = lag

	_, lagIcdf := pitchContourCodebooks(bandwidth, nanoseconds)
	e.rangeEncoder.EncodeSymbolWithICDF(lagIcdf, contourIndex)
}

// encodeAbsolutePitchLag codes the primary lag as a high and low part.
func (e *Encoder) encodeAbsolutePitchLag(lag int, lowPartICDF []uint, lagScale, lagMin uint32) {
	rel := uint32(lag) - lagMin //nolint:gosec // G115
	e.rangeEncoder.EncodeSymbolWithICDF(icdfPrimaryPitchLagHighPart, rel/lagScale)
	e.rangeEncoder.EncodeSymbolWithICDF(lowPartICDF, rel%lagScale)
}

// encodeLTPFilter codes the periodicity index and the per-subframe LTP filter
// index, inverting Decoder.decodeLTPFilterCoefficients.
func (e *Encoder) encodeLTPFilter(periodicityIndex uint32, filterIndices []uint32) {
	e.rangeEncoder.EncodeSymbolWithICDF(icdfPeriodicityIndex, periodicityIndex)
	filterICDF := ltpFilterIndexICDF(periodicityIndex)
	for _, index := range filterIndices {
		e.rangeEncoder.EncodeSymbolWithICDF(filterICDF, index)
	}
}

// ltpFilterIndexICDF returns the filter-index PDF for a periodicity index.
func ltpFilterIndexICDF(periodicityIndex uint32) []uint {
	switch periodicityIndex {
	case 1:
		return icdfLTPFilterIndex1
	case 2:
		return icdfLTPFilterIndex2
	default:
		return icdfLTPFilterIndex0
	}
}

// encodeLTPScaling codes the LTP scaling index. The caller emits it only for
// the first voiced SILK frame of an Opus frame.
func (e *Encoder) encodeLTPScaling(scaleIndex uint32) {
	e.rangeEncoder.EncodeSymbolWithICDF(icdfLTPScalingParameter, scaleIndex)
}

// encodeLCGSeed codes the excitation seed (RFC 6716 Section 4.2.7.7).
func (e *Encoder) encodeLCGSeed(seed uint32) {
	e.rangeEncoder.EncodeSymbolWithICDF(icdfLinearCongruentialGeneratorSeed, seed)
}
