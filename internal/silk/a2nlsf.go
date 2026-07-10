// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import "math"

// LPC-to-NLSF conversion (silk_A2NLSF, A2NLSF.c). The whitening filter's even
// and odd polynomials are root-found on a cosine grid to recover the NLSFs.

const (
	binDivStepsA2NLSF = 3
	maxIterA2NLSF     = 16
	lsfCosTabSz       = 128
)

// silkLSFCosTabFIXQ12 is the Q12 cosine table used for the root search.
//
//nolint:gochecknoglobals
var silkLSFCosTabFIXQ12 = [lsfCosTabSz + 1]int16{
	8192, 8190, 8182, 8170, 8152, 8130, 8104, 8072, 8034, 7994, 7946, 7896,
	7840, 7778, 7714, 7644, 7568, 7490, 7406, 7318, 7226, 7128, 7026, 6922,
	6812, 6698, 6580, 6458, 6332, 6204, 6070, 5934, 5792, 5648, 5502, 5352,
	5198, 5040, 4880, 4718, 4552, 4382, 4212, 4038, 3862, 3684, 3502, 3320,
	3136, 2948, 2760, 2570, 2378, 2186, 1990, 1794, 1598, 1400, 1202, 1002,
	802, 602, 402, 202, 0, -202, -402, -602, -802, -1002, -1202, -1400,
	-1598, -1794, -1990, -2186, -2378, -2570, -2760, -2948, -3136, -3320,
	-3502, -3684, -3862, -4038, -4212, -4382, -4552, -4718, -4880, -5040,
	-5198, -5352, -5502, -5648, -5792, -5934, -6070, -6204, -6332, -6458,
	-6580, -6698, -6812, -6922, -7026, -7128, -7226, -7318, -7406, -7490,
	-7568, -7644, -7714, -7778, -7840, -7896, -7946, -7994, -8034, -8072,
	-8104, -8130, -8152, -8170, -8182, -8190, -8192,
}

// smlaww returns a + ((b * c) >> 16).
func smlaww(a, b, c int32) int32 {
	return a + smulww(b, c)
}

// a2nlsfTransPoly transforms a polynomial from cos(n*f) to cos(f)^n.
func a2nlsfTransPoly(p []int32, dd int) {
	for k := 2; k <= dd; k++ {
		for n := dd; n > k; n-- {
			p[n-2] -= p[n]
		}
		p[k-2] -= p[k] << 1
	}
}

// a2nlsfEvalPoly evaluates a Q16 polynomial at the Q12 point x.
func a2nlsfEvalPoly(p []int32, x int32, dd int) int32 {
	y32 := p[dd]
	xQ16 := x << 4
	for n := dd - 1; n >= 0; n-- {
		y32 = smlaww(p[n], y32, xQ16)
	}

	return y32
}

// a2nlsfInit builds the even (P) and odd (Q) polynomials from the LPC coefs.
func a2nlsfInit(aQ16, p, q []int32, dd int) {
	p[dd] = 1 << 16
	q[dd] = 1 << 16
	for k := range dd {
		p[k] = -aQ16[dd-k-1] - aQ16[dd+k]
		q[k] = -aQ16[dd-k-1] + aQ16[dd+k]
	}
	for k := dd; k > 0; k-- {
		p[k-1] -= p[k]
		q[k-1] += q[k]
	}
	a2nlsfTransPoly(p, dd)
	a2nlsfTransPoly(q, dd)
}

// bwexpander32 applies a Q16 chirp to int32 AR coefficients.
func bwexpander32(ar []int32, d int, chirpQ16 int32) {
	chirpMinusOneQ16 := chirpQ16 - 65536
	for i := range d - 1 {
		ar[i] = smulww(chirpQ16, ar[i])
		chirpQ16 += rshiftRound32(chirpQ16*chirpMinusOneQ16, 16)
	}
	ar[d-1] = smulww(chirpQ16, ar[d-1])
}

// a2nlsf converts monic whitening filter coefficients (Q16, modified in place)
// to Q15 NLSFs, bandwidth-expanding until the roots converge.
//
//nolint:gocognit,gocyclo,cyclop,maintidx,varnamelen // faithful port of silk_A2NLSF.
func a2nlsf(nlsf []int16, aQ16 []int32, d int) {
	dd := d >> 1
	pPoly := make([]int32, dd+1)
	qPoly := make([]int32, dd+1)
	a2nlsfInit(aQ16, pPoly, qPoly, dd)
	polys := [2][]int32{pPoly, qPoly}

	p := pPoly //nolint:varnamelen // p tracks the active P/Q polynomial, as in the C reference.
	xlo := int32(silkLSFCosTabFIXQ12[0])
	ylo := a2nlsfEvalPoly(p, xlo, dd)

	var rootIx int
	if ylo < 0 {
		nlsf[0] = 0
		p = qPoly
		ylo = a2nlsfEvalPoly(p, xlo, dd)
		rootIx = 1
	}

	k := 1 //nolint:varnamelen // k indexes the cosine grid, as in the C reference.
	iterations := 0
	thr := int32(0)
	for {
		xhi := int32(silkLSFCosTabFIXQ12[k])
		yhi := a2nlsfEvalPoly(p, xhi, dd)

		if (ylo <= 0 && yhi >= thr) || (ylo >= 0 && yhi <= -thr) { //nolint:nestif // faithful port of silk_A2NLSF.
			if yhi == 0 {
				thr = 1
			} else {
				thr = 0
			}
			ffrac := int32(-256)
			for m := range binDivStepsA2NLSF {
				xmid := rshiftRound32(xlo+xhi, 1)
				ymid := a2nlsfEvalPoly(p, xmid, dd)
				if (ylo <= 0 && ymid >= 0) || (ylo >= 0 && ymid <= 0) {
					xhi = xmid
					yhi = ymid
				} else {
					xlo = xmid
					ylo = ymid
					ffrac += 128 >> m
				}
			}
			if absInt32(ylo) < 65536 {
				den := ylo - yhi
				nom := (ylo << (8 - binDivStepsA2NLSF)) + (den >> 1)
				if den != 0 {
					ffrac += nom / den
				}
			} else {
				ffrac += ylo / ((ylo - yhi) >> (8 - binDivStepsA2NLSF))
			}
			nlsf[rootIx] = int16(min((int32(k)<<8)+ffrac, math.MaxInt16))

			rootIx++
			if rootIx >= d {
				return
			}
			p = polys[rootIx&1]
			xlo = int32(silkLSFCosTabFIXQ12[k-1])
			ylo = int32(1-(rootIx&2)) << 12
		} else {
			k++
			xlo = xhi
			ylo = yhi
			thr = 0

			if k > lsfCosTabSz {
				iterations++
				if iterations > maxIterA2NLSF {
					nlsf[0] = int16((1 << 15) / int32(d+1)) //nolint:gosec // G115: quotient fits int16.
					for k = 1; k < d; k++ {
						nlsf[k] = nlsf[k-1] + nlsf[0]
					}

					return
				}
				bwexpander32(aQ16, d, 65536-(1<<iterations))
				a2nlsfInit(aQ16, pPoly, qPoly, dd)
				p = pPoly
				xlo = int32(silkLSFCosTabFIXQ12[0])
				ylo = a2nlsfEvalPoly(p, xlo, dd)
				if ylo < 0 {
					nlsf[0] = 0
					p = qPoly
					ylo = a2nlsfEvalPoly(p, xlo, dd)
					rootIx = 1
				} else {
					rootIx = 0
				}
				k = 1
			}
		}
	}
}

// a2nlsfFLP converts float LPC coefficients to Q15 NLSFs (silk_A2NLSF_FLP).
func a2nlsfFLP(nlsfQ15 []int16, pAR []float32, order int) {
	aFixQ16 := make([]int32, order)
	for i := range order {
		aFixQ16[i] = int32(math.RoundToEven(float64(pAR[i] * 65536.0)))
	}
	a2nlsf(nlsfQ15, aFixQ16, order)
}
