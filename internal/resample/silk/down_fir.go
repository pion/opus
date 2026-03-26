// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

func (r *Resampler) resamplerPrivateDownFIR(out, in []int16) {
	buf := make([]int32, r.batchSize+maxFIROrder)
	copy(buf, r.sFIR[:r.firOrder])

	firCoefs := r.coefs[2:]
	outIndex := 0
	nSamplesIn := 0
	for len(in) > 0 {
		nSamplesIn = min(len(in), r.batchSize)

		silkResamplerPrivateAR2(r.sIIR[:2], buf[r.firOrder:], in[:nSamplesIn], r.coefs)

		maxIndexQ16 := int32(nSamplesIn) << 16 // #nosec G115
		outIndex += r.resamplerPrivateDownFIRInterpolate(
			out[outIndex:],
			buf,
			firCoefs,
			maxIndexQ16,
			r.invRatioQ16,
		)

		in = in[nSamplesIn:]
		if len(in) > 1 {
			copy(buf[:r.firOrder], buf[nSamplesIn:])
		} else {
			break
		}
	}

	copy(r.sFIR[:r.firOrder], buf[nSamplesIn:])
}

func (r *Resampler) resamplerPrivateDownFIRInterpolate(
	out []int16,
	buf []int32,
	firCoefs []int16,
	maxIndexQ16, indexIncrementQ16 int32,
) int {
	outIndex := 0
	switch r.firOrder {
	case downOrderFIR0:
		for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
			bufPtr := buf[indexQ16>>16:]
			interpolInd := silkSMULWB(int32(uint32(indexQ16)&0xffff), int32(r.firFracs)) // #nosec G115

			interpolPtr := firCoefs[downOrderFIR0/2*interpolInd:]
			resQ6 := silkSMULWB(bufPtr[0], int32(interpolPtr[0]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[1], int32(interpolPtr[1]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[2], int32(interpolPtr[2]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[3], int32(interpolPtr[3]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[4], int32(interpolPtr[4]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[5], int32(interpolPtr[5]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[6], int32(interpolPtr[6]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[7], int32(interpolPtr[7]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[8], int32(interpolPtr[8]))
			interpolPtr = firCoefs[downOrderFIR0/2*(int32(r.firFracs)-1-interpolInd):] // #nosec G115
			resQ6 = silkSMLAWB(resQ6, bufPtr[17], int32(interpolPtr[0]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[16], int32(interpolPtr[1]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[15], int32(interpolPtr[2]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[14], int32(interpolPtr[3]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[13], int32(interpolPtr[4]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[12], int32(interpolPtr[5]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[11], int32(interpolPtr[6]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[10], int32(interpolPtr[7]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[9], int32(interpolPtr[8]))

			out[outIndex] = silkSAT16(silkRShiftRound(resQ6, 6))
			outIndex++
		}
	case downOrderFIR1:
		for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
			bufPtr := buf[indexQ16>>16:]

			resQ6 := silkSMULWB(bufPtr[0]+bufPtr[23], int32(firCoefs[0]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[1]+bufPtr[22], int32(firCoefs[1]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[2]+bufPtr[21], int32(firCoefs[2]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[3]+bufPtr[20], int32(firCoefs[3]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[4]+bufPtr[19], int32(firCoefs[4]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[5]+bufPtr[18], int32(firCoefs[5]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[6]+bufPtr[17], int32(firCoefs[6]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[7]+bufPtr[16], int32(firCoefs[7]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[8]+bufPtr[15], int32(firCoefs[8]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[9]+bufPtr[14], int32(firCoefs[9]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[10]+bufPtr[13], int32(firCoefs[10]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[11]+bufPtr[12], int32(firCoefs[11]))

			out[outIndex] = silkSAT16(silkRShiftRound(resQ6, 6))
			outIndex++
		}
	case downOrderFIR2:
		for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
			bufPtr := buf[indexQ16>>16:]

			resQ6 := silkSMULWB(bufPtr[0]+bufPtr[35], int32(firCoefs[0]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[1]+bufPtr[34], int32(firCoefs[1]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[2]+bufPtr[33], int32(firCoefs[2]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[3]+bufPtr[32], int32(firCoefs[3]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[4]+bufPtr[31], int32(firCoefs[4]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[5]+bufPtr[30], int32(firCoefs[5]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[6]+bufPtr[29], int32(firCoefs[6]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[7]+bufPtr[28], int32(firCoefs[7]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[8]+bufPtr[27], int32(firCoefs[8]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[9]+bufPtr[26], int32(firCoefs[9]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[10]+bufPtr[25], int32(firCoefs[10]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[11]+bufPtr[24], int32(firCoefs[11]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[12]+bufPtr[23], int32(firCoefs[12]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[13]+bufPtr[22], int32(firCoefs[13]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[14]+bufPtr[21], int32(firCoefs[14]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[15]+bufPtr[20], int32(firCoefs[15]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[16]+bufPtr[19], int32(firCoefs[16]))
			resQ6 = silkSMLAWB(resQ6, bufPtr[17]+bufPtr[18], int32(firCoefs[17]))

			out[outIndex] = silkSAT16(silkRShiftRound(resQ6, 6))
			outIndex++
		}
	}

	return outIndex
}
