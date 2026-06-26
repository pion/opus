#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT
#
# Shared helpers for working with the RFC 6716 reference implementation.
# Source this file; do not execute it directly.

_OPUS_RFC6716_URL="${RFC6716_URL:-https://www.rfc-editor.org/rfc/rfc6716.txt}"
# RFC 6716 Appendix A.1 publishes this SHA-1 for opus-rfc6716.tar.gz.
readonly _OPUS_RFC6716_SOURCE_SHA1="86a927223e73d2476646a1b933fcd3fffb6ecc8c"
_OPUS_RFC8251_PATCH_URL="${RFC8251_PATCH_URL:-https://www.ietf.org/proceedings/98/slides/materials-98-codec-opus-update-00.patch}"
# RFC 8251 Section 1 publishes this SHA-1 for the properly formatted patch.
readonly _OPUS_RFC8251_PATCH_SHA1="029e3aa88fc342c91e67a21e7bfbc9458661cd5f"

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "missing required command: $1" >&2
        exit 1
    fi
}

download() {
    local url="$1"
    local out="$2"
    curl --fail --location --show-error --silent "${url}" --output "${out}"
}

verify_sha1() {
    local want="$1"
    local path="$2"
    if command -v sha1sum >/dev/null 2>&1; then
        printf "%s  %s\n" "${want}" "${path}" | sha1sum -c -
    else
        printf "%s  %s\n" "${want}" "${path}" | shasum -a 1 -c -
    fi
}

base64_decode() {
    if base64 --help 2>&1 | grep -q -- "--decode"; then
        base64 --decode
    else
        base64 -D
    fi
}

# prepare_reference_source downloads the RFC 6716 C source, verifies its
# SHA-1 (RFC 6716 Appendix A.1), applies the RFC 8251 decoder patch, and
# unpacks the tree into <reference_dir>.  After this call the directory is
# ready for `make opus_demo opus_compare`.
#
# Usage: prepare_reference_source <work_dir> <reference_dir>
prepare_reference_source() {
    local work_dir="$1"
    local reference_dir="$2"

    local rfc_path="$work_dir/rfc6716.txt"
    local archive_path="$work_dir/opus-rfc6716.tar.gz"
    local patch_path="$work_dir/rfc8251.patch"

    echo "Downloading RFC 6716..."
    download "${_OPUS_RFC6716_URL}" "${rfc_path}"

    echo "Extracting reference source..."
    grep '^   ###' "${rfc_path}" | sed -e 's/...###//' | base64_decode >"${archive_path}"
    verify_sha1 "${_OPUS_RFC6716_SOURCE_SHA1}" "${archive_path}"
    mkdir -p "${reference_dir}"
    tar -xzf "${archive_path}" -C "${reference_dir}" --strip-components=1

    echo "Applying RFC 8251 decoder patch..."
    download "${_OPUS_RFC8251_PATCH_URL}" "${patch_path}"
    verify_sha1 "${_OPUS_RFC8251_PATCH_SHA1}" "${patch_path}"
    patch -d "${reference_dir}" -p1 <"${patch_path}"
}
