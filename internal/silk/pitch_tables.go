// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// Pitch estimator constants and codebooks (pitch_est_defines.h,
// pitch_est_tables.c).
const (
	peMaxNBSubfr        = 4
	peSubfrLengthMS     = 5
	peLTPMemLengthMS    = 20
	peMaxFSKHz          = 16
	peMaxLagMS          = 18
	peMinLagMS          = 2
	peMaxLag            = peMaxLagMS * peMaxFSKHz
	peDSrchLength       = 24
	peNBStage3Lags      = 5
	peNBCbksStage2      = 3
	peNBCbksStage2Ext   = 11
	peNBCbksStage2_10ms = 3
	peNBCbksStage3Max   = 34
	peNBCbksStage3_10ms = 12
	silkPEMaxComplex    = 2
	scratchSizePitch    = 22

	peShortlagBias    = 0.2
	pePrevlagBias     = 0.2
	peFlatcontourBias = 0.05
)

//nolint:gochecknoglobals // pitch codebook tables from pitch_est_tables.c.
var (
	silkCBLagsStage2_10ms = [peMaxNBSubfr >> 1][]int8{
		{0, 1, 0},
		{0, 0, 1},
	}

	silkCBLagsStage3_10ms = [peMaxNBSubfr >> 1][]int8{
		{0, 0, 1, -1, 1, -1, 2, -2, 2, -2, 3, -3},
		{0, 1, 0, 1, -1, 2, -1, 2, -2, 3, -2, 3},
	}

	silkLagRangeStage3_10ms = [peMaxNBSubfr >> 1][2]int8{
		{-3, 7},
		{-2, 7},
	}

	silkCBLagsStage2 = [peMaxNBSubfr][]int8{
		{0, 2, -1, -1, -1, 0, 0, 1, 1, 0, 1},
		{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0},
		{0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
		{0, -1, 2, 1, 0, 1, 1, 0, 0, -1, -1},
	}

	silkCBLagsStage3 = [peMaxNBSubfr][]int8{
		{0, 0, 1, -1, 0, 1, -1, 0, -1, 1, -2, 2, -2, -2, 2, -3, 2, 3, -3, -4, 3, -4, 4, 4, -5, 5, -6, -5, 6, -7, 6, 5, 8, -9},
		{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, -1, 1, 0, 0, 1, -1, 0, 1, -1, -1, 1, -1, 2, 1, -1, 2, -2, -2, 2, -2, 2, 2, 3, -3},
		{0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1, -1, 1, 0, 0, 2, 1, -1, 2, -1, -1, 2, -1, 2, 2, -1, 3, -2, -2, -2, 3},
		{0, 1, 0, 0, 1, 0, 1, -1, 2, -1, 2, -1, 2, 3, -2, 3, -2, -2, 4, 4, -3, 5, -3, -4, 6, -4, 6, 5, -5, 8, -6, -5, -7, 9},
	}

	silkLagRangeStage3 = [silkPEMaxComplex + 1][peMaxNBSubfr][2]int8{
		{{-5, 8}, {-1, 6}, {-1, 6}, {-4, 10}},
		{{-6, 10}, {-2, 6}, {-1, 6}, {-5, 10}},
		{{-9, 12}, {-3, 7}, {-2, 7}, {-7, 13}},
	}

	silkNbCbkSearchsStage3 = [silkPEMaxComplex + 1]int8{16, 24, 34}
)
