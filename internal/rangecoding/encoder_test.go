// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package rangecoding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

//nolint:gochecknoglobals
var testICDFTable = []uint{256, 32, 160, 256}

func TestEncoderRoundTrip(t *testing.T) {
	encoder := &Encoder{}
	encoder.Init()
	encoder.EncodeSymbolLogP(1, 0)
	encoder.EncodeSymbolLogP(3, 1)
	encoder.EncodeSymbolWithICDF(testICDFTable, 2)
	encoder.EncodeUniform(6, 4)
	encoder.EncodeUniform(300, 257)
	encoder.EncodeLaplace(72<<7, 127<<6, -2)
	encoder.EncodeLaplace(72<<7, 127<<6, 3)
	encoder.EncodeRawBits(5, 0x16)
	encoder.EncodeRawBits(11, 0x5A3)

	packet := encoder.Done()
	decoder := &Decoder{}
	decoder.Init(packet)

	assert.Equal(t, uint32(0), decoder.DecodeSymbolLogP(1))
	assert.Equal(t, uint32(1), decoder.DecodeSymbolLogP(3))
	assert.Equal(t, uint32(2), decoder.DecodeSymbolWithICDF(testICDFTable))

	value, ok := decoder.DecodeUniform(6)
	assert.True(t, ok)
	assert.Equal(t, uint32(4), value)

	value, ok = decoder.DecodeUniform(300)
	assert.True(t, ok)
	assert.Equal(t, uint32(257), value)

	assert.Equal(t, -2, decoder.DecodeLaplace(72<<7, 127<<6))
	assert.Equal(t, 3, decoder.DecodeLaplace(72<<7, 127<<6))
	assert.Equal(t, uint32(0x16), decoder.DecodeRawBits(5))
	assert.Equal(t, uint32(0x5A3), decoder.DecodeRawBits(11))
	assert.NotZero(t, decoder.FinalRange())
}

func TestEncoderCumulativeRoundTrip(t *testing.T) {
	encoder := &Encoder{}
	encoder.Init()
	encoder.EncodeCumulative(2, 3, 5)
	encoder.EncodeRawBits(3, 0x05)

	packet := encoder.Done()
	decoder := &Decoder{}
	decoder.Init(packet)

	symbol := decoder.DecodeCumulative(5)
	decoder.UpdateCumulative(symbol, symbol+1, 5)

	assert.Equal(t, uint32(2), symbol)
	assert.Equal(t, uint32(0x05), decoder.DecodeRawBits(3))
}

func TestEncoderDoneEmptyFrame(t *testing.T) {
	encoder := &Encoder{}
	encoder.Init()

	packet := encoder.Done()

	assert.NotNil(t, packet)
	decoder := &Decoder{}
	assert.NotPanics(t, func() {
		decoder.Init(packet)
	})
}

func TestEncoderFinalRangeMatchesDecoder(t *testing.T) {
	encoder := &Encoder{}
	encoder.Init()
	encoder.EncodeSymbolLogP(3, 1)
	encoder.EncodeUniform(6, 4)
	encoder.EncodeLaplace(72<<7, 127<<6, 2)

	packet := encoder.Done()
	decoder := &Decoder{}
	decoder.Init(packet)

	decoder.DecodeSymbolLogP(3)
	decoder.DecodeUniform(6)
	decoder.DecodeLaplace(72<<7, 127<<6)

	assert.Equal(t, encoder.FinalRange(), decoder.FinalRange())
}

func TestEncoderTell(t *testing.T) {
	t.Run("reports one bit after initialization", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()

		assert.Equal(t, uint(1), encoder.Tell())
		assert.Equal(t, uint(8), encoder.TellFrac())
	})

	t.Run("increases after raw bits are encoded", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		tellBefore := encoder.Tell()

		encoder.EncodeRawBits(8, 0xAB)

		assert.Equal(t, tellBefore+8, encoder.Tell())
	})

	t.Run("matches decoder Tell after the same symbols", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		encoder.EncodeSymbolLogP(1, 0)
		encoder.EncodeSymbolLogP(3, 1)
		encoder.EncodeRawBits(8, 0xFF)
		encoderTell := encoder.Tell()

		packet := encoder.Done()
		decoder := &Decoder{}
		decoder.Init(packet)
		decoder.DecodeSymbolLogP(1)
		decoder.DecodeSymbolLogP(3)
		decoder.DecodeRawBits(8)

		assert.Equal(t, encoderTell, decoder.Tell())
	})

	t.Run("TellFrac does not underflow on fresh encoder", func(t *testing.T) {
		encoder := &Encoder{rangeSize: 1 << 31}

		assert.NotPanics(t, func() {
			_ = encoder.TellFrac()
		})
	})
}

func TestEncoderUniformEdgeCases(t *testing.T) {
	t.Run("total of one is a no-op", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		tellBefore := encoder.Tell()

		encoder.EncodeUniform(1, 0)

		assert.Equal(t, tellBefore, encoder.Tell())
	})

	t.Run("symbol clamped to total-1 still decodes", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		encoder.EncodeUniform(6, 99)

		packet := encoder.Done()
		decoder := &Decoder{}
		decoder.Init(packet)

		got, ok := decoder.DecodeUniform(6)
		assert.True(t, ok)
		assert.Equal(t, uint32(5), got)
	})

	t.Run("round-trips all values in a small alphabet", func(t *testing.T) {
		for symbol := range uint32(6) {
			encoder := &Encoder{}
			encoder.Init()
			encoder.EncodeUniform(6, symbol)

			packet := encoder.Done()
			decoder := &Decoder{}
			decoder.Init(packet)

			got, ok := decoder.DecodeUniform(6)
			assert.True(t, ok)
			assert.Equal(t, symbol, got, "symbol %d", symbol)
		}
	})
}

func TestEncoderRawBitsEdgeCases(t *testing.T) {
	t.Run("zero bits is a no-op", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		tellBefore := encoder.Tell()

		encoder.EncodeRawBits(0, 0xFF)

		assert.Equal(t, tellBefore, encoder.Tell())
	})

	t.Run("rounds trip a full byte", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		encoder.EncodeRawBits(8, 0xB2)

		packet := encoder.Done()
		decoder := &Decoder{}
		decoder.Init(packet)

		assert.Equal(t, uint32(0xB2), decoder.DecodeRawBits(8))
	})

	t.Run("high bits beyond n are masked", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		encoder.EncodeRawBits(4, 0xFF)

		packet := encoder.Done()
		decoder := &Decoder{}
		decoder.Init(packet)

		assert.Equal(t, uint32(0x0F), decoder.DecodeRawBits(4))
	})
}

func TestEncoderLaplaceEdgeCases(t *testing.T) {
	zeroFrequency := uint32(72 << 7)
	decay := uint32(127 << 6)

	for _, test := range []struct {
		name  string
		value int
	}{
		{name: "zero delta", value: 0},
		{name: "first positive delta", value: 1},
		{name: "first negative delta", value: -1},
		{name: "large positive delta", value: 10},
		{name: "large negative delta", value: -10},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoder := &Encoder{}
			encoder.Init()
			encoder.EncodeLaplace(zeroFrequency, decay, test.value)

			packet := encoder.Done()
			decoder := &Decoder{}
			decoder.Init(packet)

			assert.Equal(t, test.value, decoder.DecodeLaplace(zeroFrequency, decay))
		})
	}
}

func TestEncoderICDFEdgeCases(t *testing.T) {
	t.Run("encodes all valid symbols in the table", func(t *testing.T) {
		for symbol := range len(testICDFTable) - 1 {
			symbol := uint32(symbol)
			encoder := &Encoder{}
			encoder.Init()
			encoder.EncodeSymbolWithICDF(testICDFTable, symbol)

			packet := encoder.Done()
			decoder := &Decoder{}
			decoder.Init(packet)

			assert.Equal(t, symbol, decoder.DecodeSymbolWithICDF(testICDFTable), "symbol %d", symbol)
		}
	})

	t.Run("ignores too-short table", func(t *testing.T) {
		encoder := &Encoder{}
		encoder.Init()
		tellBefore := encoder.Tell()

		encoder.EncodeSymbolWithICDF([]uint{256}, 0)

		assert.Equal(t, tellBefore, encoder.Tell())
	})
}

func FuzzEncoderRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{255, 128, 64, 32, 16, 8, 4, 2})
	f.Add([]byte{9, 7, 5, 3, 1, 0, 2, 4, 6, 8})

	f.Fuzz(func(t *testing.T, data []byte) {
		encoder := &Encoder{}
		encoder.Init()

		ops := make([]fuzzOperation, 0, len(data))
		for index := range data {
			switch data[index] % 5 {
			case 0:
				logp := uint(data[index]%7 + 1)
				symbol := uint32(data[index] & 1)
				encoder.EncodeSymbolLogP(logp, symbol)
				ops = append(ops, fuzzOperation{kind: 0, logp: logp, symbol: symbol})
			case 1:
				symbol := uint32(data[index] % 3)
				encoder.EncodeSymbolWithICDF(testICDFTable, symbol)
				ops = append(ops, fuzzOperation{kind: 1, symbol: symbol, icdfTable: testICDFTable})
			case 2:
				total := uint32(data[index]) + 2
				symbol := uint32(data[index]>>1) % total
				encoder.EncodeUniform(total, symbol)
				ops = append(ops, fuzzOperation{kind: 2, total: total, symbol: symbol})
			case 3:
				value := int(data[index]%7) - 3
				encoder.EncodeLaplace(72<<7, 127<<6, value)
				ops = append(ops, fuzzOperation{kind: 3, laplace: value})
			default:
				rawBits := uint(data[index] % 17)
				rawValue := uint32(data[index])
				if index+1 < len(data) {
					rawValue = uint32(data[index]) | uint32(data[index+1])<<8
				}
				if rawBits < 32 {
					rawValue &= bitMask(rawBits)
				}
				encoder.EncodeRawBits(rawBits, rawValue)
				ops = append(ops, fuzzOperation{kind: 4, rawBits: rawBits, rawValue: rawValue})
			}
		}

		packet := encoder.Done()
		decoder := &Decoder{}
		decoder.Init(packet)

		for _, op := range ops {
			assertFuzzOperation(t, decoder, op)
		}
	})
}

type fuzzOperation struct {
	kind      byte
	logp      uint
	symbol    uint32
	total     uint32
	laplace   int
	rawBits   uint
	rawValue  uint32
	icdfTable []uint
}

func assertFuzzOperation(t *testing.T, decoder *Decoder, op fuzzOperation) {
	t.Helper()

	switch op.kind {
	case 0:
		assert.Equal(t, op.symbol, decoder.DecodeSymbolLogP(op.logp))
	case 1:
		assert.Equal(t, op.symbol, decoder.DecodeSymbolWithICDF(op.icdfTable))
	case 2:
		got, ok := decoder.DecodeUniform(op.total)
		assert.True(t, ok)
		assert.Equal(t, op.symbol, got)
	case 3:
		assert.Equal(t, op.laplace, decoder.DecodeLaplace(72<<7, 127<<6))
	case 4:
		assert.Equal(t, op.rawValue, decoder.DecodeRawBits(op.rawBits))
	}
}
