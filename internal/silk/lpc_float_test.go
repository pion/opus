// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutocorrelationFLP(t *testing.T) {
	in := []float32{1, 2, 3, 4}
	results := make([]float32, 3)
	autocorrelationFLP(results, in, len(in), 3)

	assert.InDelta(t, 1+4+9+16, results[0], 1e-4)    // sum of squares
	assert.InDelta(t, 1*2+2*3+3*4, results[1], 1e-4) // lag 1
	assert.InDelta(t, 1*3+2*4, results[2], 1e-4)     // lag 2
}

func TestBwexpanderFLP(t *testing.T) {
	ar := []float32{1, 1, 1, 1}
	bwexpanderFLP(ar, len(ar), 0.5)
	assert.InDelta(t, 0.5, ar[0], 1e-6)
	assert.InDelta(t, 0.25, ar[1], 1e-6)
	assert.InDelta(t, 0.125, ar[2], 1e-6)
	assert.InDelta(t, 0.0625, ar[3], 1e-6)
}

func TestK2ASingleCoefficient(t *testing.T) {
	a := make([]float32, 1)
	k2aFLP(a, []float32{0.5}, 1)
	assert.InDelta(t, -0.5, a[0], 1e-6)
}

// TestSchurThenK2A checks the Schur/k2a pair reproduces a known stable AR
// filter from its autocorrelation.
func TestSchurThenK2A(t *testing.T) {
	// AR(1) process x[n] = a*x[n-1] + e: autocorr r[k] = r[0]*a^|k|.
	const a = 0.8 //nolint:varnamelen // a is the AR(1) coefficient in the test model.
	order := 4
	autoCorr := make([]float32, order+1)
	for k := range autoCorr {
		autoCorr[k] = float32(math.Pow(a, float64(k)))
	}

	refl := make([]float32, order)
	resNrg := schurFLP(refl, autoCorr, order)

	// First reflection coefficient of an AR(1) process is -a.
	assert.InDelta(t, -a, refl[0], 1e-4)
	assert.Greater(t, resNrg, float32(0))
	assert.LessOrEqual(t, resNrg, autoCorr[0])

	coef := make([]float32, order)
	k2aFLP(coef, refl, order)
	// Recovered LPC: first tap ~ a, the rest ~ 0.
	assert.InDelta(t, a, coef[0], 1e-3)
	for k := 1; k < order; k++ {
		assert.InDelta(t, 0, coef[k], 1e-3)
	}
}

func TestApplySineWindowFLP(t *testing.T) {
	length := 16
	px := make([]float32, length)
	for i := range px {
		px[i] = 1
	}

	rising := make([]float32, length)
	applySineWindowFLP(rising, px, 1, length)
	// Rising window: starts near 0, increases, ends near the peak.
	assert.InDelta(t, 0, rising[0], 0.1)
	assert.Greater(t, rising[length-1], rising[0])
	for _, v := range rising {
		assert.GreaterOrEqual(t, v, float32(-1e-3))
		assert.LessOrEqual(t, v, float32(1.001))
	}

	falling := make([]float32, length)
	applySineWindowFLP(falling, px, 2, length)
	// Falling window: starts near the peak, ends near 0.
	assert.Greater(t, falling[0], falling[length-1])
	assert.InDelta(t, 0, falling[length-1], 0.1)
}

func TestLPCAnalysisFilterFLP(t *testing.T) {
	s := []float32{1, 2, 3, 4, 5, 6, 7, 8} //nolint:varnamelen // s is the input signal.
	order := 2
	r := make([]float32, len(s)) //nolint:varnamelen // r is the residual buffer.

	// Zero predictor: residual equals the input past the first order samples.
	lpcAnalysisFilterFLP(r, []float32{0, 0}, s, len(s), order)
	for i := range order {
		assert.Equal(t, float32(0), r[i])
	}
	for i := order; i < len(s); i++ {
		assert.InDelta(t, s[i], r[i], 1e-6)
	}

	// A known predictor removes its own prediction.
	coef := []float32{1.5, -0.5}
	lpcAnalysisFilterFLP(r, coef, s, len(s), order)
	for i := order; i < len(s); i++ {
		want := s[i] - (coef[0]*s[i-1] + coef[1]*s[i-2])
		require.InDelta(t, want, r[i], 1e-4)
	}
}
