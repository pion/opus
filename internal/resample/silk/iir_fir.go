// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

func (r *Resampler) resamplerPrivateIIRFIR(out, in []int16) {
	if r.fsOutKHz == r.fsInKHz*3 {
		r.resamplerPrivateIIRFIR3x(out, in)

		return
	}

	if cap(r.iirFIRBuf) < (2*r.batchSize)+orderFIR12 {
		r.iirFIRBuf = make([]int16, (2*r.batchSize)+orderFIR12)
	}
	buf := r.iirFIRBuf[:(2*r.batchSize)+orderFIR12]
	clear(buf)
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

func (r *Resampler) resamplerPrivateIIRFIR3x(out, in []int16) {
	phase0 := resamplerFracFIR12[0]
	reversePhase0 := resamplerFracFIR12[11]
	phase8 := resamplerFracFIR12[8]
	reversePhase8 := resamplerFracFIR12[3]
	phase4 := resamplerFracFIR12[4]
	reversePhase4 := resamplerFracFIR12[7]

	h0 := r.sFIRIIR[0]
	h1 := r.sFIRIIR[1]
	h2 := r.sFIRIIR[2]
	h3 := r.sFIRIIR[3]
	h4 := r.sFIRIIR[4]
	h5 := r.sFIRIIR[5]
	h6 := r.sFIRIIR[6]
	h7 := r.sFIRIIR[7]

	outIndex := 0
	for sampleIndex := range in {
		resQ15 := silkSMULBB(int32(h0), int32(phase0[0]))
		resQ15 = silkSMLABB(resQ15, int32(h1), int32(phase0[1]))
		resQ15 = silkSMLABB(resQ15, int32(h2), int32(phase0[2]))
		resQ15 = silkSMLABB(resQ15, int32(h3), int32(phase0[3]))
		resQ15 = silkSMLABB(resQ15, int32(h4), int32(reversePhase0[3]))
		resQ15 = silkSMLABB(resQ15, int32(h5), int32(reversePhase0[2]))
		resQ15 = silkSMLABB(resQ15, int32(h6), int32(reversePhase0[1]))
		resQ15 = silkSMLABB(resQ15, int32(h7), int32(reversePhase0[0]))
		out[outIndex] = silkSAT16(silkRShiftRound(resQ15, 15))

		resQ15 = silkSMULBB(int32(h0), int32(phase8[0]))
		resQ15 = silkSMLABB(resQ15, int32(h1), int32(phase8[1]))
		resQ15 = silkSMLABB(resQ15, int32(h2), int32(phase8[2]))
		resQ15 = silkSMLABB(resQ15, int32(h3), int32(phase8[3]))
		resQ15 = silkSMLABB(resQ15, int32(h4), int32(reversePhase8[3]))
		resQ15 = silkSMLABB(resQ15, int32(h5), int32(reversePhase8[2]))
		resQ15 = silkSMLABB(resQ15, int32(h6), int32(reversePhase8[1]))
		resQ15 = silkSMLABB(resQ15, int32(h7), int32(reversePhase8[0]))
		out[outIndex+1] = silkSAT16(silkRShiftRound(resQ15, 15))

		even, odd := r.resamplerPrivateUp2HQSample(in[sampleIndex])
		resQ15 = silkSMULBB(int32(h1), int32(phase4[0]))
		resQ15 = silkSMLABB(resQ15, int32(h2), int32(phase4[1]))
		resQ15 = silkSMLABB(resQ15, int32(h3), int32(phase4[2]))
		resQ15 = silkSMLABB(resQ15, int32(h4), int32(phase4[3]))
		resQ15 = silkSMLABB(resQ15, int32(h5), int32(reversePhase4[3]))
		resQ15 = silkSMLABB(resQ15, int32(h6), int32(reversePhase4[2]))
		resQ15 = silkSMLABB(resQ15, int32(h7), int32(reversePhase4[1]))
		resQ15 = silkSMLABB(resQ15, int32(even), int32(reversePhase4[0]))
		out[outIndex+2] = silkSAT16(silkRShiftRound(resQ15, 15))

		h0, h1, h2, h3 = h2, h3, h4, h5
		h4, h5, h6, h7 = h6, h7, even, odd
		outIndex += 3
	}

	r.sFIRIIR = [orderFIR12]int16{h0, h1, h2, h3, h4, h5, h6, h7}
}
