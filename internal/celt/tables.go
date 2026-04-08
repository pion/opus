// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

// bandEdges are the 2.5 ms CELT band edges from RFC 6716 Table 55.
var bandEdges = [...]int16{ //nolint:gochecknoglobals
	0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 16, 20, 24, 28, 32, 40, 48, 60, 78, 100,
}

// bandAllocation is the static CELT allocation table from RFC 6716 Table 57.
// Rows are energy bands, columns are allocation vectors 0 through 10.
var bandAllocation = [maxBands][11]uint8{ //nolint:gochecknoglobals
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	{90, 80, 75, 69, 63, 56, 49, 40, 34, 29, 20},
	{110, 100, 90, 84, 78, 71, 65, 58, 51, 45, 39},
	{118, 110, 103, 93, 86, 80, 75, 71, 65, 60, 54},
	{126, 119, 112, 104, 95, 89, 83, 80, 76, 70, 65},
	{134, 128, 120, 114, 103, 97, 91, 88, 83, 77, 72},
	{144, 137, 129, 124, 113, 107, 101, 97, 92, 86, 83},
	{152, 145, 137, 132, 123, 117, 111, 107, 102, 96, 93},
	{162, 154, 147, 142, 133, 127, 121, 117, 112, 106, 103},
	{172, 164, 157, 152, 143, 137, 131, 127, 122, 116, 113},
	{200, 200, 198, 194, 183, 177, 171, 167, 162, 156, 153},
	{200, 200, 200, 200, 198, 194, 188, 183, 179, 173, 168},
	{200, 200, 200, 200, 200, 200, 199, 194, 190, 185, 180},
	{200, 200, 200, 200, 200, 200, 200, 200, 199, 194, 190},
	{200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200},
	{200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200},
	{200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200},
	{200, 200, 200, 200, 200, 200, 200, 200, 198, 193, 188},
	{200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200},
	{200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200},
	{200, 200, 200, 200, 200, 200, 200, 200, 198, 193, 188},
}

// These ICDFs mirror the CELT symbol PDFs from RFC 6716 Table 56 and the
// decoder subsections it references, including Section 4.3.3/Table 58.
// nolint:gochecknoglobals,unused
var (
	icdfSilence        = []uint{32768, 32767, 32768}
	icdfPostFilter     = []uint{2, 1, 2}
	icdfTransient      = []uint{8, 7, 8}
	icdfTapset         = []uint{4, 2, 3, 4}
	icdfTFChange       = []uint{4, 3, 4}
	icdfTFChangeLong   = []uint{16, 15, 16}
	icdfSpread         = []uint{32, 7, 9, 30, 32}
	icdfAllocationTrim = []uint{128, 2, 4, 9, 19, 41, 87, 109, 119, 124, 126, 128}
)
