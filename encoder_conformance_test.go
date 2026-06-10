// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build conformance

package opus

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	encoderConformanceRate       = 48000
	encoderConformanceFrameSize  = 960
	encoderConformanceFrameCount = 100
	encoderConformanceMaxLag     = 2048
)

type encoderConformanceCase struct {
	name     string
	channels int
	bitrate  int
	sample   func(i, channel int) float64
}

// TestRFC6716ConformanceEncoder encodes with the Go encoder and decodes with
// the reference opus_demo. A bug that is symmetric between the Go encoder and
// the Go decoder passes every round-trip test in the repo; decoding with the
// reference is the only way to catch it. The quality scores against the
// original signal (and the reference encoder baseline) are printed but not
// asserted.
func TestRFC6716ConformanceEncoder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("RFC 6716 conformance uses the POSIX-oriented reference Makefile")
	}

	refDir := os.Getenv(envRFC6716Reference)
	if refDir == "" {
		t.Skipf("%s is required to run RFC 6716 encoder conformance", envRFC6716Reference)
	}

	opusDemo, opusCompare := buildRFC6716ReferenceTools(t, refDir)

	cases := []encoderConformanceCase{
		{
			name:     "mono_sine",
			channels: 1,
			bitrate:  64000,
			sample: func(i, _ int) float64 {
				return encoderConformanceTone(i, 440, 17)
			},
		},
		{
			name:     "stereo_tones",
			channels: 2,
			bitrate:  96000,
			sample: func(i, channel int) float64 {
				if channel == 1 {
					return encoderConformanceTone(i, 660, 23)
				}

				return encoderConformanceTone(i, 440, 17)
			},
		},
		{
			name:     "stereo_wide",
			channels: 2,
			bitrate:  96000,
			sample: func(i, channel int) float64 {
				if channel == 1 {
					return encoderConformanceTone(i, 3000, 23)
				}

				return encoderConformanceTone(i, 440, 17)
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runEncoderConformanceCase(t, opusDemo, opusCompare, testCase)
		})
	}
}

// encoderConformanceTone applies amplitude modulation so estimateCodecDelay
// can find a unique peak: with a pure tone every lag one period apart
// correlates equally.
func encoderConformanceTone(i int, freq, modFreq float64) float64 {
	tSeconds := float64(i) / encoderConformanceRate
	envelope := 0.6 + 0.4*math.Sin(2*math.Pi*modFreq*tSeconds)

	return 0.5 * envelope * math.Sin(2*math.Pi*freq*tSeconds)
}

func runEncoderConformanceCase(
	t *testing.T,
	opusDemo, opusCompare string,
	testCase encoderConformanceCase,
) {
	t.Helper()

	dir := t.TempDir()
	originalPCM := filepath.Join(dir, "original.pcm")
	originalStereoPCM := filepath.Join(dir, "original-stereo.pcm")
	goBitstream := filepath.Join(dir, "go.bit")
	referenceDecodePCM := filepath.Join(dir, "refdec.pcm")
	goDecodePCM := filepath.Join(dir, "godec.pcm")

	original, originalStereo := writeEncoderConformanceSignal(
		t, originalPCM, originalStereoPCM, testCase,
	)
	encodeWithGo(t, original, testCase.channels, testCase.bitrate, goBitstream)

	runReferenceOpusDemo(
		t, opusDemo, "decode Go bitstream with reference",
		"-d", strconv.Itoa(encoderConformanceRate), "2",
		goBitstream, referenceDecodePCM,
	)
	decodeBitstreamWithGo(t, goBitstream, goDecodePCM)

	// The reference decode and the Go decode of the same bitstream must match;
	// opus_compare exits non-zero below its conformance threshold.
	out, err := runOpusCompare(
		opusCompare, encoderConformanceRate, 2,
		referenceDecodePCM, goDecodePCM,
	)
	if err != nil {
		t.Fatalf("opus_compare reference decode vs Go decode: %v\n%s", err, out)
	}
	printOpusCompareQuality(t, opusCompareQuality(out))

	logEncoderQuality(
		t, opusCompare, "Go encoder vs original",
		originalStereo, referenceDecodePCM, dir, "go",
	)
	logReferenceEncoderBaseline(
		t, opusDemo, opusCompare, testCase, originalStereo, originalPCM, dir,
	)
}

// writeEncoderConformanceSignal writes the s16le encode input plus a stereo
// copy: opus_compare always reads its first file as interleaved stereo (mono
// mode downmixes it), so every comparison runs on stereo PCM and mono cases
// duplicate the channel.
func writeEncoderConformanceSignal(
	t *testing.T,
	path, stereoPath string,
	testCase encoderConformanceCase,
) (original, originalStereo []byte) {
	t.Helper()

	sampleCount := encoderConformanceFrameSize * encoderConformanceFrameCount
	original = make([]byte, sampleCount*testCase.channels*2)
	originalStereo = make([]byte, sampleCount*4)
	for i := range sampleCount {
		for channel := range 2 {
			sourceChannel := min(channel, testCase.channels-1)
			value := testCase.sample(i, sourceChannel)
			sample := uint16(int16(math.Round(value * 32767)))
			binary.LittleEndian.PutUint16(originalStereo[(i*2+channel)*2:], sample)
			if channel < testCase.channels {
				binary.LittleEndian.PutUint16(original[(i*testCase.channels+channel)*2:], sample)
			}
		}
	}

	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write original PCM: %v", err)
	}
	if err := os.WriteFile(stereoPath, originalStereo, 0o600); err != nil {
		t.Fatalf("write stereo original PCM: %v", err)
	}

	return original, originalStereo
}

// encodeWithGo writes packets in the opus_demo framing: payload length and
// encoder final range, both 4-byte big-endian, before each payload. opus_demo
// checks a non-zero final range against its own decode, so this also verifies
// range coder sync with the reference.
func encodeWithGo(t *testing.T, pcm []byte, channels, bitrate int, path string) {
	t.Helper()

	encoder, err := NewEncoder(WithChannels(channels), WithBitrate(bitrate))
	if err != nil {
		t.Fatalf("create Go encoder: %v", err)
	}

	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create Go bitstream: %v", err)
	}
	defer out.Close()

	frameBytes := encoderConformanceFrameSize * channels * 2
	packet := make([]byte, maxOpusFrameSize+1)
	for offset := 0; offset+frameBytes <= len(pcm); offset += frameBytes {
		n, err := encoder.Encode(pcm[offset:offset+frameBytes], packet)
		if err != nil {
			t.Fatalf("frame at byte %d: Go encode: %v", offset, err)
		}

		if err := binary.Write(out, binary.BigEndian, uint32(n)); err != nil {
			t.Fatalf("write payload length: %v", err)
		}
		if err := binary.Write(out, binary.BigEndian, encoder.celtEncoder.FinalRange()); err != nil {
			t.Fatalf("write final range: %v", err)
		}
		if _, err := out.Write(packet[:n]); err != nil {
			t.Fatalf("write payload: %v", err)
		}
	}
}

func decodeBitstreamWithGo(t *testing.T, bitPath, outPath string) {
	t.Helper()

	bitstream, err := os.ReadFile(bitPath)
	if err != nil {
		t.Fatalf("read Go bitstream: %v", err)
	}

	out, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create Go decode output: %v", err)
	}
	defer out.Close()

	decoder, err := NewDecoderWithOutput(encoderConformanceRate, 2)
	if err != nil {
		t.Fatalf("create Go decoder: %v", err)
	}

	pcm := make([]byte, encoderConformanceFrameSize*4)
	for offset, frame := 0, 0; offset < len(bitstream); frame++ {
		if offset+8 > len(bitstream) {
			t.Fatalf("frame %d: truncated bitstream header", frame)
		}
		payloadLen := int(binary.BigEndian.Uint32(bitstream[offset:]))
		wantFinalRange := binary.BigEndian.Uint32(bitstream[offset+4:])
		offset += 8

		if offset+payloadLen > len(bitstream) {
			t.Fatalf("frame %d: truncated payload", frame)
		}
		payload := bitstream[offset : offset+payloadLen]
		offset += payloadLen

		if _, _, err := decoder.Decode(payload, pcm); err != nil {
			t.Fatalf("frame %d: Go decode: %v", frame, err)
		}

		gotFinalRange, err := conformanceFinalRange(&decoder)
		if err != nil {
			t.Fatalf("frame %d: final range unavailable: %v", frame, err)
		}
		if gotFinalRange != wantFinalRange {
			t.Fatalf(
				"frame %d: encoder/decoder final range mismatch: want 0x%08x got 0x%08x",
				frame, wantFinalRange, gotFinalRange,
			)
		}

		if _, err := out.Write(pcm); err != nil {
			t.Fatalf("frame %d: write Go decode output: %v", frame, err)
		}
	}
}

func runReferenceOpusDemo(t *testing.T, opusDemo, description string, args ...string) {
	t.Helper()

	cmd := exec.Command(opusDemo, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", description, err, out)
	}
}

// logEncoderQuality compares a decoded output against the original signal,
// compensating the constant codec delay first: opus_compare does not align
// its inputs and a few ms of offset wrecks the score.
func logEncoderQuality(
	t *testing.T,
	opusCompare, label string,
	originalStereo []byte,
	decodedPCM, dir, prefix string,
) {
	t.Helper()

	decoded, err := os.ReadFile(decodedPCM)
	if err != nil {
		t.Fatalf("read decoded PCM: %v", err)
	}

	lag := estimateCodecDelay(originalStereo, decoded)

	trimmedOriginal := filepath.Join(dir, prefix+"-original-trimmed.pcm")
	trimmedDecoded := filepath.Join(dir, prefix+"-decoded-trimmed.pcm")
	trimBytes := lag * 4
	common := min(len(originalStereo), len(decoded)) - trimBytes
	if common <= 0 {
		t.Fatalf("decoded output too short to align: lag %d samples", lag)
	}
	if err := os.WriteFile(trimmedOriginal, originalStereo[:common], 0o600); err != nil {
		t.Fatalf("write trimmed original: %v", err)
	}
	if err := os.WriteFile(trimmedDecoded, decoded[trimBytes:trimBytes+common], 0o600); err != nil {
		t.Fatalf("write trimmed decoded: %v", err)
	}

	out, err := runOpusCompare(
		opusCompare, encoderConformanceRate, 2,
		trimmedOriginal, trimmedDecoded,
	)
	// The 0-100 quality metric is only printed above the conformance
	// threshold; the weighted error (lower is better) is always printed.
	quality := opusCompareQuality(out)
	weightedError := opusCompareInternalError(out)
	switch {
	case err == nil:
		fmt.Printf("%s: %s: quality %s %% (weighted error %s, delay %d samples)\n",
			t.Name(), label, quality, weightedError, lag)
	case weightedError != "":
		fmt.Printf("%s: %s: below quality threshold, weighted error %s (delay %d samples)\n",
			t.Name(), label, weightedError, lag)
	default:
		t.Fatalf("opus_compare %s: %v\n%s", label, err, out)
	}
}

func opusCompareInternalError(opusCompareOutput []byte) string {
	const marker = "weighted error is "

	output := string(opusCompareOutput)
	index := strings.Index(output, marker)
	if index < 0 {
		return ""
	}
	fields := strings.Fields(output[index+len(marker):])
	if len(fields) == 0 {
		return ""
	}

	return strings.TrimSuffix(fields[0], ")")
}

// logReferenceEncoderBaseline runs the same pipeline with the reference
// encoder so the Go score has a baseline in the same run.
func logReferenceEncoderBaseline(
	t *testing.T,
	opusDemo, opusCompare string,
	testCase encoderConformanceCase,
	originalStereo []byte,
	originalPCM, dir string,
) {
	t.Helper()

	referenceBitstream := filepath.Join(dir, "reference.bit")
	referenceDecodePCM := filepath.Join(dir, "reference-dec.pcm")

	runReferenceOpusDemo(
		t, opusDemo, "encode original with reference",
		"-e", "audio", strconv.Itoa(encoderConformanceRate), strconv.Itoa(testCase.channels),
		strconv.Itoa(testCase.bitrate), "-cbr", originalPCM, referenceBitstream,
	)
	runReferenceOpusDemo(
		t, opusDemo, "decode reference bitstream",
		"-d", strconv.Itoa(encoderConformanceRate), "2",
		referenceBitstream, referenceDecodePCM,
	)

	logEncoderQuality(
		t, opusCompare, "reference encoder vs original",
		originalStereo, referenceDecodePCM, dir, "reference",
	)
}

// estimateCodecDelay cross-correlates the original and decoded stereo PCM to
// find the constant codec delay in samples (CELT alone is 120, libopus 312).
func estimateCodecDelay(originalStereo, decoded []byte) int {
	sampleAt := func(pcm []byte, i int) float64 {
		left := float64(int16(binary.LittleEndian.Uint16(pcm[i*4:])))
		right := float64(int16(binary.LittleEndian.Uint16(pcm[i*4+2:])))

		return left + right
	}

	samples := min(len(originalStereo), len(decoded)) / 4
	window := min(samples-encoderConformanceMaxLag, 4*encoderConformanceRate/10)
	if window <= 0 {
		return 0
	}

	bestLag := 0
	bestCorrelation := math.Inf(-1)
	for lag := range encoderConformanceMaxLag {
		var correlation float64
		for i := range window {
			correlation += sampleAt(originalStereo, i) * sampleAt(decoded, i+lag)
		}
		if correlation > bestCorrelation {
			bestCorrelation = correlation
			bestLag = lag
		}
	}

	return bestLag
}
