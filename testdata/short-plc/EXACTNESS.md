<!-- SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly> -->
<!-- SPDX-License-Identifier: MIT -->

# Exactness scope and review regressions

The reference is generic floating-point libopus at
`22244de5a79bd1d6d623c32e72bf1954b56235be`, with float approximations enabled,
intrinsics/RTCD and neural PLC/DRED disabled, and phase inversion enabled.
Passing RFC conformance is a separate, tolerance-based interoperability check;
it does not prove bit-exact output for every packet or state transition.

`TestSoftClipLibopusReference` compares every float32 output bit and the carried
clipping memory across 128 consecutive frames for each of mono and stereo.
Inputs include overshoot, alternating signs, peaks spanning frame boundaries,
and values outside the clipping curve's domain. This catches ARM64 fused
multiply-add differences which the range/continuity tests cannot detect.
The test runs without race instrumentation on Ubuntu and native macOS/ARM64.

Reproduce `softclip-reference.json.gz` from a clean reference checkout at the
pin above, configured with `CFLAGS='-O2 -ffp-contract=off'` and the profile above:

```sh
cc -O2 -ffp-contract=off -Iinclude /path/to/softclip-reference.c .libs/libopus.a -lm -o /tmp/softclip-reference
/tmp/softclip-reference | gzip -n > /path/to/softclip-reference.json.gz
```

## PLC review corpus

`TestDecoderReviewReference` adds 120 sequences, each with 12 steps: 1,440
decode steps including 480 loss requests. Every sequence is checked at all five
API output rates (8/12/16/24/48 kHz), in mono and stereo. Output sample count,
final range and every int16 PCM sample must match the pinned reference.

API output rate is independent of encoded bandwidth. The received packet TOC
is checked for the expected mode and bandwidth; this corpus covers:

| Profiles | Encoded mode/bandwidth | Additional state coverage |
| --- | --- | --- |
| 0-2 | SILK NB, MB, WB | Received, single/repeated loss, recovery |
| 3-4 | Hybrid SWB, FB | Received, single/repeated loss, recovery |
| 5-8 | CELT NB, WB, SWB, FB | Received, single/repeated loss, recovery |
| 9-10 | SILK NB -> MB -> WB and reverse | Internal-rate changes, including directly after loss |
| 11 | CELT FB with over-range input | Clipped packet -> loss -> recovery; nonzero clipping memory preserved |

All steps in this supplemental corpus are 20 ms. The existing 1,300-scenario
corpus separately covers the documented duration, channel-conversion and forced
transition cases; it alone is not evidence for all internal bandwidths.
The supplemental corpus uses fresh encoders at SILK rate changes but keeps the
decoder continuous. It does not exercise arbitrary mode/redundancy transitions.
Focused SILK tests additionally check the low-gain LTP signed-16-bit multiply
and retained CNG/PLC history on internal-rate changes.

Regenerate from the same clean reference checkout and configuration:

```sh
cc -O2 -ffp-contract=off -Iinclude /path/to/review-corpus.c .libs/libopus.a -lm -o /tmp/review-corpus
/tmp/review-corpus | gzip -n > /path/to/review-corpus.json.gz
```

Compressed fixture SHA-256:
`31a856bf22ca4f61c9502ba0eadf44b4ecd9f3c2b459b1d6648850cd86936509`.
The dedicated quality workflow runs both strict corpora without race
instrumentation on Ubuntu and native macOS/ARM64; the supplemental regression
test also remains in the ordinary race suite.

## Review boundaries

The ordinary decoder is reviewed in #247; PLC numerical/state corrections and
all PLC reference fixtures are reviewed in #250. Removing the unreachable SILK
floating-point fallback is a separate stacked cleanup, with no new codec
arithmetic, fixture changes or widened exactness claim. The corrected PLC PR
passes the same strict corpora before that cleanup is applied. Only #247 and
#250 require paired landing; the cleanup can be reviewed independently.

## Known exclusions

The three pre-existing redundancy/PLC transition-state differences tracked in
https://github.com/pion/opus/issues/251 are not covered by the exactness claim:
PLC mode/redundancy bookkeeping, Hybrid-to-SILK fade-out ordering relative to
redundancy, and the previous-mode guard for CELT-to-SILK redundancy. Each needs
a dedicated libopus sequence before it can be claimed as fixed.

The intermediate ordinary-decoder PR temporarily relaxes two legacy PLC
assertions. The PLC follow-up must land immediately afterwards to restore the
RMSE bound and exact pitch assertion. The intermediate bounds are compatibility
allowances, not evidence of PLC exactness.
