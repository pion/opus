// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

const (
	ltpOrder       = 5
	ltpCorrInvMax  = 0.03
	ltpMatrixSize  = ltpOrder * ltpOrder
	ltpHalfOrder   = ltpOrder / 2
	ltpLastDiagIdx = ltpMatrixSize - 1
)

// corrVectorFLP computes the cross-correlation X'*t (silk_corrVector_FLP).
func corrVectorFLP(x, t []float32, l, order int, xt []float32) {
	for lag := range order {
		xt[lag] = float32(innerProductFLP(x[order-1-lag:], t, l))
	}
}

// corrMatrixFLP computes the symmetric correlation matrix X'*X, stored row-major
// (silk_corrMatrix_FLP).
func corrMatrixFLP(x []float32, l, order int, xx []float32) {
	p1 := order - 1
	energy := energyFLP(x[p1:], l)
	xx[0] = float32(energy)
	for j := 1; j < order; j++ {
		energy += float64(x[p1-j])*float64(x[p1-j]) - float64(x[p1+l-j])*float64(x[p1+l-j])
		xx[j*order+j] = float32(energy)
	}

	p2 := order - 2
	for lag := 1; lag < order; lag++ {
		energy = innerProductFLP(x[p1:], x[p2:], l)
		xx[lag*order] = float32(energy)
		xx[lag] = float32(energy)
		for j := 1; j < order-lag; j++ {
			energy += float64(x[p1-j])*float64(x[p2-j]) - float64(x[p1+l-j])*float64(x[p2+l-j])
			xx[(lag+j)*order+j] = float32(energy)
			xx[j*order+(lag+j)] = float32(energy)
		}
		p2--
	}
}

// scaleVectorFLP multiplies each element by gain.
func scaleVectorFLP(data []float32, gain float32, size int) {
	for i := range size {
		data[i] *= gain
	}
}

// findLTPFLP builds the normalized LTP correlation matrices (XX) and vectors
// (xX) for each subframe (silk_find_LTP_FLP). r is the LPC residual and rOffset
// points at the start of the current frame within it (with history before).
func findLTPFLP(xx, xX, r []float32, rOffset int, lag []int, subfrLength, nbSubfr int) {
	xxPtr, xXPtr, rPtr := 0, 0, rOffset
	for k := range nbSubfr {
		lagPtr := rPtr - (lag[k] + ltpHalfOrder)
		corrMatrixFLP(r[lagPtr:], subfrLength, ltpOrder, xx[xxPtr:])
		corrVectorFLP(r[lagPtr:], r[rPtr:], subfrLength, ltpOrder, xX[xXPtr:])

		energy := float32(energyFLP(r[rPtr:], subfrLength+ltpOrder))
		temp := 1.0 / max(energy, ltpCorrInvMax*0.5*(xx[xxPtr]+xx[xxPtr+ltpLastDiagIdx])+1.0)
		scaleVectorFLP(xx[xxPtr:], temp, ltpMatrixSize)
		scaleVectorFLP(xX[xXPtr:], temp, ltpOrder)

		rPtr += subfrLength
		xxPtr += ltpMatrixSize
		xXPtr += ltpOrder
	}
}
