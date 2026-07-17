// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

// LTP state-scaling gains (silk_LTPScales_table_Q14).
//
//nolint:gochecknoglobals // constant lookup table.
var ltpScalesTableQ14 = [3]int32{15565, 12288, 8192}

// ltpScaleControl selects the LTP state-scaling index for an independently
// coded voiced frame (silk_LTP_scale_ctrl_FLP). ltpredCodGain is the LTP
// prediction gain in dB and snrDBQ7 the target SNR in Q7. Higher expected loss
// pushes toward stronger scaling so decoded frames recover faster; with no
// configured packet loss the index is always 0. Returns the index and its Q14
// scale.
//
//nolint:gosec // G115: dB gains and loss percentages are small, in-range values.
func ltpScaleControl(
	ltpredCodGain float32, snrDBQ7 int32, packetLossPerc, nFramesPerPacket int, lbrr bool,
) (int, int32) {
	roundLoss := packetLossPerc * nFramesPerPacket
	if lbrr {
		roundLoss = 2 + int(smulbb(int32(roundLoss), int32(roundLoss)))/100
	}

	idx := 0
	if smulbb(int32(ltpredCodGain), int32(roundLoss)) > log2lin(2900-snrDBQ7) {
		idx++
	}
	if smulbb(int32(ltpredCodGain), int32(roundLoss)) > log2lin(3900-snrDBQ7) {
		idx++
	}

	return idx, ltpScalesTableQ14[idx]
}
