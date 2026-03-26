// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

func (r *Resampler) resamplerPrivateUp2HQ(out, in []int16) {
	for sampleIndex := range in {
		in32 := int32(in[sampleIndex]) << 10

		diff := in32 - r.sIIR[0]
		delta := silkSMULWB(diff, int32(resamplerUp2HQ0[0]))
		out32 := r.sIIR[0] + delta
		r.sIIR[0] = in32 + delta

		out32_1 := out32
		diff = out32_1 - r.sIIR[1]
		delta = silkSMULWB(diff, int32(resamplerUp2HQ0[1]))
		out32 = r.sIIR[1] + delta
		r.sIIR[1] = out32_1 + delta

		out32_2 := out32
		diff = out32_2 - r.sIIR[2]
		delta = silkSMLAWB(diff, diff, int32(resamplerUp2HQ0[2]))
		out32 = r.sIIR[2] + delta
		r.sIIR[2] = out32_2 + delta

		out[2*sampleIndex] = silkSAT16(silkRShiftRound(out32, 10))

		diff = in32 - r.sIIR[3]
		delta = silkSMULWB(diff, int32(resamplerUp2HQ1[0]))
		out32 = r.sIIR[3] + delta
		r.sIIR[3] = in32 + delta

		out32_1 = out32
		diff = out32_1 - r.sIIR[4]
		delta = silkSMULWB(diff, int32(resamplerUp2HQ1[1]))
		out32 = r.sIIR[4] + delta
		r.sIIR[4] = out32_1 + delta

		out32_2 = out32
		diff = out32_2 - r.sIIR[5]
		delta = silkSMLAWB(diff, diff, int32(resamplerUp2HQ1[2]))
		out32 = r.sIIR[5] + delta
		r.sIIR[5] = out32_2 + delta

		out[(2*sampleIndex)+1] = silkSAT16(silkRShiftRound(out32, 10))
	}
}
