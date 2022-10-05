package oggreader

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

// buildOggFile generates a valid oggfile that can
// be used for tests
func buildOggContainer() []byte {
	return []byte{
		0x4f, 0x67, 0x67, 0x53, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x8e, 0x9b, 0x20, 0xaa, 0x00, 0x00,
		0x00, 0x00, 0x61, 0xee, 0x61, 0x17, 0x01, 0x13, 0x4f, 0x70,
		0x75, 0x73, 0x48, 0x65, 0x61, 0x64, 0x01, 0x02, 0x00, 0x0f,
		0x80, 0xbb, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4f, 0x67, 0x67,
		0x53, 0x00, 0x00, 0xda, 0x93, 0xc2, 0xd9, 0x00, 0x00, 0x00,
		0x00, 0x8e, 0x9b, 0x20, 0xaa, 0x02, 0x00, 0x00, 0x00, 0x49,
		0x97, 0x03, 0x37, 0x01, 0x05, 0x98, 0x36, 0xbe, 0x88, 0x9e,
	}
}

func TestOggReader_ParseValidHeader(t *testing.T) {
	reader, header, err := NewWith(bytes.NewReader(buildOggContainer()))
	switch {
	case err != nil:
		t.Fatal()
	case reader == nil:
		t.Fatal()
	case header == nil:
		t.Fatal()
	case !reflect.DeepEqual(header.ChannelMap, uint8(0)):
		t.Fatal()
	case !reflect.DeepEqual(header.Channels, uint8(2)):
		t.Fatal()
	case !reflect.DeepEqual(header.OutputGain, uint16(0)):
		t.Fatal()
	case !reflect.DeepEqual(header.PreSkip, uint16(0xf00)):
		t.Fatal()
	case !reflect.DeepEqual(header.SampleRate, uint32(48000)):
		t.Fatal()
	case !reflect.DeepEqual(header.Version, uint8(1)):
		t.Fatal()
	}
}

func TestOggReader_ParseNextPage(t *testing.T) {
	ogg := bytes.NewReader(buildOggContainer())
	reader, _, err := NewWith(ogg)
	switch {
	case err != nil:
		t.Fatal()
	case reader == nil:
		t.Fatal()
	}

	payload, _, err := reader.ParseNextPage()
	switch {
	case err != nil:
		t.Fatal()
	case !reflect.DeepEqual([][]byte{{0x98, 0x36, 0xbe, 0x88, 0x9e}}, payload):
		t.Fatal()
	}

	_, _, err = reader.ParseNextPage()
	if !errors.Is(err, io.EOF) {
		t.Fatal()
	}
}

func TestOggReader_ParseErrors(t *testing.T) {
	t.Run("Assert that Reader isn't nil", func(t *testing.T) {
		_, _, err := NewWith(nil)
		if !errors.Is(err, errNilStream) {
			t.Fatal()
		}
	})

	t.Run("Invalid ID Page Header Signature", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[0] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		if !errors.Is(err, errBadIDPageSignature) {
			t.Fatal()
		}
	})

	t.Run("Invalid ID Page Header Type", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[5] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		if !errors.Is(err, errBadIDPageType) {
			t.Fatal()
		}
	})

	t.Run("Invalid ID Page Payload Length", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[27] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		if !errors.Is(err, errBadIDPageLength) {
			t.Fatal()
		}
	})

	t.Run("Invalid ID Page Payload Length", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[35] = 0

		_, _, err := newWith(bytes.NewReader(ogg), false)
		if !errors.Is(err, errBadIDPagePayloadSignature) {
			t.Fatal()
		}
	})

	t.Run("Invalid Page Checksum", func(t *testing.T) {
		ogg := buildOggContainer()
		ogg[22] = 0

		_, _, err := NewWith(bytes.NewReader(ogg))
		if !errors.Is(err, errChecksumMismatch) {
			t.Fatal()
		}
	})
}
