1#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT

# Builds the real-audio corpus that TestEncoderQualityRealCorpus measures
# against. The clips are live recordings from the Internet Archive's Live Music
# Archive, whose taping policies allow free redistribution; only their URLs and
# checksums live in this repo, never the audio.
#
# Every clip was checked to be free of prior codec damage two ways: its spectrum
# descends to Nyquist with no plateau at the dither floor, and its round-trip
# score is flat across framing offsets. Material that has already been through a
# codec fails one or both, and scores about a dB high when re-encoded on the
# grid it already carries, which makes it useless as a quality reference.
#
#   ./.github/scripts/fetch-quality-corpus.sh [output-dir]

set -euo pipefail

OUT_DIR="${1:-${OPUS_QUALITY_CORPUS:-corpus}}"

# item|file|sha1|start seconds|duration seconds
MANIFEST="
oar2006-01-14.mix.flac16|oar2006-01-14d2t09.mix.flac|54e85d4f7a90afd5d9e1ccf8a4ee4d39b2c60b62|60|30
jj2008-06-14.mk4|jj2008-06-14d2t02.flac|90682e3425e1fd516e1c44190457f8745e60db23|60|30
tlg2006-10-21.mk4v.flac16|tlg2006-10-21d2t04.flac|5c7a4872e056d294a9303674ff7ae6dd272a0734|60|30
gd1977-05-08.shure57.stevenson.29303.flac16|gd1977-05-08d03t05.flac|2182ce0c10de3e6c5760982944f4ca0cf1c5eb51|60|30
pattersonh2006-01-07.184.flac16|pattersonh2006-01-07d2t08.flac|5830b37cc92ac915cae58d32f5515fc602623885|60|30
moe2004-10-29.at4051a.flac|moe2004-10-29set2t09.flac|fddde10084bc760b0f0ae5d8ef726d8058fb0a69|60|30
SOMS2004-11-13.flac|SOMS2004-11-13D2T06.flac|2b7a44350021077690b2533f8925bc737d220baf|60|30
mss2003-12-31.flac16f|mss2003-12-31d3.flac16/mss2003-12-31d3t02.flac|56ea71e19e61e160e5bfdd1126182ac18c5d576c|60|30
maroon5-2004-10-21.flac16|maroon5-2004-10-21t05.flac|0cf9e7d771d05f01988e4ae66117df8e81ef8252|60|30
johnmayer2008-08-02.DPA4023.flac16|John_Mayer_2008-08-02_t05.flac|b80d8191f6fba32d38b81a9e8b871fd070665d4c|60|30
ryanadams2006-10-17.sbd.flac16|ryanadams2006-10-17.sbd.d2t05.flac|2ef12d80888f6a9188e913f905107f4c18a839ce|60|30
esmith2003-01-31.flac16|es2003-01-31d1t12.flac|e556a82cedfc3c3d3e3ca00c3b3730ed8bff53e9|60|30
"

for tool in curl ffmpeg sha1sum; do
    command -v "${tool}" >/dev/null 2>&1 || {
        echo "missing ${tool}" >&2
        exit 1
    }
done

mkdir -p "${OUT_DIR}"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

while IFS='|' read -r item file sha start dur; do
    if [ -z "${item}" ]; then
        continue
    fi
    name="${item%%.*}"
    target="${OUT_DIR}/${name}.pcm"
    if [ -s "${target}" ]; then
        echo "have ${name}"

        continue
    fi

    src="${WORK_DIR}/src.flac"
    url="https://archive.org/download/${item}/$(echo "${file}" | sed 's/ /%20/g')"
    curl -sL --retry 3 -m 600 -o "${src}" "${url}"

    got="$(sha1sum "${src}" | cut -d' ' -f1)"
    if [ "${got}" != "${sha}" ]; then
        echo "checksum mismatch for ${name}: ${got} != ${sha}" >&2
        exit 1
    fi

    # soxr is pinned because these masters are 44.1 kHz and a different
    # resampler would move the baseline for reasons that are not the encoder.
    ffmpeg -v error -ss "${start}" -t "${dur}" -i "${src}" \
        -af aresample=resampler=soxr:precision=28:out_sample_rate=48000 \
        -ac 2 -f s16le "${target}" -y
    rm -f "${src}"
    echo "built ${name}"
done <<<"${MANIFEST}"

echo
echo "corpus ready in ${OUT_DIR}"
echo "  export OPUS_QUALITY_CORPUS=$(cd "${OUT_DIR}" && pwd)"
