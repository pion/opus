// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodePitchLagsRoundTrip(t *testing.T) {
	configs := []struct {
		bandwidth   Bandwidth
		nanoseconds int
	}{
		{BandwidthNarrowband, nanoseconds10Ms},
		{BandwidthNarrowband, nanoseconds20Ms},
		{BandwidthMediumband, nanoseconds10Ms},
		{BandwidthMediumband, nanoseconds20Ms},
		{BandwidthWideband, nanoseconds10Ms},
		{BandwidthWideband, nanoseconds20Ms},
	}

	for _, cfg := range configs {
		_, _, lagMin, lagMax := pitchLagCodebooks(cfg.bandwidth)
		lagCb, lagIcdf := pitchContourCodebooks(cfg.bandwidth, cfg.nanoseconds)
		contourMax := len(lagIcdf) - 2

		scenarios := []struct {
			name       string
			isFirst    bool
			prevVoiced bool
			prevLag    int
			lag        int
		}{
			{"absolute", true, false, 100, int(lagMin) + 10},
			{"relative_near", false, true, int(lagMin) + 50, int(lagMin) + 52},
			{"relative_escape", false, true, int(lagMin) + 10, int(lagMax) - 5},
		}

		for _, sc := range scenarios {
			for _, contourIndex := range []uint32{0, uint32(contourMax / 2), uint32(contourMax)} { //nolint:gosec
				name := fmt.Sprintf("bw%d_ns%d_%s_c%d", cfg.bandwidth, cfg.nanoseconds, sc.name, contourIndex)
				t.Run(name, func(t *testing.T) {
					enc := NewEncoder()
					enc.previousLag = sc.prevLag
					enc.isPreviousFrameVoiced = sc.prevVoiced
					enc.rangeEncoder.Init()
					enc.encodePitchLags(sc.lag, contourIndex, cfg.bandwidth, cfg.nanoseconds, sc.isFirst)
					encRange := enc.rangeEncoder.FinalRange()
					data := enc.rangeEncoder.Done()

					dec := NewDecoder()
					dec.previousLag = sc.prevLag
					dec.isPreviousFrameVoiced = sc.prevVoiced
					dec.rangeDecoder.Init(data)
					_, pitchLags := dec.decodePitchLags(frameSignalTypeVoiced, cfg.bandwidth, cfg.nanoseconds, sc.isFirst)

					require.Len(t, pitchLags, subframeCount(cfg.nanoseconds))
					for k := range pitchLags {
						want := clamp(int32(lagMin), int32(sc.lag+int(lagCb[contourIndex][k])), int32(lagMax)) //nolint:gosec
						require.Equalf(t, int(want), pitchLags[k], "subframe %d", k)
					}
					require.Equal(t, sc.lag, dec.previousLag)
					assert.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
				})
			}
		}
	}
}

func TestEncodeLTPFilterRoundTrip(t *testing.T) {
	codebooks := [][][]int8{
		codebookLTPFilterPeriodicityIndex0,
		codebookLTPFilterPeriodicityIndex1,
		codebookLTPFilterPeriodicityIndex2,
	}

	for periodicity := range uint32(3) {
		codebook := codebooks[periodicity]
		for _, subframes := range []int{2, 4} {
			name := fmt.Sprintf("p%d_sf%d", periodicity, subframes)
			t.Run(name, func(t *testing.T) {
				filterIndices := make([]uint32, subframes)
				for i := range filterIndices {
					filterIndices[i] = uint32((i*3 + 1) % len(codebook)) //nolint:gosec
				}

				enc := NewEncoder()
				enc.rangeEncoder.Init()
				enc.encodeLTPFilter(periodicity, filterIndices)
				encRange := enc.rangeEncoder.FinalRange()
				data := enc.rangeEncoder.Done()

				dec := NewDecoder()
				dec.rangeDecoder.Init(data)
				bQ7 := dec.decodeLTPFilterCoefficients(frameSignalTypeVoiced, subframes)

				require.Len(t, bQ7, subframes)
				for i := range filterIndices {
					require.Equalf(t, codebook[filterIndices[i]], []int8(bQ7[i]), "subframe %d", i)
				}
				assert.Equal(t, encRange, dec.rangeDecoder.FinalRange(), "range coder desync")
			})
		}
	}
}

func TestEncodeLTPScalingRoundTrip(t *testing.T) {
	want := []float32{15565, 12288, 8192}
	for scaleIndex := range uint32(3) {
		enc := NewEncoder()
		enc.rangeEncoder.Init()
		enc.encodeLTPScaling(scaleIndex)
		encRange := enc.rangeEncoder.FinalRange()
		data := enc.rangeEncoder.Done()

		dec := NewDecoder()
		dec.rangeDecoder.Init(data)
		got := dec.decodeLTPScalingParameter(frameSignalTypeVoiced, true)

		require.Equal(t, want[scaleIndex], got)
		assert.Equal(t, encRange, dec.rangeDecoder.FinalRange())
	}
}

func TestEncodeLCGSeedRoundTrip(t *testing.T) {
	for seed := range uint32(4) {
		enc := NewEncoder()
		enc.rangeEncoder.Init()
		enc.encodeLCGSeed(seed)
		encRange := enc.rangeEncoder.FinalRange()
		data := enc.rangeEncoder.Done()

		dec := NewDecoder()
		dec.rangeDecoder.Init(data)
		got := dec.decodeLinearCongruentialGeneratorSeed()

		require.Equal(t, seed, got)
		assert.Equal(t, encRange, dec.rangeDecoder.FinalRange())
	}
}
