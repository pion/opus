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
