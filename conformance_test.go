// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build conformance

package opus

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type conformanceKey struct {
	rate     int
	channels int
	vector   string
}

type conformanceResult struct {
	passed  bool
	quality string
}

const (
	envRFC6716Reference      = "OPUS_RFC6716_REFERENCE"
	envRFC6716Testvectors    = "OPUS_RFC6716_TESTVECTORS"
	envConformanceMarkdown   = "OPUS_CONFORMANCE_MARKDOWN"
	maxConformancePacketSize = maxOpusFrameSize * 48
)

func TestRFC6716Conformance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("RFC 6716 conformance uses the POSIX-oriented reference Makefile")
	}

	rates := []int{8000, 12000, 16000, 24000, 48000}
	channelCounts := []int{1, 2}
	vectors := []string{
		"01", "02", "03", "04", "05", "06",
		"07", "08", "09", "10", "11", "12",
	}

	refDir, vectorRoot := conformanceDataPaths(t)

	opusCompare := buildRFC6716ReferenceTools(t, refDir)
	results := make(map[conformanceKey]conformanceResult)
	var resultsMu sync.Mutex

	t.Run("vectors", func(t *testing.T) {
		for _, rate := range rates {
			for _, channels := range channelCounts {
				for _, vector := range vectors {
					key := conformanceKey{
						rate:     rate,
						channels: channels,
						vector:   vector,
					}
					t.Run(
						fmt.Sprintf("rate_%d/channels_%d/testvector%s", rate, channels, vector),
						func(t *testing.T) {
							t.Parallel()

							quality := ""
							defer func() {
								resultsMu.Lock()
								results[key] = conformanceResult{passed: !t.Failed(), quality: quality}
								resultsMu.Unlock()
							}()

							bitstream := conformanceBitstreamPath(t, vectorRoot, vector)
							referencePCMs := conformanceReferencePCMs(vectorRoot, vector)
							goPCM := filepath.Join(t.TempDir(), "go.pcm")

							decodeRFC6716Vector(t, rate, channels, bitstream, goPCM)
							quality = compareRFC6716Output(
								t,
								opusCompare,
								rate,
								channels,
								referencePCMs,
								goPCM,
							)
						},
					)
				}
			}
		}
	})

	printConformanceMatrix(results, rates, channelCounts, vectors)
	writeConformanceMarkdown(t, os.Getenv(envConformanceMarkdown), results, rates, channelCounts, vectors)
}

func conformanceDataPaths(t *testing.T) (refDir, vectorRoot string) {
	t.Helper()

	refDir = os.Getenv(envRFC6716Reference)
	vectorRoot = os.Getenv(envRFC6716Testvectors)
	if refDir == "" || vectorRoot == "" {
		t.Skipf("%s and %s are required to run RFC 6716 conformance", envRFC6716Reference, envRFC6716Testvectors)
	}

	return refDir, vectorRoot
}

func compareRFC6716Output(
	t *testing.T,
	opusCompare string,
	rate, channels int,
	referencePCMs []string,
	goPCM string,
) string {
	t.Helper()

	checkedReference := false
	var failures []string
	for _, referencePCM := range referencePCMs {
		if _, err := os.Stat(referencePCM); err != nil {
			continue
		}
		checkedReference = true
		out, err := runOpusCompare(opusCompare, rate, channels, referencePCM, goPCM)
		if err == nil {
			quality := opusCompareQuality(out)
			printOpusCompareQuality(t, quality)

			return quality
		}
		failures = append(failures, fmt.Sprintf("%s: %v\n%s", referencePCM, err, out))
	}
	if !checkedReference {
		t.Fatalf("no reference PCM found among %v", referencePCMs)
	}

	t.Fatalf("opus_compare failed for all references:\n%s", strings.Join(failures, "\n"))

	return ""
}

func conformanceBitstreamPath(t *testing.T, vectorRoot, vector string) string {
	t.Helper()

	// RFC 8251 Section 11 keeps the decoder input bitstreams unchanged, so the
	// newer archive is the preferred source and the RFC 6716 archive is an
	// equivalent fallback when only the legacy bundle is available.
	for _, vectorSet := range []string{"rfc8251", "rfc6716"} {
		path := filepath.Join(vectorRoot, vectorSet, "testvector"+vector+".bit")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	t.Fatalf("missing testvector%s.bit in RFC 8251 or RFC 6716 vectors", vector)

	return ""
}

func conformanceReferencePCMs(vectorRoot, vector string) []string {
	// RFC 8251 Section 11 permits either the original RFC 6716 output set or
	// one of the updated output sets for the same unchanged input bitstreams.
	return []string{
		filepath.Join(vectorRoot, "rfc8251", "testvector"+vector+".dec"),
		filepath.Join(vectorRoot, "rfc8251", "testvector"+vector+"m.dec"),
		filepath.Join(vectorRoot, "rfc6716", "testvector"+vector+".dec"),
	}
}

func buildRFC6716ReferenceTools(t *testing.T, refDir string) (opusCompare string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(refDir, "Makefile")); err != nil {
		t.Fatalf("missing RFC 6716 reference Makefile: %v", err)
	}

	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("make is required to build the RFC 6716 reference tools: %v", err)
	}
	if _, err := exec.LookPath("cp"); err != nil {
		t.Skipf("cp is required to prepare the RFC 6716 reference tools: %v", err)
	}

	buildDir := filepath.Join(t.TempDir(), "rfc6716")
	cmd := exec.Command("cp", "-R", refDir, buildDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("copy RFC 6716 reference tree: %v\n%s", err, out)
	}

	cmd = exec.Command("make", "opus_demo", "opus_compare")
	cmd.Dir = buildDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build RFC 6716 reference tools: %v\n%s", err, out)
	}

	opusDemo := filepath.Join(buildDir, "opus_demo")
	opusCompare = filepath.Join(buildDir, "opus_compare")
	for _, path := range []string{opusDemo, opusCompare} {
		if info, err := os.Stat(path); err != nil {
			t.Fatalf("missing built RFC 6716 tool %s: %v", filepath.Base(path), err)
		} else if info.IsDir() {
			t.Fatalf("built RFC 6716 tool %s is a directory", filepath.Base(path))
		}
	}

	return opusCompare
}

func decodeRFC6716Vector(t *testing.T, rate, channels int, bitstream, outPath string) {
	t.Helper()

	in, err := os.Open(bitstream)
	if err != nil {
		t.Fatalf("open bitstream: %v", err)
	}
	defer in.Close()

	out, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create Go PCM output: %v", err)
	}
	defer out.Close()

	decoder, err := NewDecoderWithOutput(rate, channels)
	if err != nil {
		t.Fatalf("create Go decoder: %v", err)
	}

	pcm := make([]byte, 5760*2*2)
	for frame := 0; ; frame++ {
		var payloadLen uint32
		if err := binary.Read(in, binary.BigEndian, &payloadLen); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("frame %d: read payload length: %v", frame, err)
		}
		var wantFinalRange uint32
		if err := binary.Read(in, binary.BigEndian, &wantFinalRange); err != nil {
			t.Fatalf("frame %d: read final range: %v", frame, err)
		}

		if payloadLen > maxConformancePacketSize {
			t.Fatalf("frame %d: payload length %d exceeds %d", frame, payloadLen, maxConformancePacketSize)
		}
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(in, payload); err != nil {
			t.Fatalf("frame %d: read payload: %v", frame, err)
		}

		if _, _, err := decoder.Decode(payload, pcm); err != nil {
			t.Fatalf("frame %d: Go decode: %v", frame, err)
		}

		gotFinalRange, err := conformanceFinalRange(&decoder)
		if err != nil {
			t.Fatalf("frame %d: final range unavailable: %v", frame, err)
		}
		if wantFinalRange != 0 && gotFinalRange != wantFinalRange {
			t.Fatalf(
				"frame %d: final range mismatch: want 0x%08x got 0x%08x",
				frame,
				wantFinalRange,
				gotFinalRange,
			)
		}

		samplesPerChannel, err := conformancePacketSamplesPerChannel(payload, rate)
		if err != nil {
			t.Fatalf("frame %d: packet duration: %v", frame, err)
		}

		outBytes := samplesPerChannel * channels * 2
		if outBytes > len(pcm) {
			t.Fatalf("frame %d: PCM output length %d exceeds buffer length %d", frame, outBytes, len(pcm))
		}
		if _, err := out.Write(pcm[:outBytes]); err != nil {
			t.Fatalf("frame %d: write Go PCM output: %v", frame, err)
		}
	}
}

func runOpusCompare(opusCompare string, rate, channels int, referencePCM, goPCM string) ([]byte, error) {
	args := []string{"-r", strconv.Itoa(rate), referencePCM, goPCM}
	if channels == 2 {
		args = append([]string{"-s"}, args...)
	}

	cmd := exec.Command(opusCompare, args...)

	return cmd.CombinedOutput()
}

func opusCompareQuality(opusCompareOutput []byte) string {
	const prefix = "Opus quality metric:"

	for _, line := range strings.Split(string(opusCompareOutput), "\n") {
		if strings.HasPrefix(line, prefix) {
			fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			if len(fields) == 0 {
				return ""
			}

			return fields[0]
		}
	}

	return ""
}

func printOpusCompareQuality(t *testing.T, quality string) {
	t.Helper()

	if quality != "" {
		fmt.Printf("%s: Opus quality metric: %s %%\n", t.Name(), quality)
	}
}

func printConformanceMatrix(
	results map[conformanceKey]conformanceResult,
	rates []int,
	channelCounts []int,
	vectors []string,
) {
	if len(results) == 0 {
		return
	}

	fmt.Println("Opus conformance matrix")
	fmt.Println("Legend: numeric cells are opus_compare quality percentages; FAIL means the vector did not pass.")
	fmt.Println("Inputs use the shared RFC 6716 / RFC 8251 bitstream corpus; accepted references follow RFC 8251 Section 11.")

	printConformanceMatrixRule(vectors)
	fmt.Printf("| %-8s | %-2s |", "rate", "ch")
	for _, vector := range vectors {
		fmt.Printf(" %-*s |", conformanceMatrixVectorCellWidth, vector)
	}
	fmt.Println()
	printConformanceMatrixRule(vectors)

	for _, rate := range rates {
		for _, channels := range channelCounts {
			fmt.Printf("| %-8d | %-2d |", rate, channels)
			for _, vector := range vectors {
				key := conformanceKey{
					rate:     rate,
					channels: channels,
					vector:   vector,
				}
				fmt.Printf(" %-*s |", conformanceMatrixVectorCellWidth, conformanceMatrixCell(results, key))
			}
			fmt.Println()
		}
	}
	printConformanceMatrixRule(vectors)
}

const conformanceMatrixVectorCellWidth = 5

func printConformanceMatrixRule(vectors []string) {
	fmt.Print("+----------+----+")
	for range vectors {
		fmt.Print(strings.Repeat("-", conformanceMatrixVectorCellWidth+2) + "+")
	}
	fmt.Println()
}

func writeConformanceMarkdown(
	t *testing.T,
	path string,
	results map[conformanceKey]conformanceResult,
	rates []int,
	channelCounts []int,
	vectors []string,
) {
	t.Helper()

	if path == "" || len(results) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("Legend: numeric cells are `opus_compare` quality percentages; `FAIL` means the vector did not pass.\n\n")
	b.WriteString("Inputs use the shared RFC 6716 / RFC 8251 bitstream corpus; accepted references follow RFC 8251 Section 11.\n\n")
	b.WriteString("| rate | ch |")
	for _, vector := range vectors {
		fmt.Fprintf(&b, " %s |", vector)
	}
	b.WriteString("\n| --- | --- |")
	for range vectors {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	for _, rate := range rates {
		for _, channels := range channelCounts {
			fmt.Fprintf(&b, "| %d | %d |", rate, channels)
			for _, vector := range vectors {
				key := conformanceKey{
					rate:     rate,
					channels: channels,
					vector:   vector,
				}
				fmt.Fprintf(&b, " %s |", conformanceMatrixCell(results, key))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write conformance markdown: %v", err)
	}
}

func conformanceMatrixCell(results map[conformanceKey]conformanceResult, key conformanceKey) string {
	result, ok := results[key]
	if !ok {
		return "SKIP"
	}
	if !result.passed {
		return "FAIL"
	}
	if result.quality != "" {
		return result.quality
	}

	return "PASS"
}

func conformanceFinalRange(d *Decoder) (uint32, error) {
	switch d.previousMode {
	case configurationModeCELTOnly:
		return d.celtDecoder.FinalRange(), nil
	case configurationModeSilkOnly:
		return d.rangeFinal, nil
	case configurationModeHybrid:
		return d.rangeFinal, nil
	default:
		return 0, fmt.Errorf("unsupported final range mode: %s", d.previousMode)
	}
}

func conformancePacketSamplesPerChannel(packet []byte, rate int) (int, error) {
	if len(packet) == 0 {
		return 0, errTooShortForTableOfContentsHeader
	}

	tocHeader := tableOfContentsHeader(packet[0])
	frames, err := parsePacketFrames(packet, tocHeader)
	if err != nil {
		return 0, err
	}

	nanoseconds := tocHeader.configuration().frameDuration().nanoseconds()

	return int(int64(len(frames)) * int64(nanoseconds) * int64(rate) / 1000000000), nil
}
