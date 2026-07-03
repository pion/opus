// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	qualityTestRate        = 48000
	qualityTestFrameSize   = 960
	qualityTestFrameCount  = 100
	qualityTestBitrate     = 96000
	qualityTestChannels    = 1
	regressionThresholdDB  = 1.5
	qualityTestMaxLag      = 2048
	qualityBaselineVersion = 1

	// harmonicsPeakAmplitude is the sum of the amplitudes of the 5-harmonic
	// series (1 + 1/2 + 1/3 + 1/4 + 1/5), used to normalize generateHarmonics
	// so its peak stays near 0.5.
	harmonicsPeakAmplitude = 1.0 + 1.0/2.0 + 1.0/3.0 + 1.0/4.0 + 1.0/5.0
)

type qualityBaselineSignal struct {
	Tier1SNRDB  float64 `json:"tier1SnrDb"`
	Tier2WSNRDB float64 `json:"tier2WsnrDb,omitempty"`
}

type qualityBaseline struct {
	Version int                              `json:"version"`
	Bitrate int                              `json:"bitrate"`
	Signals map[string]qualityBaselineSignal `json:"signals"`
}

type qualitySignal struct {
	name     string
	channels int
	generate func(n int) []float32
}

func qualityTestSignals() []qualitySignal {
	return []qualitySignal{
		{name: "chirp", channels: qualityTestChannels, generate: generateChirp},
		{name: "harmonics", channels: qualityTestChannels, generate: generateHarmonics},
		{name: "burst", channels: qualityTestChannels, generate: generateBurst},
		{name: "shaped_noise", channels: qualityTestChannels, generate: generateShapedNoise},
		{name: "onset", channels: qualityTestChannels, generate: generateOnset},
	}
}

func generateChirp(n int) []float32 {
	samples := make([]float32, n)
	freqStart := 200.0
	freqEnd := 4000.0
	for i := range n {
		t := float64(i) / qualityTestRate
		totalT := float64(n) / qualityTestRate
		freq := freqStart + (freqEnd-freqStart)*t/totalT
		samples[i] = float32(0.8 * math.Sin(2*math.Pi*freq*t))
	}

	return samples
}

func generateHarmonics(n int) []float32 {
	samples := make([]float32, n)
	for i := range n {
		t := float64(i) / qualityTestRate
		var s float64
		for h := 1; h <= 5; h++ {
			s += math.Sin(2*math.Pi*200*float64(h)*t) / float64(h)
		}
		samples[i] = float32(0.5 * s / harmonicsPeakAmplitude)
	}

	return samples
}

func generateBurst(n int) []float32 {
	samples := make([]float32, n)
	half := n / 2
	for i := range n {
		t := float64(i) / qualityTestRate
		if i >= half {
			samples[i] = float32(0.9 * math.Sin(2*math.Pi*440*t))
		}
	}

	return samples
}

func generateShapedNoise(n int) []float32 {
	samples := make([]float32, n)
	for i := range n {
		seed := uint64(i)*6364136223846793005 + 1442695040888963407
		noise := float64(int64(seed>>33)) / float64(1<<30)
		if i > 0 {
			noise = (noise + float64(samples[i-1])) / 2.0
		}
		samples[i] = float32(0.6 * noise)
	}

	return samples
}

func generateOnset(n int) []float32 {
	samples := make([]float32, n)
	half := n / 2
	fadeLen := qualityTestRate / 50
	for i := range n {
		t := float64(i) / qualityTestRate
		if i >= half {
			env := 1.0
			if rel := i - half; rel < fadeLen {
				env = float64(rel) / float64(fadeLen)
			}
			samples[i] = float32(0.8 * env * math.Sin(2*math.Pi*440*t))
		}
	}

	return samples
}

func TestEncoderQuality(t *testing.T) {
	baseline := loadQualityBaseline(t)
	signals := qualityTestSignals()

	type signalResult struct {
		snr     float64
		base    float64
		delta   float64
		hasBase bool
	}

	results := make([]signalResult, len(signals))
	var mu sync.Mutex

	for i, sig := range signals {
		t.Run(sig.name, func(t *testing.T) {
			t.Parallel()

			n := qualityTestFrameSize * qualityTestFrameCount
			original := sig.generate(n)
			decoded := roundTripGo(t, original, sig.channels)

			snr := computeSNR(original, decoded)
			t.Logf("signal=%s SNR=%.1f dB", sig.name, snr)

			res := signalResult{snr: snr}
			if sigData, ok := baseline.Signals[sig.name]; ok {
				res.base = sigData.Tier1SNRDB
				res.delta = sigData.Tier1SNRDB - snr
				res.hasBase = true
				t.Logf("baseline=%.1f dB delta=%.1f dB threshold=%.1f dB",
					sigData.Tier1SNRDB, res.delta, regressionThresholdDB)
				assert.LessOrEqualf(t, res.delta, regressionThresholdDB,
					"quality regression: signal=%s SNR=%.1f dB baseline=%.1f dB",
					sig.name, snr, sigData.Tier1SNRDB)
			} else {
				t.Logf("no baseline for signal %s, pass (first run)", sig.name)
			}

			mu.Lock()
			results[i] = res
			mu.Unlock()
		})
	}

	t.Cleanup(func() {
		mdPath := os.Getenv("OPUS_QUALITY_MARKDOWN")
		if mdPath == "" {
			return
		}

		var buf bytes.Buffer
		_, _ = fmt.Fprintln(&buf, "### Tier 1 — SNR regression (96 kbps, pion encode → pion decode)")
		_, _ = fmt.Fprintln(&buf)
		_, _ = fmt.Fprintf(&buf, "Delta = baseline − current SNR; positive = regression. Fail threshold: %.1f dB.\n",
			regressionThresholdDB)
		_, _ = fmt.Fprintln(&buf)
		_, _ = fmt.Fprintln(&buf, "| Signal | SNR (dB) | Baseline (dB) | Delta | Status |")
		_, _ = fmt.Fprintln(&buf, "|---|---:|---:|---:|---|")
		for i, sig := range signals {
			res := results[i]
			status := "OK"
			if res.hasBase && res.delta > regressionThresholdDB {
				status = "FAIL"
			}
			_, _ = fmt.Fprintf(&buf, "| %s | %.1f | %.1f | %+.1f | %s |\n",
				sig.name, res.snr, res.base, res.delta, status)
		}

		if err := os.WriteFile(mdPath, buf.Bytes(), 0o600); err != nil { //nolint:gosec // G306: 0o600 is intentional.
			t.Logf("write quality markdown: %v", err)
		}
	})
}

func roundTripGo(t *testing.T, pcm []float32, channels int) []float32 {
	t.Helper()

	encoder, err := NewEncoder(WithChannels(channels), WithBitrate(qualityTestBitrate))
	require.NoError(t, err, "create encoder")

	decoder, err := NewDecoderWithOutput(qualityTestRate, channels)
	require.NoError(t, err, "create decoder")

	frameSamples := qualityTestFrameSize * channels
	packet := make([]byte, maxOpusFrameSize+1)
	var decoded []float32
	for offset := 0; offset+frameSamples <= len(pcm); offset += frameSamples {
		frame := make([]float32, frameSamples)
		copy(frame, pcm[offset:offset+frameSamples])

		n, err := encoder.EncodeFloat32(frame, packet)
		require.NoErrorf(t, err, "encode frame at sample %d", offset)

		out := make([]float32, frameSamples)
		_, _, err = decoder.DecodeFloat32(packet[:n], out)
		require.NoErrorf(t, err, "decode frame at sample %d", offset)

		decoded = append(decoded, out...)
	}

	return decoded
}

func computeSNR(original, decoded []float32) float64 {
	lag := estimateCodecDelayFloat32(original, decoded)
	if lag > 0 && lag < len(decoded) {
		decoded = decoded[lag:]
	}

	n := min(len(original), len(decoded))
	if n == 0 {
		return 0
	}

	var signalPower, noisePower float64
	for i := range n {
		signalPower += float64(original[i]) * float64(original[i])
		diff := float64(original[i]) - float64(decoded[i])
		noisePower += diff * diff
	}

	if noisePower == 0 {
		return 100.0
	}

	return 10 * math.Log10(signalPower/noisePower)
}

func estimateCodecDelayFloat32(original, decoded []float32) int {
	samples := min(len(original), len(decoded))
	window := min(samples-qualityTestMaxLag, 4*qualityTestRate/10)
	if window <= 0 {
		return 0
	}

	bestLag := 0
	bestCorrelation := math.Inf(-1)
	for lag := range qualityTestMaxLag {
		var correlation float64
		for i := range window {
			correlation += float64(original[i]) * float64(decoded[i+lag])
		}

		if correlation > bestCorrelation {
			bestCorrelation = correlation
			bestLag = lag
		}
	}

	return bestLag
}

func loadQualityBaseline(t *testing.T) qualityBaseline {
	t.Helper()

	path := filepath.Join("testdata", "encoder-quality-baseline.json")
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed testdata path.
	if err != nil {
		t.Logf("no golden file at %s, baseline will be empty (first run)", path)

		return qualityBaseline{
			Version: qualityBaselineVersion,
			Bitrate: qualityTestBitrate,
			Signals: make(map[string]qualityBaselineSignal),
		}
	}

	var baseline qualityBaseline
	require.NoError(t, json.Unmarshal(data, &baseline), "parse golden file")

	require.Equalf(t, qualityBaselineVersion, baseline.Version,
		"baseline schema version mismatch: got %d, want %d", baseline.Version, qualityBaselineVersion)
	require.Equalf(t, qualityTestBitrate, baseline.Bitrate,
		"baseline was generated at a different bitrate: got %d, want %d", baseline.Bitrate, qualityTestBitrate)

	if baseline.Signals == nil {
		baseline.Signals = make(map[string]qualityBaselineSignal)
	}

	t.Logf("loaded baseline: version=%d bitrate=%d signals=%d", baseline.Version, baseline.Bitrate, len(baseline.Signals))

	return baseline
}
