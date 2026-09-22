// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/pion/opus/pkg/oggreader"
)

// BenchmarkDecodeSteady measures the steady-state, per-frame decode cost. Unlike
// BenchmarkDecode -- which is skipped unless -oggfile is given, and which rebuilds the
// decoder and re-parses the stream on every iteration -- this benchmark runs by default
// on the embedded tiny.ogg: it parses the packets once, reuses a single decoder and
// output buffer, warms up the decoder's scratch buffers, and then loops over the same
// packets. That isolates the recurring per-frame work from one-time setup.
func BenchmarkDecodeSteady(b *testing.B) {
	var pkts [][]byte
	ogg, _, err := oggreader.NewWith(bytes.NewReader(tinyogg))
	if err != nil {
		b.Fatal(err)
	}
	for {
		segments, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			b.Fatal(err)
		}
		if len(segments) > 0 && bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}
		pkts = append(pkts, segments...)
	}

	decoder := NewDecoder()
	var out [1920]byte
	// Warm up the decoder's scratch buffers so the timed loop measures steady state.
	for i := range pkts {
		_, _, _ = decoder.Decode(pkts[i], out[:]) //nolint:errcheck // warm-up only
	}

	b.ResetTimer()
	for range b.N {
		for i := range pkts {
			if _, _, err := decoder.Decode(pkts[i], out[:]); err != nil {
				b.Fatal(err)
			}
		}
	}
}
