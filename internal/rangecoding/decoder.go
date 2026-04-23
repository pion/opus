// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package rangecoding

import "math/bits"

// Decoder implements rfc6716#section-4.1
// Opus uses an entropy coder based on range coding [RANGE-CODING]
// [MARTIN79], which is itself a rediscovery of the FIFO arithmetic code
// introduced by [CODING-THESIS].  It is very similar to arithmetic
// encoding, except that encoding is done with digits in any base
// instead of with bits, so it is faster when using larger bases (i.e.,
// a byte).  All of the calculations in the range coder must use bit-
// exact integer arithmetic.
//
// Symbols may also be coded as "raw bits" packed directly into the
// bitstream, bypassing the range coder.  These are packed backwards
// starting at the end of the frame, as illustrated in Figure 12.  This
// reduces complexity and makes the stream more resilient to bit errors,
// as corruption in the raw bits will not desynchronize the decoding
// process, unlike corruption in the input to the range decoder.  Raw
// bits are only used in the CELT layer.
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
//		Legend:
//
//		LSB = Least Significant Bit
//		MSB = Most Significant Bit
//
//		     Figure 12: Illustrative Example of Packing Range Coder
//		                        and Raw Bits Data
//
// Each symbol coded by the range coder is drawn from a finite alphabet
// and coded in a separate "context", which describes the size of the
// alphabet and the relative frequency of each symbol in that alphabet.
//
// Suppose there is a context with n symbols, identified with an index
// that ranges from 0 to n-1.  The parameters needed to encode or decode
// symbol k in this context are represented by a three-tuple
// (fl[k], fh[k], ft), all 16-bit unsigned integers, with
// 0 <= fl[k] < fh[k] <= ft <= 65535.  The values of this tuple are
// derived from the probability model for the symbol, represented by
// traditional "frequency counts".  Because Opus uses static contexts,
// those are not updated as symbols are decoded.  Let f[i] be the
// frequency of symbol i.  Then, the three-tuple corresponding to symbol
// k is given by the following:
//
//	        k-1                                   n-1
//	        __                                    __
//	fl[k] = \  f[i],  fh[k] = fl[k] + f[k],  ft = \  f[i]
//	        /_                                    /_
//	        i=0                                   i=0
//
// The range decoder extracts the symbols and integers encoded using the
// range encoder in Section 5.1.  The range decoder maintains an
// internal state vector composed of the two-tuple (val, rng), where val
// represents the difference between the high end of the current range
// and the actual coded value, minus one, and rng represents the size of
// the current range.  Both val and rng are 32-bit unsigned integer
// values.
type Decoder struct {
	data        []byte
	bitsRead    uint
	rawBitsRead uint
	nbitsTotal  uint

	rangeSize              uint32 // rng in RFC 6716
	highAndCodedDifference uint32 // val in RFC 6716
}

const (
	maxUniformRangeCoderBits = 8
	laplaceTotal             = 32768
	laplaceMinProbability    = 1
	laplaceGuaranteedDeltas  = 16
)

// Init sets the state of the Decoder
// Let b0 be an 8-bit unsigned integer containing first input byte (or
// containing zero if there are no bytes in this Opus frame).  The
// decoder initializes rng to 128 and initializes val to (127 -
//
//	(b0>>1)), where (b0>>1) is the top 7 bits of the first input byte.
//
// It saves the remaining bit, (b0&1), for use in the renormalization
// procedure described in Section 4.1.2.1, which the decoder invokes
// immediately after initialization to read additional bits and
// establish the invariant that rng > 2**23.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.1.1
func (r *Decoder) Init(data []byte) {
	r.data = data
	r.bitsRead = 0
	r.rawBitsRead = 0
	r.nbitsTotal = 9

	r.rangeSize = 128
	r.highAndCodedDifference = 127 - r.getBits(7)
	r.normalize()
}

// SetStorageSize adjusts the logical frame size without resetting decoder
// state. Opus hybrid redundancy removes tail bytes from the CELT range coder
// after the shared decoder has already consumed SILK symbols.
func (r *Decoder) SetStorageSize(size int) {
	if size < 0 {
		size = 0
	}
	if size < len(r.data) {
		r.data = r.data[:size]
	}
}

// DecodeSymbolWithICDF decodes a single symbol
// with a table-based context of up to 8 bits.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.1.3.3
func (r *Decoder) DecodeSymbolWithICDF(cumulativeDistributionTable []uint) uint32 {
	total := uint32(cumulativeDistributionTable[0]) //nolint:gosec // G115
	cumulativeDistributionTable = cumulativeDistributionTable[1:]

	scale := r.rangeSize / total
	symbol := r.highAndCodedDifference/scale + 1
	symbol = total - uint32(localMin(uint(symbol), uint(total))) //nolint:gosec // G115

	symbolIndex := uint32(0)
	for uint32(cumulativeDistributionTable[symbolIndex]) <= symbol { //nolint:gosec // G115
		symbolIndex++
	}

	high := uint32(cumulativeDistributionTable[symbolIndex]) //nolint:gosec // G115
	low := uint32(0)
	if symbolIndex != 0 {
		low = uint32(cumulativeDistributionTable[symbolIndex-1]) //nolint:gosec // G115
	}

	r.update(scale, low, high, total)

	return symbolIndex
}

func (r *Decoder) decodeUniformSymbol(total uint32) uint32 {
	scale := r.rangeSize / total
	symbol := r.highAndCodedDifference/scale + 1

	return total - uint32(localMin(uint(symbol), uint(total))) //nolint:gosec // G115: symbol is clamped to total.
}

func (r *Decoder) decodeAndUpdateUniformSymbol(total uint32) uint32 {
	symbol := r.decodeUniformSymbol(total)
	r.update(r.rangeSize/total, symbol, symbol+1, total)

	return symbol
}

// DecodeCumulative decodes the cumulative frequency index used by CELT's
// custom range-coded symbols. Call UpdateCumulative with the selected interval.
func (r *Decoder) DecodeCumulative(total uint32) uint32 {
	return r.decodeUniformSymbol(total)
}

// UpdateCumulative commits a custom cumulative interval previously selected
// from DecodeCumulative.
func (r *Decoder) UpdateCumulative(low, high, total uint32) {
	r.update(r.rangeSize/total, low, high, total)
}

// DecodeUniform decodes an RFC 6716 Section 4.1.5 ec_dec_uint() symbol.
//
// It returns false when the decoded raw-bit suffix produces a value outside
// [0,total), in which case the saturated value matches the reference decoder.
func (r *Decoder) DecodeUniform(total uint32) (uint32, bool) {
	if total == 0 {
		return 0, false
	}
	if total == 1 {
		return 0, true
	}

	limit := total - 1
	bitCount := bits.Len32(limit)
	if bitCount <= maxUniformRangeCoderBits {
		return r.decodeAndUpdateUniformSymbol(total), true
	}

	rawBitCount := bitCount - maxUniformRangeCoderBits
	rangeTotal := (limit >> rawBitCount) + 1
	symbol := r.decodeAndUpdateUniformSymbol(rangeTotal)
	value := (symbol << rawBitCount) | r.DecodeRawBits(uint(rawBitCount))
	if value <= limit {
		return value, true
	}

	return limit, false
}

func laplaceFirstDecayFrequency(fs0 uint32, decay uint32) uint32 {
	frequencyTotal := uint32(laplaceTotal) - laplaceMinProbability*(2*laplaceGuaranteedDeltas) - fs0

	return frequencyTotal * (16384 - decay) >> 15
}

// DecodeLaplace decodes the ec_laplace_decode() symbol used by the CELT layer.
//
// RFC 6716 Section 4.3.2.1 describes coarse energy deltas as Laplace-distributed
// prediction errors; the reference implementation decodes them with this helper.
func (r *Decoder) DecodeLaplace(fs0 uint32, decay uint32) int {
	value := 0
	symbol := r.decodeUniformSymbol(laplaceTotal)
	low := uint32(0)
	frequency := fs0

	if symbol >= frequency {
		value++
		low = frequency
		frequency = laplaceFirstDecayFrequency(fs0, decay) + laplaceMinProbability
		for frequency > laplaceMinProbability && symbol >= low+2*frequency {
			frequency *= 2
			low += frequency
			frequency = ((frequency - 2*laplaceMinProbability) * decay) >> 15
			frequency += laplaceMinProbability
			value++
		}
		if frequency <= laplaceMinProbability {
			extra := (symbol - low) >> 1
			value += int(extra)
			low += 2 * extra * laplaceMinProbability
		}
		if symbol < low+frequency {
			value = -value
		} else {
			low += frequency
		}
	}

	high := min(low+frequency, uint32(laplaceTotal))
	r.update(r.rangeSize/laplaceTotal, low, high, laplaceTotal)

	return value
}

// DecodeSymbolLogP decodes a single binary symbol.
// The context is described by a single parameter, logp, which
// is the absolute value of the base-2 logarithm of the probability of a
// "1".
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.1.3.2
func (r *Decoder) DecodeSymbolLogP(logp uint) uint32 {
	scale := r.rangeSize >> logp
	symbol := uint32(0)

	if r.highAndCodedDifference >= scale {
		r.highAndCodedDifference -= scale
		r.rangeSize -= scale
	} else {
		r.rangeSize = scale
		symbol = 1
	}
	r.normalize()

	return symbol
}

func (r *Decoder) getBit() uint32 {
	index := r.bitsRead / 8
	offset := r.bitsRead % 8

	if index >= uint(len(r.data)) {
		return 0
	}

	r.bitsRead++

	return uint32((r.data[index] >> (7 - offset)) & 1)
}

func (r *Decoder) getBits(n int) uint32 {
	bits := uint32(0)

	for i := range n {
		if i != 0 {
			bits <<= 1
		}

		bits |= r.getBit()
	}

	return bits
}

func (r *Decoder) getRawBit() uint32 {
	index := r.rawBitsRead / 8
	offset := r.rawBitsRead % 8
	r.rawBitsRead++
	r.nbitsTotal++
	if index >= uint(len(r.data)) {
		return 0
	}

	byteIndex := len(r.data) - 1 - int(index) //nolint:gosec // G115: index is bounded by len(r.data) above.

	return uint32((r.data[byteIndex] >> offset) & 1)
}

func (r *Decoder) getRawBits(n uint) uint32 {
	var bits uint32

	for i := uint(0); i < n && i < 32; i++ {
		bits |= r.getRawBit() << i
	}
	for i := uint(32); i < n; i++ {
		_ = r.getRawBit()
	}

	return bits
}

// minRangeSize is the minimum allowed size for rng.
// It's equal to math.Pow(2, 23).
const minRangeSize = 1 << 23

// To normalize the range, the decoder repeats the following process,
// implemented by ec_dec_normalize() (entdec.c), until rng > 2**23.  If
// rng is already greater than 2**23, the entire process is skipped.
// First, it sets rng to (rng<<8).  Then, it reads the next byte of the
// Opus frame and forms an 8-bit value sym, using the leftover bit
// buffered from the previous byte as the high bit and the top 7 bits of
// the byte just read as the other 7 bits of sym.  The remaining bit in
// the byte just read is buffered for use in the next iteration.  If no
// more input bytes remain, it uses zero bits instead.  See
// Section 4.1.1 for the initialization used to process the first byte.
// Then, it sets
//
// val = ((val<<8) + (255-sym)) & 0x7FFFFFFF
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.1.2.1
func (r *Decoder) normalize() {
	for r.rangeSize <= minRangeSize {
		r.rangeSize <<= 8
		r.nbitsTotal += 8
		r.highAndCodedDifference = ((r.highAndCodedDifference << 8) + (255 - r.getBits(8))) & 0x7FFFFFFF
	}
}

func (r *Decoder) update(scale, low, high, total uint32) {
	r.highAndCodedDifference -= scale * (total - high)
	if low != 0 {
		r.rangeSize = scale * (high - low)
	} else {
		r.rangeSize -= scale * (total - high)
	}

	r.normalize()
}

// SetInternalValues is used when using the RangeDecoder when testing.
func (r *Decoder) SetInternalValues(data []byte, bitsRead uint, rangeSize uint32, highAndCodedDifference uint32) {
	r.data = data
	r.bitsRead = bitsRead
	r.rawBitsRead = 0
	r.nbitsTotal = bitsRead
	r.rangeSize = rangeSize
	r.highAndCodedDifference = highAndCodedDifference
}

// DecodeRawBits decodes raw bits packed from the end of the frame.
// RFC 6716 Section 4.1.4 defines this LSB-first tail packing for CELT.
func (r *Decoder) DecodeRawBits(n uint) uint32 {
	return r.getRawBits(n)
}

// Tell returns a conservative upper bound, in whole bits, of how many bits
// have been consumed from the current frame, per RFC 6716 Section 4.1.6.1.
func (r *Decoder) Tell() uint {
	lg := uint(bits.Len32(r.rangeSize)) //nolint:gosec // G115: bits.Len32 returns 0..32.
	if lg == 0 {
		return r.nbitsTotal
	}
	if r.nbitsTotal <= lg {
		return 0
	}

	return r.nbitsTotal - lg
}

// TellFrac returns a conservative upper bound in 1/8 bit units.
// This follows the ec_tell_frac() construction in RFC 6716 Section 4.1.6.2.
func (r *Decoder) TellFrac() uint {
	lg := uint(bits.Len32(r.rangeSize)) //nolint:gosec // G115: bits.Len32 returns 0..32.
	if lg == 0 {
		return r.nbitsTotal * 8
	}
	if lg < 24 {
		return r.Tell() * 8
	}

	rQ15 := uint64(r.rangeSize >> (lg - 16))
	for range 3 {
		rQ15 = (rQ15 * rQ15) >> 15
		bit := rQ15 >> 16
		lg = 2*lg + uint(bit)
		if bit != 0 {
			rQ15 >>= 1
		}
	}

	total := r.nbitsTotal * 8
	if total <= lg {
		return 0
	}

	return total - lg
}

// RemainingBits reports a conservative estimate of the unread payload bits.
// RFC 6716 Section 4.1.4 allows range and raw-bit cursor overlap.
func (r *Decoder) RemainingBits() int {
	return len(r.data)*8 - int(r.bitsRead) - int(r.rawBitsRead) //nolint:gosec // G115: decode cursors are frame-sized.
}

// FinalRange exposes the current range coder range state for tests.
func (r *Decoder) FinalRange() uint32 {
	return r.rangeSize
}

func localMin(a, b uint) uint {
	if a < b {
		return a
	}

	return b
}
