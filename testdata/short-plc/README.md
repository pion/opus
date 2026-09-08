<!-- SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly> -->
<!-- SPDX-License-Identifier: MIT -->

# Short CELT PLC reference fixture

`libopus.json` is generated from synthetic integer PCM by `generate.c`, using
Xiph libopus commit `22244de5a79bd1d6d623c32e72bf1954b56235be` (1.6.1).
It contains three seed packets, three losses, and two recovery packets per
case. The encoder also advances through each missing interval at its exact
duration, discarding those packets. The decoder output is 16 kHz, mono/stereo.
The ordinary cases use restricted-lowdelay CELT, CBR 64 kbps per channel,
complexity 10, DTX off. Transition cases use the pinned test-only force-mode
control (`src/opus_private.h`, request 11002) to switch SILK to CELT. The last
seed packet contains trailing CELT redundancy; the Go test asserts that this
state was actually reached before requesting short PLC.

Regenerate on Linux with an LF checkout of the exact commit (requires the
normal libopus autotools prerequisites):

```sh
git checkout --detach 22244de5a79bd1d6d623c32e72bf1954b56235be
./autogen.sh
./configure --disable-shared --disable-intrinsics --disable-extra-programs --disable-doc
make -j4
cc -O2 -Iinclude /path/to/pion/opus/testdata/short-plc/generate.c .libs/libopus.a -lm -o generate
./generate > /path/to/pion/opus/testdata/short-plc/libopus.json
```

Recorded with GCC 13.3.0, Linux/amd64; floating-point build, default fast
float approximations, intrinsics disabled. The source archive used for the
recorded build had CRLF normalized to LF. No model-based PLC was enabled.
Ordinary Go tests consume the checked-in fixture and need neither C nor cgo.

## Interpretation

`go test -run TestDecodePLCShortLibopus -v .` reports RMSE and maximum absolute
PCM differences in int16 units separately for seed, loss, and recovery.
The test asserts sample counts and final range on every step. It does **not**
assert PCM equality or perceptual equivalence: Pion's current CELT PLC uses
noise synthesis, while libopus can use pitch-based concealment. Equal final
ranges on recovery prove entropy-decoding agreement, not equal synthesis
history. The separate duration matrix compares the public API with a direct
short-frame invocation of Pion's CELT core, including recovery PCM.

Observed mono results on Go 1.26.1, Linux/amd64 (three losses, then recovery):

| Duration | Loss RMSE range | First recovery RMSE | Second recovery RMSE |
| --- | ---: | ---: | ---: |
| 2.5 ms | 411–930 | 1123 | 741 |
| 5 ms | 908–1089 | 1023 | 748 |
| 10 ms | 1003–1373 | 1014 | 745 |
| 20 ms (existing API) | 1241–1364 | 1112 | 743 |

Before any loss, mono/stereo CELT seed PCM differs by at most one int16 unit.
The SILK→CELT transition seed differs by at most two. Stereo loss RMSE ranges
from 1227 to 2583 across the new short durations; first recovery from 1251 to
1652. Transition cases have loss RMSE 907–2200 and first recovery 1196–1204.
These are measurements on this synthetic fixture, not quality targets or
evidence of improvement over libopus. The extension reuses existing PLC
synthesis and exposes its duration correctly; improving PLC quality is
separate work.
