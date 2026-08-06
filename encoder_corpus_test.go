// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build conformance

package opus

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	conformanceWindowsGOOS = "windows"
	envQualityCorpus       = "OPUS_QUALITY_CORPUS"
	// corpusWindowSamples is 10 ms per channel. A codec artifact that is
	// audible as a click lives in one or two of these, and averaging over a
	// whole clip hides it, so they are counted separately from the mean score.
	corpusWindowSamples = 480
	// corpusBadWindowSNRDB is the point below which a window's error is
	// comparable to its own signal — that is what a click measures like.
	corpusBadWindowSNRDB = 3.0
	// corpusSilentWindowEnergy skips windows too quiet for their SNR to mean
	// anything.
	corpusSilentWindowEnergy = 1e4
	corpusRegressionSlack    = 0.15
)

type corpusBaselineClip struct {
	WeightedError float64 `json:"weightedError"`
	BadWindows    int     `json:"badWindows"`
	TotalWindows  int     `json:"totalWindows"`
}

type corpusBaseline struct {
	Version int                           `json:"version"`
	Bitrate int                           `json:"bitrate"`
	Clips   map[string]corpusBaselineClip `json:"clips"`
}

// TestEncoderQualityRealCorpus measures encode quality on real audio rather
// than synthetic signals, which repeatedly disagreed with real material during
// the CELT encoder work: a change could lift one synthetic score, drop another,
// and still be a large win on music.
//
// It reports two numbers per clip. The weighted error from opus_compare is the
// aggregate perceptual score. The bad-window count is the tail: brief spans
// where the error is as large as the signal, which is what a listener hears as
// a click even when the aggregate barely moves.
//
// Set OPUS_QUALITY_CORPUS to a directory of 48 kHz stereo s16le .pcm files and
// OPUS_RFC6716_REFERENCE to a libopus checkout. Set OPUS_QUALITY_CORPUS_UPDATE
// to rewrite the baseline instead of asserting against it.
func TestEncoderQualityRealCorpus(t *testing.T) {
	if runtime.GOOS == conformanceWindowsGOOS {
		t.Skip("the reference tools use a POSIX-oriented Makefile")
	}
	corpusDir := os.Getenv(envQualityCorpus)
	if corpusDir == "" {
		t.Skipf("%s is required to measure quality on real audio", envQualityCorpus)
	}
	refDir := os.Getenv(envRFC6716Reference)
	if refDir == "" {
		t.Skipf("%s is required for opus_compare", envRFC6716Reference)
	}
	_, opusCompare := buildRFC6716ReferenceTools(t, refDir)

	clips, err := filepath.Glob(filepath.Join(corpusDir, "*.pcm"))
	require.NoError(t, err, "scan corpus")
	require.NotEmptyf(t, clips, "no .pcm clips in %s", corpusDir)
	sort.Strings(clips)

	baseline := loadCorpusBaseline(t)
	measured := make(map[string]corpusBaselineClip, len(clips))

	for _, path := range clips {
		name := strings.TrimSuffix(filepath.Base(path), ".pcm")
		t.Run(name, func(t *testing.T) {
			original, readErr := os.ReadFile(path) //nolint:gosec // G304: corpus path from test env var.
			require.NoError(t, readErr)

			decoded := corpusRoundTrip(t, original)
			result := scoreCorpusClip(t, opusCompare, t.TempDir(), original, decoded)

			t.Logf("weighted error %.4f, bad windows %d/%d",
				result.WeightedError, result.BadWindows, result.TotalWindows)
			measured[name] = result

			base, ok := baseline.Clips[name]
			if !ok {
				t.Logf("no baseline for %s yet, recording only", name)

				return
			}
			assert.LessOrEqualf(t, result.WeightedError, base.WeightedError*(1+corpusRegressionSlack),
				"weighted error regressed: %.4f vs baseline %.4f", result.WeightedError, base.WeightedError)
			assert.LessOrEqualf(t, result.BadWindows, base.BadWindows+1,
				"more windows where the error matches the signal: %d vs baseline %d",
				result.BadWindows, base.BadWindows)
		})
	}

	t.Cleanup(func() {
		if os.Getenv("OPUS_QUALITY_CORPUS_UPDATE") == "" {
			return
		}
		writeCorpusBaseline(t, corpusBaseline{
			Version: qualityBaselineVersion,
			Bitrate: qualityTestBitrate,
			Clips:   measured,
		})
	})
}

// corpusRoundTrip encodes and decodes a whole clip through the public API,
// which is the path a caller actually uses.
func corpusRoundTrip(t *testing.T, original []byte) []byte {
	t.Helper()

	encoder, err := NewEncoder(WithChannels(2), WithBitrate(qualityTestBitrate))
	require.NoError(t, err)
	decoder, err := NewDecoderWithOutput(qualityTestRate, 2)
	require.NoError(t, err)

	frameBytes := qualityTestFrameSize * 2 * 2
	packet := make([]byte, maxOpusFrameSize+1)
	pcm := make([]byte, frameBytes)

	decoded := make([]byte, 0, len(original))
	for offset := 0; offset+frameBytes <= len(original); offset += frameBytes {
		n, encErr := encoder.Encode(original[offset:offset+frameBytes], packet)
		require.NoErrorf(t, encErr, "encode at byte %d", offset)
		_, _, decErr := decoder.Decode(packet[:n], pcm)
		require.NoErrorf(t, decErr, "decode at byte %d", offset)
		decoded = append(decoded, pcm...)
	}

	return decoded
}

// scoreCorpusClip aligns the decode against the original and returns both the
// aggregate opus_compare score and the tail metric.
func scoreCorpusClip(t *testing.T, opusCompare, dir string, original, decoded []byte) corpusBaselineClip {
	t.Helper()

	decodedPath := filepath.Join(dir, "decoded.pcm")
	writeQualityBytes(t, decodedPath, decoded)
	trimmedOriginal := filepath.Join(dir, "original-trimmed.pcm")
	trimmedDecoded := filepath.Join(dir, "decoded-trimmed.pcm")
	trimAndAlignPCM(t, original, decodedPath, trimmedOriginal, trimmedDecoded)

	out, err := runOpusCompare(opusCompare, qualityTestRate, 2, trimmedOriginal, trimmedDecoded)
	weighted := opusCompareInternalError(out)
	require.NotEmptyf(t, weighted, "opus_compare produced no score: %v\n%s", err, out)
	score, parseErr := strconv.ParseFloat(weighted, 64)
	require.NoError(t, parseErr, "parse weighted error %q", weighted)

	alignedOriginal, err := os.ReadFile(trimmedOriginal) //nolint:gosec // G304: path built above.
	require.NoError(t, err)
	alignedDecoded, err := os.ReadFile(trimmedDecoded) //nolint:gosec // G304: path built above.
	require.NoError(t, err)
	bad, total := countBadWindows(alignedOriginal, alignedDecoded)

	return corpusBaselineClip{WeightedError: score, BadWindows: bad, TotalWindows: total}
}

// countBadWindows returns how many 10 ms windows carry as much error as
// signal. Both inputs must already be aligned interleaved stereo s16le.
func countBadWindows(original, decoded []byte) (bad, total int) {
	samples := min(len(original), len(decoded)) / 4
	for start := 0; start+corpusWindowSamples <= samples; start += corpusWindowSamples {
		var signal, noise float64
		for i := start; i < start+corpusWindowSamples; i++ {
			for channel := range 2 {
				offset := i*4 + channel*2
				//nolint:gosec // G115: little-endian s16 round-trip.
				a := float64(int16(binary.LittleEndian.Uint16(original[offset:])))
				//nolint:gosec // G115: little-endian s16 round-trip.
				b := float64(int16(binary.LittleEndian.Uint16(decoded[offset:])))
				signal += a * a
				noise += (b - a) * (b - a)
			}
		}
		total++
		if signal/float64(2*corpusWindowSamples) < corpusSilentWindowEnergy || noise == 0 {
			continue
		}
		if 10*math.Log10(signal/noise) < corpusBadWindowSNRDB {
			bad++
		}
	}

	return bad, total
}

func corpusBaselinePath() string {
	return filepath.Join("testdata", "encoder-quality-corpus-baseline.json")
}

func loadCorpusBaseline(t *testing.T) corpusBaseline {
	t.Helper()

	data, err := os.ReadFile(corpusBaselinePath()) //nolint:gosec // G304: fixed testdata path.
	if err != nil {
		t.Logf("no corpus baseline yet, first run records only")

		return corpusBaseline{Version: qualityBaselineVersion, Clips: map[string]corpusBaselineClip{}}
	}
	var baseline corpusBaseline
	require.NoError(t, json.Unmarshal(data, &baseline), "parse corpus baseline")
	if baseline.Clips == nil {
		baseline.Clips = map[string]corpusBaselineClip{}
	}

	return baseline
}

func writeCorpusBaseline(t *testing.T, baseline corpusBaseline) {
	t.Helper()

	data, err := json.MarshalIndent(baseline, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(corpusBaselinePath(), append(data, '\n'), 0o600))
	t.Logf("wrote %s", corpusBaselinePath())
}
