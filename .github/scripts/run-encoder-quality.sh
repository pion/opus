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

markdown_file="${WORK_DIR}/quality-tables.md"

# --- Tier 1: SNR regression (pure Go, no external tools) ---
echo "=== Tier 1: SNR regression ==="
cd "${REPO_DIR}"
tier1_log="${WORK_DIR}/tier1.log"
set +e
OPUS_QUALITY_MARKDOWN="${markdown_file}" \
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
OPUS_QUALITY_MARKDOWN="${markdown_file}" \
    OPUS_RFC6716_REFERENCE="${reference_dir}" \
    go test -v -tags conformance -run TestEncoderQualityVsReference -count=1 . 2>&1 | tee "${tier2_log}"
tier2_exit=${PIPESTATUS[0]}
set -e
echo ""

# --- Report ---
status_text="pass"
if [ "${tier1_exit}" -ne 0 ] || [ "${tier2_exit}" -ne 0 ]; then
    status_text="fail (informational)"
fi

{
    echo "<!-- opus-encoder-quality -->"
    echo "## Encoder Quality Report"
    echo
    echo "**Status:** ${status_text}"
    echo
    if [ -s "${markdown_file}" ]; then
        cat "${markdown_file}"
    else
        echo "No quality tables generated."
    fi
    echo
    echo "<details><summary>Run output</summary>"
    echo
    echo '```text'
    tail -n 200 "${tier1_log}"
    [ -s "${tier2_log}" ] && tail -n 200 "${tier2_log}"
    echo '```'
    echo "</details>"
    echo
    echo "---"
    echo "*Baseline: \`testdata/encoder-quality-baseline.json\`*"
} >"${result_file}"

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    cat "${result_file}" >>"${GITHUB_STEP_SUMMARY}"
fi

if [ "${tier1_exit}" -ne 0 ] || [ "${tier2_exit}" -ne 0 ]; then
    echo "FAILED: tier1=${tier1_exit} tier2=${tier2_exit}"
    exit 1
fi

echo "All quality checks passed."
