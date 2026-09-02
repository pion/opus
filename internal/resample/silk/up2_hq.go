// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

func (r *Resampler) resamplerPrivateUp2HQ(out, in []int16) {
	// The even and odd outputs each use a three-stage allpass recurrence.
	// State stays local throughout the block. out needs two slots per input sample.
	s0, s1, s2 := r.sIIR[0], r.sIIR[1], r.sIIR[2]
	s3, s4, s5 := r.sIIR[3], r.sIIR[4], r.sIIR[5]
	c0, c1, c2 := int32(resamplerUp2HQ0[0]), int32(resamplerUp2HQ0[1]), int32(resamplerUp2HQ0[2])
	c3, c4, c5 := int32(resamplerUp2HQ1[0]), int32(resamplerUp2HQ1[1]), int32(resamplerUp2HQ1[2])
	for sampleIndex := range in {
		in32 := int32(in[sampleIndex]) << 10

		diff := in32 - s0
		delta := silkSMULWB(diff, c0)
		out32 := s0 + delta
		s0 = in32 + delta

		out32_1 := out32
		diff = out32_1 - s1
		delta = silkSMULWB(diff, c1)
		out32 = s1 + delta
		s1 = out32_1 + delta

		out32_2 := out32
		diff = out32_2 - s2
		delta = silkSMLAWB(diff, diff, c2)
		out32 = s2 + delta
		s2 = out32_2 + delta

		out[2*sampleIndex] = silkSAT16(silkRShiftRound(out32, 10))

		diff = in32 - s3
		delta = silkSMULWB(diff, c3)
		out32 = s3 + delta
		s3 = in32 + delta

		out32_1 = out32
		diff = out32_1 - s4
		delta = silkSMULWB(diff, c4)
		out32 = s4 + delta
		s4 = out32_1 + delta

		out32_2 = out32
		diff = out32_2 - s5
		delta = silkSMLAWB(diff, diff, c5)
		out32 = s5 + delta
		s5 = out32_2 + delta

		out[(2*sampleIndex)+1] = silkSAT16(silkRShiftRound(out32, 10))
	}
	// The next block continues both filter recurrences from these states.
	r.sIIR = [6]int32{s0, s1, s2, s3, s4, s5}
}
