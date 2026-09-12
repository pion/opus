// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build plc_audio

package opus

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWritePLCAudioExamples writes aligned, header-only WAV files from the
// checked reference PCM and the decoder under test. It is opt-in so ordinary
// test runs remain read-only and do not carry artifact-generation overhead.
func TestWritePLCAudioExamples(t *testing.T) {
	outputDir := os.Getenv("PLC_AUDIO_OUTPUT")
	if outputDir == "" {
		t.Skip("PLC_AUDIO_OUTPUT is required")
	}
	label := os.Getenv("PLC_AUDIO_LABEL")
	if label == "" {
		label = "current"
	}
	corpusPath := os.Getenv("PLC_CORPUS_PATH")
	if corpusPath == "" {
		corpusPath = "testdata/short-plc/corpus.json.gz"
	}
	file, err := os.Open(corpusPath) //nolint:gosec // Explicit offline fixture path.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	var corpus plcAudioCorpus
	require.NoError(t, json.NewDecoder(reader).Decode(&corpus))
	require.NoError(t, os.MkdirAll(outputDir, 0o755))

	selections := []struct {
		name                                              string
		signal, channels, outputChannels, rate, mode, seq int
	}{
		{"celt-periodic-mono", 0, 1, 1, 48000, 0, 0},
		{"hybrid-rfc8251-stereo", 6, 2, 2, 48000, 2, 0},
		{"silk-rfc8251-stereo", 6, 2, 2, 48000, 4, 0},
	}
	for _, selection := range selections {
		t.Run(selection.name, func(t *testing.T) {
			scenario := findPLCAudioCase(t, &corpus, selection.signal, selection.channels,
				selection.outputChannels, selection.rate, selection.mode, selection.seq)
			decoder, decoderErr := NewDecoderWithOutput(scenario.Rate, scenario.OutputChannels)
			require.NoError(t, decoderErr)
			var actual, reference []int16
			for step, frame := range scenario.Steps {
				out := make([]int16, frame.Samples*scenario.OutputChannels)
				if frame.Packet == "" {
					require.NoError(t, decoder.DecodePLC(out), "step %d", step)
				} else {
					packet, decodeErr := hex.DecodeString(frame.Packet)
					require.NoError(t, decodeErr)
					count, decodeErr := decoder.DecodeToInt16(packet, out)
					require.NoError(t, decodeErr, "step %d", step)
					require.Equal(t, frame.Samples, count, "step %d", step)
				}
				actual = append(actual, out...)
				reference = append(reference, frame.PCM...)
			}
			actualPath := filepath.Join(outputDir, label+"-"+selection.name+".wav")
			referencePath := filepath.Join(outputDir, "reference-"+selection.name+".wav")
			writePCM16WAV(t, actualPath, scenario.Rate, scenario.OutputChannels, actual)
			writePCM16WAV(t, referencePath, scenario.Rate, scenario.OutputChannels, reference)
			t.Logf("%s sha256=%x", actualPath, sha256.Sum256(mustReadFile(t, actualPath)))
			t.Logf("%s sha256=%x", referencePath, sha256.Sum256(mustReadFile(t, referencePath)))
		})
	}
}

type plcAudioCorpus struct {
	Cases []plcAudioCase
}

type plcAudioCase struct {
	Signal, Channels, Rate, Mode, Sequence int
	OutputChannels                         int `json:"output_channels"`
	Steps                                  []struct {
		Packet  string
		Samples int
		PCM     []int16
	}
}

func findPLCAudioCase(
	t *testing.T,
	corpus *plcAudioCorpus,
	signal, channels, outputChannels, rate, mode, sequence int,
) *plcAudioCase {
	t.Helper()
	for i := range corpus.Cases {
		candidate := &corpus.Cases[i]
		if candidate.Signal == signal && candidate.Channels == channels &&
			candidate.OutputChannels == outputChannels && candidate.Rate == rate &&
			candidate.Mode == mode && candidate.Sequence == sequence {
			return candidate
		}
	}
	t.Fatalf("audio case not found: signal=%d channels=%d output=%d rate=%d mode=%d sequence=%d",
		signal, channels, outputChannels, rate, mode, sequence)

	return nil
}

func writePCM16WAV(t *testing.T, path string, sampleRate, channels int, samples []int16) {
	t.Helper()
	file, err := os.Create(path) //nolint:gosec // Explicit opt-in local artifact path.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	dataBytes := len(samples) * 2
	header := []any{
		[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + dataBytes), [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(channels), uint32(sampleRate),
		uint32(sampleRate * channels * 2), uint16(channels * 2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, uint32(dataBytes),
	}
	for _, value := range header {
		require.NoError(t, binary.Write(file, binary.LittleEndian, value))
	}
	require.NoError(t, binary.Write(file, binary.LittleEndian, samples))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // Path was produced by this test.
	require.NoError(t, err)

	return data
}
