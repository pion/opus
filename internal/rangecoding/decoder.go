package rangecoding

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
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	| Range coder data (packed MSB to LSB) ->                       :
//	+                                                               +
//	:                                                               :
//	+     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	:     | <- Boundary occurs at an arbitrary bit position         :
//	+-+-+-+                                                         +
//	:                          <- Raw bits data (packed LSB to MSB) |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
//	Legend:
//
//	LSB = Least Significant Bit
//	MSB = Most Significant Bit
//
//	     Figure 12: Illustrative Example of Packing Range Coder
//	                        and Raw Bits Data
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
	data     []byte
	bitsRead uint

	rangeSize              uint32 // rng in RFC 6716
	highAndCodedDifference uint32 // val in RFC 6716
}

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

	r.rangeSize = 128
	r.highAndCodedDifference = 127 - r.getBits(7)
	r.normalize()
}

// DecodeSymbolWithICDF decodes a single symbol
// with a table-based context of up to 8 bits.
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.1.3.3
func (r *Decoder) DecodeSymbolWithICDF(cumulativeDistributionTable []uint) uint32 {
	var k, scale, total, symbol, low, high uint32

	total = uint32(cumulativeDistributionTable[0])
	cumulativeDistributionTable = cumulativeDistributionTable[1:]

	scale = r.rangeSize / total
	symbol = r.highAndCodedDifference/scale + 1
	symbol = total - uint32(min(uint(symbol), uint(total)))

	for k = 0; uint32(cumulativeDistributionTable[k]) <= symbol; k++ {
	}

	high = uint32(cumulativeDistributionTable[k])
	if k != 0 {
		low = uint32(cumulativeDistributionTable[k-1])
	} else {
		low = 0
	}

	r.update(scale, low, high, total)
	return k
}

// DecodeSymbolLogP decodes a single binary symbol.
// The context is described by a single parameter, logp, which
// is the absolute value of the base-2 logarithm of the probability of a
// "1".
//
// https://datatracker.ietf.org/doc/html/rfc6716#section-4.1.3.2
func (r *Decoder) DecodeSymbolLogP(logp uint) uint32 {
	k := uint32(0)
	scale := r.rangeSize >> logp

	if r.highAndCodedDifference >= scale {
		r.highAndCodedDifference -= scale
		r.rangeSize -= scale
		k = 0
	} else {
		r.rangeSize = scale
		k = 1
	}
	r.normalize()

	return k
}

func (r *Decoder) getBit() uint32 {
	index := r.bitsRead / 8
	offset := r.bitsRead % 8

	if index > uint(len(r.data)-1) {
		return 0
	}

	r.bitsRead++
	return uint32((r.data[index] >> (7 - offset)) & 1)

}

func (r *Decoder) getBits(n int) uint32 {
	bits := uint32(0)

	for i := 0; i < n; i++ {
		if i != 0 {
			bits = bits << 1
		}

		bits |= r.getBit()
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
		r.highAndCodedDifference = ((r.highAndCodedDifference << 8) + (255 - r.getBits(8))) & 0x7FFFFFFF
	}
}

func (r *Decoder) update(scale, low, high, total uint32) {
	r.highAndCodedDifference -= scale * (total - high)
	if low != 0 {
		r.rangeSize = scale * (high - low)
	} else {
		r.rangeSize = r.rangeSize - scale*(total-high)
	}

	r.normalize()
}

// SetInternalValues is used when using the RangeDecoder when testing
func (r *Decoder) SetInternalValues(data []byte, bitsRead uint, rangeSize uint32, highAndCodedDifference uint32) {
	r.data = data
	r.bitsRead = bitsRead
	r.rangeSize = rangeSize
	r.highAndCodedDifference = highAndCodedDifference
}

func min(a, b uint) uint {
	if a < b {
		return a
	}
	return b
}
