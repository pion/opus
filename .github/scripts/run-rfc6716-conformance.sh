#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT

set -euo pipefail

readonly RFC6716_URL="${RFC6716_URL:-https://www.rfc-editor.org/rfc/rfc6716.txt}"
# RFC 6716 Appendix A.1 publishes this SHA-1 for opus-rfc6716.tar.gz.
readonly RFC6716_SOURCE_SHA1="86a927223e73d2476646a1b933fcd3fffb6ecc8c"
readonly RFC8251_PATCH_URL="${RFC8251_PATCH_URL:-https://www.ietf.org/proceedings/98/slides/materials-98-codec-opus-update-00.patch}"
# RFC 8251 Section 1 publishes this SHA-1 for the properly formatted patch.
readonly RFC8251_PATCH_SHA1="029e3aa88fc342c91e67a21e7bfbc9458661cd5f"
readonly RFC6716_VECTORS_URL="${RFC6716_VECTORS_URL:-https://opus-codec.org/static/testvectors/opus_testvectors.tar.gz}"
readonly RFC8251_VECTORS_URL="${RFC8251_VECTORS_URL:-https://opus-codec.org/static/testvectors/opus_testvectors-rfc8251.tar.gz}"

work_dir="${OPUS_CONFORMANCE_WORKDIR:-${RUNNER_TEMP:-/tmp}/opus-rfc-conformance}"
result_file="${OPUS_CONFORMANCE_RESULT:-${work_dir}/conformance-result.md}"
log_file="${work_dir}/go-test.log"
matrix_file="${work_dir}/conformance-matrix.md"

write_early_failure_comment() {
  local status="$1"

  mkdir -p "$(dirname "${result_file}")"
  {
    echo "<!-- opus-rfc-conformance -->"
    echo "## RFC 6716 / 8251 conformation"
    echo
    echo "**Status:** fail"
    echo
    echo "The conformance action failed before the test matrix was available."
    echo
    echo "Exit status: \`${status}\`"
    if [ -f "${log_file}" ]; then
      echo
      echo "<details><summary>Run output</summary>"
      echo
      echo '```text'
      tail -n 200 "${log_file}"
      echo '```'
      echo "</details>"
    fi
  } >"${result_file}"
}

on_exit() {
  local status="$1"

  if [ "${status}" -ne 0 ] && [ ! -s "${result_file}" ]; then
    write_early_failure_comment "${status}"
  fi
}
trap 'on_exit "$?"' EXIT

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

verify_sha1_manifest() {
  local dir="$1"

  if command -v sha1sum >/dev/null 2>&1; then
    (cd "${dir}" && sha1sum -c -)
  else
    (cd "${dir}" && shasum -a 1 -c -)
  fi
}

base64_decode() {
  if base64 --help 2>&1 | grep -q -- "--decode"; then
    base64 --decode
  else
    base64 -D
  fi
}

extract_reference_source() {
  local rfc_path="$1"
  local archive_path="$2"
  local reference_dir="$3"

  grep '^   ###' "${rfc_path}" | sed -e 's/...###//' | base64_decode >"${archive_path}"
  verify_sha1 "${RFC6716_SOURCE_SHA1}" "${archive_path}"

  mkdir -p "${reference_dir}"
  tar -xzf "${archive_path}" -C "${reference_dir}" --strip-components=1
}

extract_vector_archive() {
  local archive_path="$1"
  local dest_dir="$2"
  local expected_count="$3"
  local tmp_dir
  local found_count

  tmp_dir="$(mktemp -d "${work_dir}/vectors.XXXXXX")"
  mkdir -p "${dest_dir}"
  tar -xzf "${archive_path}" -C "${tmp_dir}"
  find "${tmp_dir}" -type f \( -name 'testvector*.bit' -o -name 'testvector*.dec' \) -exec cp {} "${dest_dir}" \;

  found_count="$(find "${dest_dir}" -type f | wc -l | tr -d ' ')"
  if [ "${found_count}" != "${expected_count}" ]; then
    echo "expected ${expected_count} vector files in ${dest_dir}, found ${found_count}" >&2
    exit 1
  fi
}

verify_rfc6716_vector_sha1s() {
  # RFC 6716 Appendix A.4 publishes SHA-1 hashes for the extracted vector files.
  verify_sha1_manifest "$1" <<'EOF'
e49b2862ceec7324790ed8019eb9744596d5be01  testvector01.bit
b809795ae1bcd606049d76de4ad24236257135e0  testvector02.bit
e0c4ecaeab44d35a2f5b6575cd996848e5ee2acc  testvector03.bit
a0f870cbe14ebb71fa9066ef3ee96e59c9a75187  testvector04.bit
9b3d92b48b965dfe9edf7b8a85edd4309f8cf7c8  testvector05.bit
28e66769ab17e17f72875283c14b19690cbc4e57  testvector06.bit
bacf467be3215fc7ec288f29e2477de1192947a6  testvector07.bit
ddbe08b688bbf934071f3893cd0030ce48dba12f  testvector08.bit
3932d9d61944dab1201645b8eeaad595d5705ecb  testvector09.bit
521eb2a1e0cc9c31b8b740673307c2d3b10c1900  testvector10.bit
6bc8f3146fcb96450c901b16c3d464ccdf4d5d96  testvector11.bit
338c3f1b4b97226bc60bc41038becbc6de06b28f  testvector12.bit
a20a2122d42de644f94445e20185358559623a1f  testvector01.dec
48ac1ff1995250a756e1e17bd32acefa8cd2b820  testvector02.dec
d15567e919db2d0e818727092c0af8dd9df23c95  testvector03.dec
1249dd28f5bd1e39a66fd6d99449dca7a8316342  testvector04.dec
93eee37e5d26a456d2c24483060132ff7eae2143  testvector05.dec
a294fc17e3157768c46c5ec0f2116de0d2c37ee2  testvector06.dec
2bf550e2f072e0941438db3f338fe99444385848  testvector07.dec
2695c1f2d1f9748ea0bf07249c70fd7b87f61680  testvector08.dec
12862add5d53a9d2a7079340a542a2f039b992bb  testvector09.dec
a081252bb2b1a902fdc500530891f47e2a373d84  testvector10.dec
dfd0f844f2a42df506934fac2100a3c03beec711  testvector11.dec
8c16b2a1fb60e3550ba165068f9d7341357fdb63  testvector12.dec
EOF
}

verify_rfc8251_vector_sha1s() {
  # RFC 8251 Section 11 publishes SHA-1 hashes for the extracted vector files.
  verify_sha1_manifest "$1" <<'EOF'
e49b2862ceec7324790ed8019eb9744596d5be01  testvector01.bit
b809795ae1bcd606049d76de4ad24236257135e0  testvector02.bit
e0c4ecaeab44d35a2f5b6575cd996848e5ee2acc  testvector03.bit
a0f870cbe14ebb71fa9066ef3ee96e59c9a75187  testvector04.bit
9b3d92b48b965dfe9edf7b8a85edd4309f8cf7c8  testvector05.bit
28e66769ab17e17f72875283c14b19690cbc4e57  testvector06.bit
bacf467be3215fc7ec288f29e2477de1192947a6  testvector07.bit
ddbe08b688bbf934071f3893cd0030ce48dba12f  testvector08.bit
3932d9d61944dab1201645b8eeaad595d5705ecb  testvector09.bit
521eb2a1e0cc9c31b8b740673307c2d3b10c1900  testvector10.bit
6bc8f3146fcb96450c901b16c3d464ccdf4d5d96  testvector11.bit
338c3f1b4b97226bc60bc41038becbc6de06b28f  testvector12.bit
f5ef93884da6a814d311027918e9afc6f2e5c2c8  testvector01.dec
48ac1ff1995250a756e1e17bd32acefa8cd2b820  testvector02.dec
d15567e919db2d0e818727092c0af8dd9df23c95  testvector03.dec
1249dd28f5bd1e39a66fd6d99449dca7a8316342  testvector04.dec
b85675d81deef84a112c466cdff3b7aaa1d2fc76  testvector05.dec
55f0b191e90bfa6f98b50d01a64b44255cb4813e  testvector06.dec
61e8b357ab090b1801eeb578a28a6ae935e25b7b  testvector07.dec
a58539ee5321453b2ddf4c0f2500e856b3966862  testvector08.dec
bb96aad2cde188555862b7bbb3af6133851ef8f4  testvector09.dec
1b6cdf0413ac9965b16184b1bea129b5c0b2a37a  testvector10.dec
b1fff72b74666e3027801b29dbc48b31f80dee0d  testvector11.dec
98e09bbafed329e341c3b4052e9c4ba5fc83f9b1  testvector12.dec
1e7d984ea3fbb16ba998aea761f4893fbdb30157  testvector01m.dec
48ac1ff1995250a756e1e17bd32acefa8cd2b820  testvector02m.dec
d15567e919db2d0e818727092c0af8dd9df23c95  testvector03m.dec
1249dd28f5bd1e39a66fd6d99449dca7a8316342  testvector04m.dec
d70b0bad431e7d463bc3da49bd2d49f1c6d0a530  testvector05m.dec
6ac1648c3174c95fada565161a6c78bdbe59c77d  testvector06m.dec
fc5e2f709693738324fb4c8bdc0dad6dda04e713  testvector07m.dec
aad2ba397bf1b6a18e8e09b50e4b19627d479f00  testvector08m.dec
6feb7a7b9d7cdc1383baf8d5739e2a514bd0ba08  testvector09m.dec
1b6cdf0413ac9965b16184b1bea129b5c0b2a37a  testvector10m.dec
fd3d3a7b0dfbdab98d37ed9aa04b659b9fefbd18  testvector11m.dec
98e09bbafed329e341c3b4052e9c4ba5fc83f9b1  testvector12m.dec
EOF
}

write_result_comment() {
  local status="$1"
  local status_text="pass"

  if [ "${status}" -ne 0 ]; then
    status_text="fail (informational)"
  fi

  {
    echo "<!-- opus-rfc-conformance -->"
    echo "## RFC 6716 / 8251 conformation"
    echo
    echo "**Status:** ${status_text}"
    echo
    echo "The action extracts the RFC 6716 reference implementation, applies the RFC 8251 decoder update patch, and then builds the patched reference tools."
    if [ "${status}" -ne 0 ]; then
      echo
      echo "This check is informational while CELT support is incomplete; the workflow still reports success."
    fi
    echo
    if [ -s "${matrix_file}" ]; then
      cat "${matrix_file}"
    else
      echo "The conformance run did not produce a result matrix."
    fi
    echo
    echo "<details><summary>Run output</summary>"
    echo
    echo '```text'
    tail -n 200 "${log_file}"
    echo '```'
    echo "</details>"
  } >"${result_file}"

  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    cat "${result_file}" >>"${GITHUB_STEP_SUMMARY}"
  fi
}

main() {
  require_command base64
  require_command curl
  require_command find
  require_command go
  require_command make
  require_command patch
  require_command sed
  require_command tar

  rm -rf "${work_dir}"
  mkdir -p "${work_dir}"

  local rfc_path="${work_dir}/rfc6716.txt"
  local source_archive="${work_dir}/opus-rfc6716.tar.gz"
  local patch_path="${work_dir}/rfc8251.patch"
  local reference_dir="${work_dir}/reference"
  local vector_dir="${work_dir}/testvectors"
  local rfc6716_vectors="${work_dir}/opus_testvectors.tar.gz"
  local rfc8251_vectors="${work_dir}/opus_testvectors-rfc8251.tar.gz"

  download "${RFC6716_URL}" "${rfc_path}"
  extract_reference_source "${rfc_path}" "${source_archive}" "${reference_dir}"

  download "${RFC8251_PATCH_URL}" "${patch_path}"
  verify_sha1 "${RFC8251_PATCH_SHA1}" "${patch_path}"
  echo "Applying RFC 8251 decoder update patch to the RFC 6716 reference implementation"
  patch -d "${reference_dir}" -p1 <"${patch_path}"

  download "${RFC6716_VECTORS_URL}" "${rfc6716_vectors}"
  extract_vector_archive "${rfc6716_vectors}" "${vector_dir}/rfc6716" 24
  verify_rfc6716_vector_sha1s "${vector_dir}/rfc6716"

  download "${RFC8251_VECTORS_URL}" "${rfc8251_vectors}"
  extract_vector_archive "${rfc8251_vectors}" "${vector_dir}/rfc8251" 36
  verify_rfc8251_vector_sha1s "${vector_dir}/rfc8251"

  export OPUS_RFC6716_REFERENCE="${reference_dir}"
  export OPUS_RFC6716_TESTVECTORS="${vector_dir}"
  export OPUS_CONFORMANCE_MARKDOWN="${matrix_file}"

  set +e
  go test -v -timeout 60m -tags conformance -run TestRFC6716Conformance . 2>&1 | tee "${log_file}"
  local test_status="${PIPESTATUS[0]}"
  set -e

  write_result_comment "${test_status}"

  return "${test_status}"
}

main "$@"
