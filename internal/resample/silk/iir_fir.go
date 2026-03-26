// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

func (r *Resampler) resamplerPrivateIIRFIR(out, in []int16) {
	buf := make([]int16, (2*r.batchSize)+orderFIR12)
	copy(buf, r.sFIRIIR[:])

	outIndex := 0
	for len(in) > 0 {
		nSamplesIn := min(len(in), r.batchSize)

		r.resamplerPrivateUp2HQ(buf[orderFIR12:], in[:nSamplesIn])

		maxIndexQ16 := int32(nSamplesIn) << 17 // #nosec G115
		outIndex += resamplerPrivateIIRFIRInterpolate(out[outIndex:], buf, maxIndexQ16, r.invRatioQ16)
		in = in[nSamplesIn:]

		if len(in) > 0 {
			copy(buf[:orderFIR12], buf[nSamplesIn*2:])
		} else {
			copy(r.sFIRIIR[:], buf[nSamplesIn*2:])
		}
	}
}

func resamplerPrivateIIRFIRInterpolate(out, buf []int16, maxIndexQ16, indexIncrementQ16 int32) int {
	outIndex := 0
	for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
		tableIndex := silkSMULWB(int32(uint32(indexQ16)&0xffff), 12)
		bufIndex := int(indexQ16 >> 16)
		bufPtr := buf[bufIndex:]

		resQ15 := silkSMULBB(int32(bufPtr[0]), int32(resamplerFracFIR12[tableIndex][0]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[1]), int32(resamplerFracFIR12[tableIndex][1]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[2]), int32(resamplerFracFIR12[tableIndex][2]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[3]), int32(resamplerFracFIR12[tableIndex][3]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[4]), int32(resamplerFracFIR12[11-tableIndex][3]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[5]), int32(resamplerFracFIR12[11-tableIndex][2]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[6]), int32(resamplerFracFIR12[11-tableIndex][1]))
		resQ15 = silkSMLABB(resQ15, int32(bufPtr[7]), int32(resamplerFracFIR12[11-tableIndex][0]))

		out[outIndex] = silkSAT16(silkRShiftRound(resQ15, 15))
		outIndex++
	}

	return outIndex
}
