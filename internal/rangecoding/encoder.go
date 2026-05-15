// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package rangecoding

import (
	"math/bits"
)

// Range coder constants for the 32-bit encoder, matching the decoder
// parameters in Section 4.1 of RFC 6716.
//
//   - codeBits  = 32 — precision of the range coder integer arithmetic.
//   - codeTop   = 2**31 — the initial range size and the sentinel that
//     separates the carry buffer from the data bits.
//   - codeShift = 23 — right-shift to extract the top 9 bits (8 data + 1
//     carry) from val before normalizing.
//   - symBits / symMax — the byte-sized output symbol width used by the
//     renormalization loop.
const (
	codeBits  = 32
	codeTop   = uint32(1) << (codeBits - 1)
	codeShift = codeBits - 9
	symBits   = 8
	symMax    = (1 << symBits) - 1
)

// Encoder implements the range encoder defined in RFC 6716 Section 5.1.
//
// The range coder acts as the bit-packer for Opus.  It is used in three
// different ways: to encode
//
//   - Entropy-coded symbols with a fixed probability model using
//     ec_encode() (entenc.c),
//
//   - Integers from 0 to (2**M - 1) using ec_enc_uint() or ec_enc_bits()
//     (entenc.c),
//
//   - Integers from 0 to (ft - 1) (where ft is not a power of two) using
//     ec_enc_uint() (entenc.c).
//
// The range encoder maintains an internal state vector composed of the
// four-tuple (val, rng, rem, ext) representing the low end of the
// current range, the size of the current range, a single buffered
// output byte, and a count of additional carry-propagating output
// bytes.  Both val and rng are 32-bit unsigned integer values, rem is a
// byte value less than 255 or the special value -1, and ext is an
// unsigned integer with at least 11 bits.  This state vector is
// initialized at the start of each frame to the value
// (0, 2**31, -1, 0).  After encoding a sequence of symbols, the value
// of rng in the encoder should exactly match the value of rng in the
// decoder after decoding the same sequence of symbols.  This is a
// powerful tool for detecting errors in either an encoder or decoder
// implementation.  The value of val, on the other hand, represents
// different things in the encoder and decoder, and is not expected to
// match.
//
// The decoder has no analog for rem and ext.  These are used to perform
// carry propagation in the renormalization loop below.  Each iteration
// of this loop produces 9 bits of output, consisting of 8 data bits and
// a carry flag.  The encoder cannot determine the final value of the
// output bytes until it propagates these carry flags.  Therefore, the
// reference implementation buffers a single non-propagating output byte
// (i.e., one less than 255) in rem and keeps a count of additional
// propagating (i.e., 255) output bytes in ext.
//
// Symbols may also be coded as "raw bits" packed directly into the
// bitstream, bypassing the range coder.  These are packed backwards
// starting at the end of the frame, as illustrated in Figure 12 of
// RFC 6716.  This reduces complexity and makes the stream more resilient
// to bit errors.  Raw bits are only used in the CELT layer.
//
//	          0                   1                   2                   3
//	          0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	         +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	         | Range coder data (packed MSB to LSB) ->                       :
//	         +                                                               +
//	         :                                                               :
//	         +     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	         :     | <- Boundary occurs at an arbitrary bit position         :
//	         +-+-+-+                                                         +
//	         :                          <- Raw bits data (packed LSB to MSB) |
//	         +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
//	Legend:
//
//	LSB = Least Significant Bit
//	MSB = Most Significant Bit
//
//	     Figure 12: Illustrative Example of Packing Range Coder
//	                        and Raw Bits Data
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1
type Encoder struct {
	buf  []byte // range-coded bytes flushed front-to-back (val in RFC 6716)
	tail []byte // raw-bits bytes flushed in LSB-first order

	endWindow uint64 // accumulator for raw bits not yet flushed to tail
	nendBits  uint   // number of valid bits in endWindow

	rangeSize uint32 // rng in RFC 6716 — current range size
	low       uint32 // val in RFC 6716 — low end of the current range

	rem      int // buffered pending byte (-1 = empty); rem in RFC 6716
	extBytes int // count of carry-propagating 0xFF bytes; ext in RFC 6716

	nbitsTotal uint // conservative bit-usage counter for Tell/TellFrac
}

// Init resets the Encoder state for a new frame.
//
// RFC 6716 Section 5.1 specifies that the encoder state vector
// (val, rng, rem, ext) is initialized at the start of each frame to
// (0, 2**31, -1, 0).  nbitsTotal is set to codeBits + 1 so that Tell()
// returns 1 after initialization, matching the decoder's post-Init value
// (the decoder consumes one bit of bootstrap input during Init).
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1
func (e *Encoder) Init() {
	e.buf = e.buf[:0]
	e.tail = e.tail[:0]
	e.endWindow = 0
	e.nendBits = 0
	e.rangeSize = codeTop
	e.low = 0
	e.rem = -1
	e.extBytes = 0
	e.nbitsTotal = codeBits + 1
}

// EncodeSymbolWithICDF encodes a symbol using the same inverse cumulative
// distribution table format consumed by Decoder.DecodeSymbolWithICDF.
//
// This implements ec_enc_icdf() (entenc.c), which is mathematically
// equivalent to calling ec_encode() with fl[k] = (1<<ftb) - icdf[k-1],
// fh[k] = (1<<ftb) - icdf[k], and ft = (1<<ftb).  It allows the encoder
// to use the same icdf tables as the decoder.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.2
func (e *Encoder) EncodeSymbolWithICDF(cumulativeDistributionTable []uint, symbol uint32) {
	if len(cumulativeDistributionTable) < 2 {
		return
	}

	total := uint32(cumulativeDistributionTable[0]) //nolint:gosec // G115
	table := cumulativeDistributionTable[1:]
	if int(symbol) >= len(table) {
		return
	}

	high := uint32(table[symbol]) //nolint:gosec // G115
	low := uint32(0)
	if symbol != 0 {
		low = uint32(table[symbol-1]) //nolint:gosec // G115
	}

	e.EncodeCumulative(low, high, total)
}

// EncodeSymbolLogP encodes a single binary symbol with probability 1/(1<<logp)
// for symbol 1.
//
// This implements ec_enc_bit_logp() (entenc.c), which is mathematically
// equivalent to calling ec_encode() with the 3-tuple
// (fl[k] = 0, fh[k] = (1<<logp) - 1, ft = (1<<logp)) if k is 0 and with
// (fl[k] = (1<<logp) - 1, fh[k] = ft = (1<<logp)) if k is 1.  The
// implementation requires no multiplications or divisions.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.2
func (e *Encoder) EncodeSymbolLogP(logp uint, symbol uint32) {
	scale := e.rangeSize >> logp
	rangeSize := e.rangeSize - scale

	if symbol != 0 {
		e.low += rangeSize
		e.rangeSize = scale
	} else {
		e.rangeSize = rangeSize
	}

	e.normalize()
}

// EncodeCumulative encodes a pre-selected cumulative interval (low, high)
// out of total equally weighted bins.
//
// This is the main encoding function ec_encode() (entenc.c) defined in
// RFC 6716 Section 5.1.1.  It encodes symbol k described by the three-tuple
// (fl[k], fh[k], ft) using the same semantics as the decoder's ec_decode().
//
// If fl[k] (low) is greater than zero:
//
//	val = val + rng - (rng / ft) * (ft - fl)
//	rng = (rng / ft) * (fh - fl)
//
// Otherwise val is unchanged and:
//
//	rng = rng - (rng / ft) * (ft - fh)
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.1
func (e *Encoder) EncodeCumulative(low, high, total uint32) {
	if total == 0 || low >= high || high > total {
		return
	}

	scale := e.rangeSize / total
	if low != 0 {
		e.low += e.rangeSize - scale*(total-low)
		e.rangeSize = scale * (high - low)
	} else {
		e.rangeSize -= scale * (total - high)
	}

	e.normalize()
}

// EncodeUniform encodes one of ft equiprobable symbols in the range
// [0, ft), implementing ec_enc_uint() (entenc.c).
//
// RFC 6716 Section 5.1.4 splits the value into a range-coded prefix of up
// to 8 high bits and, if ft requires more than 8 bits, a raw-bit suffix:
//
//	If ftb = ilog(ft - 1) <= 8, encode t directly via ec_encode().
//	If ftb > 8, encode t>>(ftb-8) via ec_encode() and the remaining
//	(ftb - 8) bits of t via ec_enc_bits().
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.4
func (e *Encoder) EncodeUniform(total, symbol uint32) {
	if total <= 1 {
		return
	}

	if symbol >= total {
		symbol = total - 1
	}

	limit := total - 1
	bitCount := bits.Len32(limit)
	if bitCount <= maxUniformRangeCoderBits {
		e.EncodeCumulative(symbol, symbol+1, total)

		return
	}

	rawBitCount := bitCount - maxUniformRangeCoderBits
	rangeTotal := (limit >> rawBitCount) + 1
	prefix := symbol >> rawBitCount
	e.EncodeCumulative(prefix, prefix+1, rangeTotal)
	e.EncodeRawBits(uint(rawBitCount), symbol&bitMask(uint(rawBitCount)))
}

// EncodeLaplace encodes a Laplace-distributed integer value using the
// same probability model as Decoder.DecodeLaplace.
//
// RFC 6716 Section 4.3.2.1 describes coarse energy deltas as
// Laplace-distributed prediction errors.  The distribution is parameterized
// by fs0 (the frequency of the zero symbol, in units of 1/32768) and decay
// (the geometric decay rate of adjacent-magnitude frequencies, Q15).
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.3.2.1
func (e *Encoder) EncodeLaplace(fs0, decay uint32, value int) {
	low, high := laplaceInterval(fs0, decay, value)
	e.EncodeCumulative(low, high, laplaceTotal)
}

// EncodeRawBits appends n bits of value to the raw-bits region at the end of
// the frame, packed in LSB-first order.
//
// RFC 6716 Section 5.1.3 specifies that raw bits used by the CELT layer are
// packed at the end of the buffer using ec_enc_bits() (entenc.c).  Because the
// raw bits may continue into the last byte output by the range coder if there
// is room in the low-order bits, Done() merges the two regions into a single
// byte when they meet.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.3
func (e *Encoder) EncodeRawBits(n uint, value uint32) {
	if n == 0 {
		return
	}
	if n < 32 {
		value &= bitMask(n)
	}
	e.endWindow |= uint64(value) << e.nendBits
	e.nendBits += n
	e.nbitsTotal += n
	for e.nendBits >= symBits {
		e.tail = append(e.tail, byte(e.endWindow&symMax))
		e.endWindow >>= symBits
		e.nendBits -= symBits
	}
}

// Tell returns a conservative upper bound, in whole bits, of the number of
// bits encoded into the current frame so far.
//
// This implements ec_tell() (entcode.h) from RFC 6716 Section 5.1.6.  The
// bit allocation routines in Opus use this value to track budget consumption
// and prevent the range coder from overflowing the output buffer.  After
// encoding the same symbols, the encoder and decoder must produce identical
// Tell() values.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.6
func (e *Encoder) Tell() uint {
	lg := uint(bits.Len32(e.rangeSize)) //nolint:gosec // G115: bits.Len32 returns 0..32.
	if lg == 0 {
		return e.nbitsTotal
	}

	if e.nbitsTotal <= lg {
		return 0
	}

	return e.nbitsTotal - lg
}

// TellFrac returns a conservative upper bound in 1/8-bit units.
//
// This implements ec_tell_frac() (entcode.c) from RFC 6716 Section 5.1.6.
// It refines the Tell() estimate by squaring down the fractional part of the
// range size three times to obtain three additional sub-bit fractions.  The
// encoder and decoder must produce identical TellFrac() values after encoding
// and decoding the same symbols.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.6
func (e *Encoder) TellFrac() uint {
	lg := uint(bits.Len32(e.rangeSize)) //nolint:gosec // G115: bits.Len32 returns 0..32.
	if lg == 0 {
		return e.nbitsTotal * 8
	}
	if lg < 24 {
		return e.Tell() * 8
	}

	rangeQ15 := uint64(e.rangeSize >> (lg - 16))
	for range 3 {
		rangeQ15 = (rangeQ15 * rangeQ15) >> 15
		bit := rangeQ15 >> 16
		lg = 2*lg + uint(bit)
		if bit != 0 {
			rangeQ15 >>= 1
		}
	}

	total := e.nbitsTotal * 8
	if total <= lg {
		return 0
	}

	return total - lg
}

// FinalRange exposes the current range coder range state for tests.
//
// RFC 6716 Section 5.1 states that after encoding a sequence of symbols the
// value of rng in the encoder should exactly match the value of rng in the
// decoder after decoding the same sequence of symbols.  This is a powerful
// tool for detecting errors in either an encoder or decoder implementation.
func (e *Encoder) FinalRange() uint32 {
	return e.rangeSize
}

// Done flushes the range coder and raw bits into a single output frame,
// implementing ec_enc_done() (entenc.c).
//
// RFC 6716 Section 5.1.5 describes the finalization procedure:
//
//  1. Find the unsigned integer end in [val, val+rng) with the largest
//     number of trailing zero bits b such that (end + (1<<b) - 1) is also
//     in [val, val+rng).  Flush the remaining bytes of end through the carry
//     buffer.
//
//  2. If the buffered output byte rem is neither zero nor -1, or the carry
//     count ext is greater than zero, flush 9 zero bits to drain the carry
//     buffer.
//
//  3. Merge the last range-coder byte and the first raw-bits byte into one
//     byte if the range coder did not consume all bits in its final byte.
//     Any space between the range coder data and the raw bits is zero-padded.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.5
func (e *Encoder) Done() []byte {
	remainingBits := e.flushRangeCoder()

	if e.rem >= 0 || e.extBytes > 0 {
		e.carryOut(0)
	}

	freeBitsInLastRangeByte := uint(0)
	if remainingBits < 0 {
		freeBitsInLastRangeByte = uint(-remainingBits) //nolint:gosec // G115
	}

	out := make([]byte, len(e.buf)+len(e.tail)+boolToInt(e.shouldWritePartialToNewByte(freeBitsInLastRangeByte)))
	copy(out, e.buf)

	for index, value := range e.tail {
		out[len(out)-1-index] = value
	}

	if e.nendBits > 0 {
		partial := byte(e.endWindow & uint64(bitMask(e.nendBits))) //nolint:gosec // G115: masked to at most 8 bits.
		if e.shouldWritePartialToNewByte(freeBitsInLastRangeByte) {
			out[len(e.buf)] = partial
		} else {
			out[len(e.buf)-1] |= partial
		}
	}

	return out
}

// flushRangeCoder finalizes the range-coded portion of the frame by finding
// the integer end in [val, val+rng) with the most trailing zero bits and
// flushing its remaining bytes through the carry buffer.
//
// RFC 6716 Section 5.1.5 specifies that end is chosen so that
// (end + (1<<b) - 1) is also within [val, val+rng), maximizing the number
// of trailing bits that can be set to arbitrary values by the raw-bits
// region.  It returns the number of bits remaining after the last carryOut
// call (≤ 0 means the final byte was fully consumed; a negative value
// indicates how many low-order bits of the last flushed byte are free for
// ORing in raw bits).
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.5
func (e *Encoder) flushRangeCoder() int {
	remainingBits := codeBits - bits.Len32(e.rangeSize)
	mask := (codeTop - 1) >> remainingBits
	end := (e.low + mask) &^ mask
	if (end | mask) >= e.low+e.rangeSize {
		remainingBits++
		mask >>= 1
		end = (e.low + mask) &^ mask
	}

	for remainingBits > 0 {
		e.carryOut(int(end >> codeShift))
		end = (end << symBits) & (codeTop - 1)
		remainingBits -= symBits
	}

	return remainingBits
}

// shouldWritePartialToNewByte reports whether the leftover raw-bits nibble
// must occupy its own byte rather than being ORed into the last range-coder
// byte.  This is the case when there are no range-coder bytes yet, or when
// the partial nibble is wider than the free low-order bits of the last
// range-coder byte.
func (e *Encoder) shouldWritePartialToNewByte(freeBitsInLastRangeByte uint) bool {
	return e.nendBits > 0 && (len(e.buf) == 0 || e.nendBits > freeBitsInLastRangeByte)
}

// normalize implements ec_enc_normalize() (entenc.c), the renormalization
// step that maintains the invariant rng > 2**23 after each symbol is encoded.
//
// RFC 6716 Section 5.1.1.1 specifies: repeat until rng > 2**23.  First,
// send the top 9 bits of val, (val>>23), to the carry buffer.  Then set
//
//	val = (val<<8) & 0x7FFFFFFF
//	rng = rng<<8
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.1
func (e *Encoder) normalize() {
	for e.rangeSize <= minRangeSize {
		e.carryOut(int(e.low >> codeShift))
		e.low = (e.low << symBits) & (codeTop - 1)
		e.rangeSize <<= symBits
		e.nbitsTotal += symBits
	}
}

// carryOut implements ec_enc_carry_out() (entenc.c), which performs carry
// propagation and output buffering for the 9-bit value produced by each
// iteration of the renormalization loop.
//
// RFC 6716 Section 5.1.1.2: the input value c consists of 8 data bits and an
// additional carry bit.
//
//   - If c == 255: ext is incremented and no other state update is performed.
//
//   - Otherwise let b = c >> 8 be the carry bit.  Then:
//
//     o If rem contains a value other than -1, output the byte (rem + b).
//     o If ext is non-zero, output ext bytes of value (255 if b == 0, else 0),
//     then set ext to 0.
//     o Set rem = c & 255.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-5.1.1
func (e *Encoder) carryOut(value int) {
	if value != symMax {
		carry := value >> symBits
		if e.rem >= 0 {
			e.buf = append(e.buf, byte(e.rem+carry)) //nolint:gosec // G115: carry propagation is bounded to one byte.
		}
		if e.extBytes > 0 {
			flush := byte((symMax + carry) & symMax)
			for range e.extBytes {
				e.buf = append(e.buf, flush)
			}
			e.extBytes = 0
		}
		e.rem = value & symMax

		return
	}
	e.extBytes++
}

// bitMask returns a uint32 with the n lowest bits set.
func bitMask(n uint) uint32 {
	if n >= 32 {
		return ^uint32(0)
	}

	return (uint32(1) << n) - 1
}

// laplaceInterval computes the cumulative [low, high) interval for the given
// value in the Laplace distribution defined by RFC 6716 Section 4.3.2.1.
//
// The distribution is parameterized by fs0 (the cumulative frequency of the
// zero symbol, in units of 1/32768) and decay (the geometric decay rate of
// adjacent-magnitude frequencies, in Q15 fixed-point).  Positive and negative
// values of equal magnitude share a frequency but occupy disjoint halves of the
// cumulative axis: the positive half comes first, the negative half follows
// immediately.  laplaceFirstDecayFrequency() computes the per-step decay.
func laplaceInterval(fs0 uint32, decay uint32, value int) (uint32, uint32) {
	if value == 0 {
		return 0, min(fs0, uint32(laplaceTotal))
	}

	magnitude := value
	if magnitude < 0 {
		magnitude = -magnitude
	}

	low := fs0
	frequency := laplaceFirstDecayFrequency(fs0, decay) + laplaceMinProbability
	currentMagnitude := 1
	for currentMagnitude < magnitude && frequency > laplaceMinProbability {
		low += 2 * frequency
		frequency = ((2*frequency - 2*laplaceMinProbability) * decay) >> 15
		frequency += laplaceMinProbability
		currentMagnitude++
	}
	if currentMagnitude < magnitude {
		deltaCount := uint32(magnitude - currentMagnitude) //nolint:gosec // G115
		low += 2 * deltaCount * laplaceMinProbability
		frequency = laplaceMinProbability
	}
	if value < 0 {
		return min(low, uint32(laplaceTotal)), min(low+frequency, uint32(laplaceTotal))
	}

	low += frequency

	return min(low, uint32(laplaceTotal)), min(low+frequency, uint32(laplaceTotal))
}

// boolToInt converts a bool to 0 or 1.
func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}
