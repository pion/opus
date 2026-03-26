// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silkresample

func silkResamplerPrivateAR2(state []int32, outQ8 []int32, in []int16, aQ14 []int16) {
	for k := range in {
		out32 := state[0] + (int32(in[k]) << 8)
		outQ8[k] = out32
		out32 <<= 2
		state[0] = silkSMLAWB(state[1], out32, int32(aQ14[0]))
		state[1] = silkSMULWB(out32, int32(aQ14[1]))
	}
}
