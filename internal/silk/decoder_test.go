package silk

import (
	"testing"
)

func TestDecodeSubframeQuantizations(t *testing.T) {
	d := &Decoder{}
	if _, err := d.Decode([]byte{0x0B, 0xE4, 0xC1, 0x36, 0xEC, 0xC5, 0x80}, false, nanoseconds20Ms); err != nil {
		t.Fatal(err)
	}

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
