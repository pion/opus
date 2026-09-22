// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // The fixture retains its canonical JSON field names.
package celt

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type celtPLCCorpus struct {
	Cases []struct {
		Mode, Rate, Channels int
		OutputChannels       int `json:"output_channels"`
		Steps                []struct {
			Packet  string
			Samples int
		}
	}
}

//nolint:gochecknoglobals // Shared immutable test fixture.
var cachedCELTPLCCorpus = sync.OnceValues(func() (*celtPLCCorpus, error) {
	file, err := os.Open("../../testdata/short-plc/corpus.json.gz")
	if err != nil {
		return nil, fmt.Errorf("open PLC corpus: %w", err)
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open PLC corpus gzip stream: %w", err), file.Close())
	}
	var corpus celtPLCCorpus
	decodeErr := json.NewDecoder(reader).Decode(&corpus)
	closeErr := errors.Join(reader.Close(), file.Close())
	if err = errors.Join(decodeErr, closeErr); err != nil {
		return nil, fmt.Errorf("read PLC corpus: %w", err)
	}

	return &corpus, nil
})

func loadCELTPLCCorpus(tb testing.TB) *celtPLCCorpus {
	tb.Helper()
	corpus, err := cachedCELTPLCCorpus()
	require.NoError(tb, err)

	return corpus
}
