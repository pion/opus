#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT
set -euo pipefail
# Arguments: prepared floating-point static build of the README's exact pin,
# then the RFC 8251 testvector02.dec file used as an additional PCM source.
src=$(realpath "${1:?provide the pinned libopus build directory}")
expected_pin=22244de5a79bd1d6d623c32e72bf1954b56235be
actual_pin=$(git -C "$src" rev-parse HEAD)
if [[ "$actual_pin" != "$expected_pin" ]] || ! git -C "$src" diff --quiet HEAD --; then
    echo "reference checkout must be clean at $expected_pin (got $actual_pin)" >&2
    exit 1
fi
rfc_pcm=$(realpath "${2:?provide RFC 8251 testvector02.dec}")
fixtures=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
printf '%s  %s\n' 48ac1ff1995250a756e1e17bd32acefa8cd2b820 "$rfc_pcm" | sha1sum -c -
export PLC_RFC8251_PCM="$rfc_pcm"
cd "$src"
if grep -Eq '^#define (FIXED_POINT|ENABLE_DEEP_PLC|ENABLE_DRED|ENABLE_OSCE|ENABLE_QEXT) ' config.h; then
    echo 'unsupported reference build configuration' >&2
    exit 1
fi
for generator in generate corpus; do
    cc -O2 -Iinclude "$fixtures/$generator.c" .libs/libopus.a -lm -o "$work/$generator-plain"
    cc -O2 -DPLC_TRACE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
        "$fixtures/$generator.c" .libs/libopus.a -lm -o "$work/$generator-trace"
    "$work/$generator-plain" > "$work/$generator-plain.json"
    "$work/$generator-trace" > "$work/$generator-trace.json" 2> "$work/$generator-state.jsonl"
    cmp "$work/$generator-plain.json" "$work/$generator-trace.json"
done
cmp "$work/generate-plain.json" "$fixtures/libopus.json"
cc -O2 -DPLC_FIRST_FLOAT -Iinclude "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/first-float"
"$work/first-float" > "$work/first-float-corpus.json" 2> "$work/first-float.json"
cmp "$work/corpus-plain.json" "$work/first-float-corpus.json"
cp "$work/first-float.json" "$fixtures/first-float.json"
cc -O2 -DPLC_NO_LOSS_FLOAT -Iinclude "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/no-loss-float"
"$work/no-loss-float" > "$work/no-loss-float-corpus.json" 2> "$work/no-loss-float.json"
cmp "$work/corpus-plain.json" "$work/no-loss-float-corpus.json"
cp "$work/no-loss-float.json" "$fixtures/no-loss-float.json"
cc -O2 -DPLC_FIRST_SPECTRUM -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/first-spectrum"
"$work/first-spectrum" > "$work/first-spectrum-corpus.json" 2> "$work/first-spectrum.json"
cmp "$work/corpus-plain.json" "$work/first-spectrum-corpus.json"
cp "$work/first-spectrum.json" "$fixtures/first-spectrum.json"
cc -O2 -DPLC_DOWNMIX_SPECTRUM -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/downmix-spectrum"
"$work/downmix-spectrum" > "$work/downmix-spectrum-corpus.json" 2> "$work/downmix-spectrum.json"
cmp "$work/corpus-plain.json" "$work/downmix-spectrum-corpus.json"
cp "$work/downmix-spectrum.json" "$fixtures/downmix-spectrum.json"
cc -O2 -DPLC_RANDOM_STEREO_SPECTRUM -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/random-stereo-spectrum"
"$work/random-stereo-spectrum" > "$work/random-stereo-spectrum-corpus.json" 2> "$work/random-stereo-spectrum.json"
cmp "$work/corpus-plain.json" "$work/random-stereo-spectrum-corpus.json"
cp "$work/random-stereo-spectrum.json" "$fixtures/random-stereo-spectrum.json"
cc -O2 -DPLC_CELT_NOISE_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/celt-noise-stage"
"$work/celt-noise-stage" > "$work/celt-noise-stage-corpus.json" 2> "$work/celt-noise-stage.json"
cmp "$work/corpus-plain.json" "$work/celt-noise-stage-corpus.json"
cp "$work/celt-noise-stage.json" "$fixtures/celt-noise-stage.json"
cc -O2 -DPLC_SILK_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -Wl,--wrap=silk_resampler -Wl,--wrap=silk_decode_core \
    -o "$work/silk-stage"
"$work/silk-stage" > "$work/silk-stage-corpus.json" 2> "$work/silk-stage.json"
cmp "$work/corpus-plain.json" "$work/silk-stage-corpus.json"
cp "$work/silk-stage.json" "$fixtures/silk-stage.json"
cc -O2 -DPLC_SILK_RECOVERY_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -Wl,--wrap=silk_resampler -Wl,--wrap=silk_decode_core \
    -o "$work/silk-recovery-stage"
"$work/silk-recovery-stage" > "$work/silk-recovery-stage-corpus.json" 2> "$work/silk-recovery-stage.json"
cmp "$work/corpus-plain.json" "$work/silk-recovery-stage-corpus.json"
cp "$work/silk-recovery-stage.json" "$fixtures/silk-recovery-stage.json"
cc -O2 -DPLC_SILK_STEREO_RECOVERY_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -Wl,--wrap=silk_resampler -Wl,--wrap=silk_decode_core \
    -o "$work/silk-stereo-recovery-stage"
"$work/silk-stereo-recovery-stage" > "$work/silk-stereo-recovery-stage-corpus.json" 2> "$work/silk-stereo-recovery-stage.json"
cmp "$work/corpus-plain.json" "$work/silk-stereo-recovery-stage-corpus.json"
cp "$work/silk-stereo-recovery-stage.json" "$fixtures/silk-stereo-recovery-stage.json"
cc -O2 -DPLC_CELT_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/celt-stage"
"$work/celt-stage" > "$work/celt-stage-corpus.json" 2> "$work/celt-stage.json"
cmp "$work/corpus-plain.json" "$work/celt-stage-corpus.json"
cp "$work/celt-stage.json" "$fixtures/celt-stage.json"
cc -O2 -DPLC_CELT_MIXED_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -o "$work/celt-mixed-stage"
"$work/celt-mixed-stage" > "$work/celt-mixed-stage-corpus.json" 2> "$work/celt-mixed-stage.json"
cmp "$work/corpus-plain.json" "$work/celt-mixed-stage-corpus.json"
cp "$work/celt-mixed-stage.json" "$fixtures/celt-mixed-stage.json"
cc -O2 -DPLC_CELT_PLC_MATH -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -Wl,--wrap=celt_fir_c -Wl,--wrap=celt_iir \
    -o "$work/celt-plc-math"
"$work/celt-plc-math" > "$work/celt-plc-math-corpus.json" 2> "$work/celt-plc-math.json"
cmp "$work/corpus-plain.json" "$work/celt-plc-math-corpus.json"
cp "$work/celt-plc-math.json" "$fixtures/celt-plc-math.json"
cc -O2 -DPLC_SILK_PLC_STATE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float \
    "$fixtures/corpus.c" .libs/libopus.a -lm -Wl,--wrap=silk_decode_frame -o "$work/silk-plc-state"
"$work/silk-plc-state" > "$work/silk-plc-state-corpus.json" 2> "$work/silk-plc-state.json"
cmp "$work/corpus-plain.json" "$work/silk-plc-state-corpus.json"
cp "$work/silk-plc-state.json" "$fixtures/silk-plc-state.json"
cc -O2 -DSILK_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float -I"$fixtures" \
    "$fixtures/silk-direct.c" .libs/libopus.a -lm -Wl,--wrap=silk_resampler -Wl,--wrap=silk_decode_core \
    -o "$work/silk-direct"
"$work/silk-direct" > "$fixtures/silk-direct.json" 2>/dev/null
cc -O2 -DSILK_PLC -DSILK_STAGE -DHAVE_CONFIG_H -I. -Iinclude -Icelt -Isilk -Isilk/float -I"$fixtures" \
    "$fixtures/silk-direct.c" .libs/libopus.a -lm -Wl,--wrap=silk_resampler -Wl,--wrap=silk_decode_core \
    -o "$work/silk-direct-plc"
"$work/silk-direct-plc" > "$fixtures/silk-direct-plc.json" 2>/dev/null
cc -O2 -DPLC_EXP2 -DHAVE_CONFIG_H -I. -Iinclude -Icelt "$fixtures/primitives.c" .libs/libopus.a -lm -o "$work/exp2"
"$work/exp2" > "$fixtures/exp2.json"
cc -O2 -DPLC_ANTI_COLLAPSE -DHAVE_CONFIG_H -I. -Iinclude -Icelt "$fixtures/primitives.c" .libs/libopus.a -lm \
    -o "$work/anti-collapse"
"$work/anti-collapse" > "$fixtures/anti-collapse.json"
cc -O2 -DPLC_MDCT_TABLES -DHAVE_CONFIG_H -I. -Iinclude -Icelt "$fixtures/primitives.c" .libs/libopus.a -lm -o "$work/mdct-tables"
"$work/mdct-tables" > "$fixtures/mdct-tables.json"
node "$fixtures/generate-mdct-tables.mjs" "$fixtures/mdct-tables.json" \
    "$fixtures/../../internal/celt/decoder_mdct_tables.go"
cc -O2 -DHAVE_CONFIG_H -I. -Iinclude -Icelt "$fixtures/primitives.c" .libs/libopus.a -lm -o "$work/primitives"
"$work/primitives" > "$work/primitives.json"
gzip -n -c "$work/primitives.json" > "$fixtures/primitives.json.gz"
gzip -n -c "$work/corpus-plain.json" > "$fixtures/corpus.json.gz"
gzip -n -c "$work/corpus-state.jsonl" > "$fixtures/corpus-state.jsonl.gz"
sha256sum "$work/"*-plain.json "$fixtures/corpus.json.gz" "$fixtures/corpus-state.jsonl.gz"
echo "Observation-only comparison files retained in $work"
