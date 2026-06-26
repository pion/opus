#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=lib-opus-reference.sh
source "${SCRIPT_DIR}/lib-opus-reference.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

result_file="${OPUS_QUALITY_RESULT:-${WORK_DIR}/encoder-quality.md}"
mkdir -p "$(dirname "${result_file}")"

# --- Tier 1: SNR regression (pure Go, no external tools) ---
echo "=== Tier 1: SNR regression ==="
cd "${REPO_DIR}"
tier1_log="${WORK_DIR}/tier1.log"
set +e
go test -v -run TestEncoderQuality -count=1 . 2>&1 | tee "${tier1_log}"
tier1_exit=${PIPESTATUS[0]}
set -e
echo ""

# --- Tier 2: opus_compare vs reference ---
echo "=== Tier 2: opus_compare ==="
require_command base64
require_command curl
require_command make
require_command patch
require_command sed
require_command tar

reference_dir="${WORK_DIR}/reference"
prepare_reference_source "${WORK_DIR}" "${reference_dir}"

tier2_log="${WORK_DIR}/tier2.log"
set +e
OPUS_RFC6716_REFERENCE="${reference_dir}" \
    go test -v -tags conformance -run TestEncoderQualityVsReference -count=1 . 2>&1 | tee "${tier2_log}"
tier2_exit=${PIPESTATUS[0]}
set -e
echo ""

# --- Report ---
{
    echo "<!-- opus-encoder-quality -->"
    echo "## Encoder Quality Report"
    echo ""
    echo "### Tier 1: SNR (pion encode → pion decode)"
    echo ""
    echo '```'
    grep -E "signal=|baseline=|--- PASS|--- FAIL" "${tier1_log}" 2>/dev/null \
        | sed 's/^    [^:]*:[0-9]*: //' \
        || echo "(no output)"
    echo '```'
    echo ""
    echo "### Tier 2: opus_compare (vs RFC 6716 reference)"
    echo ""
    echo '```'
    grep -E "pion quality=|libopus quality=|--- PASS|--- FAIL|--- SKIP" "${tier2_log}" 2>/dev/null \
        | sed 's/^    [^:]*:[0-9]*: //' \
        || echo "(no output)"
    echo '```'
    echo ""
    echo "---"
    echo "*Baseline: \`testdata/encoder-quality-baseline.json\`*"
} >"${result_file}"

if [ "${tier1_exit}" -ne 0 ] || [ "${tier2_exit}" -ne 0 ]; then
    echo "FAILED: tier1=${tier1_exit} tier2=${tier2_exit}"
    exit 1
fi

echo "All quality checks passed."
