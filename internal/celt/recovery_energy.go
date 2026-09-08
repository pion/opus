// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-FileCopyrightText: 2007-2008 CSIRO
// SPDX-FileCopyrightText: 2007-2010 Xiph.Org Foundation
// SPDX-FileCopyrightText: 2008 Gregory Maxwell
// SPDX-License-Identifier: MIT AND BSD-2-Clause

package celt

func (d *Decoder) prepareRecoveryEnergy(info *frameSideInfo) {
	if info.intraEnergy || d.lossDuration == 0 {
		return
	}

	// libopus celt_decoder.c predicts the energy trend across a loss before
	// inter-frame coarse-energy decoding, limiting loud recovery artifacts.
	// Count missing frames at the recovery frame's duration, not the PLC call's.
	missing := min(10, d.lossDuration>>info.lm)
	var safety float32
	if info.lm == 0 {
		safety = 1.5
	} else if info.lm == 1 {
		safety = 0.5
	}
	for channel := range d.previousLogE {
		for band := info.startBand; band < info.endBand; band++ {
			energy := d.previousLogE[channel][band]
			previous := d.previousLogE1[channel][band]
			older := d.previousLogE2[channel][band]
			if energy < max(previous, older) {
				slope := min(float32(2), max(previous-energy, 0.5*(older-energy)))
				energy = max(float32(-20), energy-max(float32(0), float32(1+missing)*slope))
			} else {
				energy = min(energy, min(previous, older))
			}
			d.previousLogE[channel][band] = energy - safety
		}
	}
}
