// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/pion/opus/pkg/oggreader"
	"github.com/stretchr/testify/assert"
)

// nolint: gochecknoglobals
var (
	testoggfile = flag.String("oggfile", "", "ogg file for benchmark")
	_testogg    struct {
		once sync.Once
		err  error
		data []byte
	}
)

func loadTestOgg(tb testing.TB) []byte {
	tb.Helper()

	if *testoggfile == "" {
		tb.Skip("-oggfile not specified")
	}

	_testogg.once.Do(func() {
		_testogg.data, _testogg.err = os.ReadFile(*testoggfile)
	})
	if _testogg.err != nil {
		tb.Fatal("unable to load -oggfile", _testogg.err)
	}

	return _testogg.data
}

func BenchmarkDecode(b *testing.B) {
	data := loadTestOgg(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkData(b, data)
	}
}

func benchmarkData(b *testing.B, data []byte) {
	b.Helper()

	var out [1920]byte
	ogg, _, err := oggreader.NewWith(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}

	decoder := NewDecoder()
	for {
		segments, _, err := ogg.ParseNextPage()

		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}

		if err != nil {
			b.Fatal(err)
		}

		for i := range segments {
			if _, _, err = decoder.Decode(segments[i], out[:]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

//go:embed testdata/tiny.ogg
var tinyogg []byte // nolint: gochecknoglobals

func TestTinyOgg(t *testing.T) {
	var out [1920]byte

	ogg, _, err := oggreader.NewWith(bytes.NewReader(tinyogg))
	assert.NoError(t, err)

	decoder := NewDecoder()
	for {
		segments, _, err := ogg.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		} else if bytes.HasPrefix(segments[0], []byte("OpusTags")) {
			continue
		}
		assert.NoError(t, err)

		for i := range segments {
			if _, _, err = decoder.Decode(segments[i], out[:]); err != nil {
				return
			}
		}
	}
}
