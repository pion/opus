// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

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

const defaultPLCCorpusPath = "testdata/short-plc/corpus.json.gz"

//nolint:gochecknoglobals // Shared immutable test fixture.
var cachedPLCCorpus = sync.OnceValues(func() (*plcCorpus, error) {
	return readPLCCorpus(defaultPLCCorpusPath)
})

func loadPLCCorpus(tb testing.TB) *plcCorpus {
	tb.Helper()
	corpus, err := cachedPLCCorpus()
	require.NoError(tb, err)

	return corpus
}

func readPLCCorpus(path string) (*plcCorpus, error) {
	file, err := os.Open(path) //nolint:gosec // Test helper accepts explicit offline fixture paths.
	if err != nil {
		return nil, fmt.Errorf("open PLC corpus: %w", err)
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open PLC corpus gzip stream: %w", err), file.Close())
	}
	var corpus plcCorpus
	decodeErr := json.NewDecoder(reader).Decode(&corpus)
	closeErr := errors.Join(reader.Close(), file.Close())
	if err = errors.Join(decodeErr, closeErr); err != nil {
		return nil, fmt.Errorf("read PLC corpus: %w", err)
	}

	return &corpus, nil
}
