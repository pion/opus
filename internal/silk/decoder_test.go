package silk

import (
	"errors"
	"reflect"
	"testing"

	"github.com/pion/opus/internal/rangecoding"
)

func testSilkFrame() []byte {
	return []byte{0x0B, 0xE4, 0xC1, 0x36, 0xEC, 0xC5, 0x80}
}

func testResQ10() []int16 {
	return []int16{138, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
}

func testNlsfQ1() []int16 {
	return []int16{2132, 3584, 5504, 7424, 9472, 11392, 13440, 15360, 17280, 19200, 21120, 23040, 25088, 27008, 28928, 30848}
}

func createRangeDecoder(data []byte, bitsRead uint, rangeSize uint32, highAndCodedDifference uint32) rangecoding.Decoder {
	d := rangecoding.Decoder{}
	d.SetInternalValues(data, bitsRead, rangeSize, highAndCodedDifference)
	return d
}

func TestDecode20MsOnly(t *testing.T) {
	d := &Decoder{}
	_, err := d.Decode(testSilkFrame(), false, 1, BandwidthWideband)
	if !errors.Is(err, errUnsupportedSilkFrameDuration) {
		t.Fatal(err)
	}
}

func TestDecodeStereoTODO(t *testing.T) {
	d := &Decoder{}
	_, err := d.Decode(testSilkFrame(), true, nanoseconds20Ms, BandwidthWideband)
	if !errors.Is(err, errUnsupportedSilkStereo) {
		t.Fatal(err)
	}
}

func TestDecodeFrameType(t *testing.T) {
	d := &Decoder{rangeDecoder: createRangeDecoder(testSilkFrame(), 31, 536870912, 437100388)}

	signalType, quantizationOffsetType := d.determineFrameType(false)
	if signalType != frameSignalTypeInactive {
		t.Fatal()
	}
	if quantizationOffsetType != frameQuantizationOffsetTypeHigh {
		t.Fatal()
	}
}

func TestDecodeSubframeQuantizations(t *testing.T) {
	d := &Decoder{rangeDecoder: createRangeDecoder(testSilkFrame(), 31, 482344960, 437100388)}

	d.decodeSubframeQuantizations(frameSignalTypeInactive)

	switch {
	case d.subframeState[0].gain != 3.21875:
		t.Fatal()
	case d.subframeState[1].gain != 1.71875:
		t.Fatal()
	case d.subframeState[2].gain != 1.46875:
		t.Fatal()
	case d.subframeState[3].gain != 1.46875:
		t.Fatal()
	}
}

func TestNormalizeLineSpectralFrequencyStageOne(t *testing.T) {
	d := &Decoder{rangeDecoder: createRangeDecoder(testSilkFrame(), 47, 722810880, 387065757)}

	I1 := d.normalizeLineSpectralFrequencyStageOne(false, BandwidthWideband)
	if I1 != 9 {
		t.Fatal()
	}
}

func TestNormalizeLineSpectralFrequencyStageTwo(t *testing.T) {
	d := &Decoder{rangeDecoder: createRangeDecoder(testSilkFrame(), 47, 50822640, 5895957)}

	resQ10 := d.normalizeLineSpectralFrequencyStageTwo(BandwidthWideband, 9)
	if !reflect.DeepEqual(resQ10, testResQ10()) {
		t.Fatal()
	}
}

func TestNormalizeLineSpectralFrequencyCoefficients(t *testing.T) {
	d := &Decoder{rangeDecoder: createRangeDecoder(testSilkFrame(), 55, 493249168, 174371199)}

	nlsfQ1 := d.normalizeLineSpectralFrequencyCoefficients(BandwidthWideband, testResQ10(), 9)
	if !reflect.DeepEqual(nlsfQ1, testNlsfQ1()) {
		t.Fatal()
	}
}

func TestExcitation(t *testing.T) {
	silkFrame := []byte{0x84, 0x2e, 0x67, 0xd3, 0x85, 0x65, 0x54, 0xe3, 0x9d, 0x90, 0x0a, 0xfa, 0x98, 0xea, 0xfd, 0x98, 0x94, 0x41, 0xf9, 0x6d, 0x1d, 0xa0}
	d := &Decoder{rangeDecoder: createRangeDecoder(silkFrame, 71, 851775140, 846837397)}

	lcgSeed := d.decodeLinearCongruentialGeneratorSeed()
	d.decodeExcitation(nanoseconds20Ms, BandwidthWideband, false, lcgSeed, frameSignalTypeUnvoiced, frameQuantizationOffsetTypeLow)
}
