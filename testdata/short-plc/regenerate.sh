#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT
set -euo pipefail
# Argument: prepared, floating-point static build of the README's exact pin.
src=$(realpath "${1:?provide the pinned libopus build directory}")
fixtures=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
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
cc -O2 -DHAVE_CONFIG_H -I. -Iinclude -Icelt "$fixtures/primitives.c" .libs/libopus.a -lm -o "$work/primitives"
"$work/primitives" > "$work/primitives.json"
gzip -n -c "$work/primitives.json" > "$fixtures/primitives.json.gz"
gzip -n -c "$work/corpus-plain.json" > "$fixtures/corpus.json.gz"
gzip -n -c "$work/corpus-state.jsonl" > "$fixtures/corpus-state.jsonl.gz"
sha256sum "$work/"*-plain.json "$fixtures/corpus.json.gz" "$fixtures/corpus-state.jsonl.gz"
echo "Observation-only comparison files retained in $work"
