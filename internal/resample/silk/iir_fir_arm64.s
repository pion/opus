// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build arm64 && go1.27 && !purego

#include "textflag.h"

// The wrapper checks the buffer spans and positive count/step.
// If out and buf alias, later windows may read earlier outputs, so process
// outputs in order.
TEXT ·resamplerPrivateIIRFIRInterpolateNEON(SB), NOSPLIT, $0-36
	MOVD out+0(FP), R0
	MOVD buf+8(FP), R1
	MOVD coefficients+16(FP), R2
	MOVD sampleCount+24(FP), R3
	MOVWU indexIncrementQ16+32(FP), R4
	MOVD $0, R5
	MOVD $12, R7

firLoop:
	// The fraction selects floor(fraction*12/65536), a phase in [0,11].
	// Each phase occupies 16 bytes; the integer part selects the sample window.
	AND $0xffff, R5, R6
	MUL R7, R6, R6
	LSR $16, R6, R6
	ADD R6<<4, R2, R6
	LSR $16, R5, R8
	ADD R8<<1, R1, R8
	// These loads accept unaligned windows and read exactly eight int16s.
	VLD1 (R8), [V0.H8]
	VLD1 (R6), [V1.H8]
	// Multiply both signed int16 halves, then reduce with wrapping int32 adds.
	// Reassociating these additions is exact modulo 2^32; do not saturate yet.
	VSMULL V0.H4, V1.H4, V2.S4
	VSMLAL2 V0.H8, V1.H8, V2.S4
	VADDV V2.S4, V2
	// SRSHR matches ((sum >> 14) + 1) >> 1, including negative half-way values
	// rounding toward +infinity, without overflowing a 32-bit rounding bias.
	VSRSHR $15, V2.S4, V2.S4
	// Saturate only the rounded sum, then store exactly one int16 output.
	VSQXTN V2.S4, V2.H4
	VMOV V2.H[0], R9
	MOVH.P R9, 2(R0)
	ADD R4, R5, R5
	SUB $1, R3, R3
	CBNZ R3, firLoop
	RET
