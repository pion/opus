// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build conformance

package silk

// FinalRange exposes the SILK range coder state for RFC conformance tests.
func (d *Decoder) FinalRange() uint32 {
	return d.rangeDecoder.FinalRange()
}
