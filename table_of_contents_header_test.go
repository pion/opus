// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigurationMode(t *testing.T) {
	t.Parallel()

	assert.Equal(t, configurationModeSilkOnly, configuration(0).mode())
	assert.Equal(t, configurationModeHybrid, configuration(12).mode())
	assert.Equal(t, configurationModeCELTOnly, configuration(16).mode())
	assert.Equal(t, configurationMode(0), configuration(32).mode())
}

func TestConfigurationModeString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Silk-only", configurationModeSilkOnly.String())
	assert.Equal(t, "CELT-only", configurationModeCELTOnly.String())
	assert.Equal(t, "Hybrid", configurationModeHybrid.String())
	assert.Equal(t, "Invalid Configuration Mode", configurationMode(0).String())
}

func TestFrameDuration(t *testing.T) {
	t.Parallel()

	assert.Equal(t, frameDuration2500us, configuration(16).frameDuration())
	assert.Equal(t, frameDuration5ms, configuration(17).frameDuration())
	assert.Equal(t, frameDuration10ms, configuration(0).frameDuration())
	assert.Equal(t, frameDuration20ms, configuration(1).frameDuration())
	assert.Equal(t, frameDuration40ms, configuration(10).frameDuration())
	assert.Equal(t, frameDuration60ms, configuration(11).frameDuration())
	assert.Equal(t, frameDuration(0), configuration(32).frameDuration())
}

func TestFrameDurationString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2.5ms", frameDuration2500us.String())
	assert.Equal(t, "5ms", frameDuration5ms.String())
	assert.Equal(t, "10ms", frameDuration10ms.String())
	assert.Equal(t, "20ms", frameDuration20ms.String())
	assert.Equal(t, "40ms", frameDuration40ms.String())
	assert.Equal(t, "60ms", frameDuration60ms.String())
	assert.Equal(t, "Invalid Frame Duration", frameDuration(0).String())
}

func TestFrameDurationNanoseconds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2500000, frameDuration2500us.nanoseconds())
	assert.Equal(t, 5000000, frameDuration5ms.nanoseconds())
	assert.Equal(t, 10000000, frameDuration10ms.nanoseconds())
	assert.Equal(t, 20000000, frameDuration20ms.nanoseconds())
	assert.Equal(t, 40000000, frameDuration40ms.nanoseconds())
	assert.Equal(t, 60000000, frameDuration60ms.nanoseconds())
	assert.Equal(t, 0, frameDuration(0).nanoseconds())
}

func TestBandwidth(t *testing.T) {
	t.Parallel()

	assert.Equal(t, BandwidthNarrowband, configuration(0).bandwidth())
	assert.Equal(t, BandwidthMediumband, configuration(4).bandwidth())
	assert.Equal(t, BandwidthWideband, configuration(8).bandwidth())
	assert.Equal(t, BandwidthSuperwideband, configuration(12).bandwidth())
	assert.Equal(t, BandwidthFullband, configuration(14).bandwidth())
	assert.Equal(t, BandwidthNarrowband, configuration(16).bandwidth())
	assert.Equal(t, BandwidthWideband, configuration(20).bandwidth())
	assert.Equal(t, BandwidthSuperwideband, configuration(24).bandwidth())
	assert.Equal(t, BandwidthFullband, configuration(28).bandwidth())
	assert.Equal(t, Bandwidth(255), configuration(32).bandwidth())
}

func TestBandwidthString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Narrowband", BandwidthNarrowband.String())
	assert.Equal(t, "Mediumband", BandwidthMediumband.String())
	assert.Equal(t, "Wideband", BandwidthWideband.String())
	assert.Equal(t, "Superwideband", BandwidthSuperwideband.String())
	assert.Equal(t, "Fullband", BandwidthFullband.String())
	assert.Equal(t, "Auto", BandwidthAuto.String())
	assert.Equal(t, "Invalid Bandwidth", Bandwidth(255).String())
}

func TestBandwidthSampleRate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 8000, BandwidthNarrowband.SampleRate())
	assert.Equal(t, 12000, BandwidthMediumband.SampleRate())
	assert.Equal(t, 16000, BandwidthWideband.SampleRate())
	assert.Equal(t, 24000, BandwidthSuperwideband.SampleRate())
	assert.Equal(t, 48000, BandwidthFullband.SampleRate())
	assert.Equal(t, 0, Bandwidth(0).SampleRate())
}

func TestParseFrameCountByte(t *testing.T) {
	t.Parallel()

	isVBR, hasPadding, frameCount := parseFrameCountByte(0b11000101)

	assert.True(t, isVBR)
	assert.True(t, hasPadding)
	assert.Equal(t, byte(5), frameCount)
}
