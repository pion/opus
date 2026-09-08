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
assert PCM equality or perceptual equivalence. It now also checks against
the recorded pre-periodic-PLC baseline and gates aggregate improvement. Equal final
ranges on recovery prove entropy-decoding agreement, not equal synthesis
history. The separate duration matrix compares the public API with a direct
short-frame invocation of Pion's CELT core, including recovery PCM.

Historical baseline at `c0d7ee63cecdc35aa81b83cbde40c70148da9e74`, Go 1.26.1,
Linux/amd64, before periodic PLC (three losses, then recovery):

| Duration | Loss RMSE range | First recovery RMSE | Second recovery RMSE |
| --- | ---: | ---: | ---: |
| 2.5 ms | 411–930 | 1123 | 741 |
| 5 ms | 908–1089 | 1023 | 748 |
| 10 ms | 1003–1373 | 1014 | 745 |
| 20 ms (existing API) | 1241–1364 | 1112 | 743 |

Before any loss, mono/stereo CELT seed PCM differs by at most one int16 unit.
The SILK→CELT transition seed differs by at most two. Baseline stereo loss RMSE ranges
from 1227 to 2583 across the new short durations; first recovery from 1251 to
1652. Transition cases have loss RMSE 907–2200 and first recovery 1196–1204.
These are measurements on this synthetic fixture, not quality targets or
evidence of improvement over libopus. The extension reuses existing PLC
synthesis and exposes its duration correctly; improving PLC quality is
the follow-up below.

## Periodic PLC follow-up

The decoder now implements the standard (non-neural) periodic/noise PLC state
machine from the same libopus pin: a bounded 2048-sample pre-deemphasis history,
pitch search, 24th-order LPC excitation/synthesis, repeated-loss attenuation,
energy explosion rejection, noise floor tracking, and prefilter/TDAC overlap
repair before resuming MDCT synthesis. Loss duration is counted in 2.5 ms units.
At 100 ms consecutive concealment, Hybrid start bands, or when periodic PLC
is not ready, it uses noise. Noise recovery needs two consecutive received
frames before periodic PLC is enabled again. No public API or dependencies change.

`corpus.c` supplies 502 deterministic streams: sawtooth, frequency change,
decay, seeded noise, impulses and silence; all five API rates, mono/stereo and
channel conversion; short and mixed frames, long bursts through the noise
threshold, losses after one/two received packets, channel switching,
SILK-to-CELT, CELT-to-SILK and Hybrid. The encoder advances even for lost frames.
`corpus.json.gz` contains the public API reference PCM and packets.
`corpus-state.jsonl.gz` contains per-step type, loss duration, skip flag, pitch,
and four two-channel energy histories from the reference.
`primitives.c` / `primitives.json.gz` independently exercise downsampling,
pitch selection, windowed 24-lag autocorrelation, and LPC on six mono/stereo
signals. These isolate float64 accumulation in Go from float32 in generic C;
they do not substitute for the public PCM quality gate.

**Matching decoder policy:** libopus 1.6.1 disables intensity-stereo phase
inversion for mono output by default (`celt_decoder.c` initialization).
Pion retains signaled inversion. The corpus explicitly sets
`OPUS_SET_PHASE_INVERSION_DISABLED(0)` in libopus to match that existing policy.
Without it, the two decoders already disagree before a loss in stereo-to-mono
fullband streams, giving different pitch-search inputs. No PLC selection is
forced and no production Pion behavior was changed to accommodate the fixture.

`trace.h` compiles the exact `celt_decoder.c` into the generator translation
unit and reads its state without changing it. The linker does not pull the
duplicate decoder object from the static archive. `regenerate.sh` builds plain
and observation-only generators and requires byte-identical output before
writing fixtures. The original short fixture remains byte-identical too.

```sh
bash testdata/short-plc/regenerate.sh /path/to/prepared/pinned/libopus
go test -run 'TestDecodePLCShortLibopus|TestPLCCorpus|TestCELTPLCWarmAllocations' .
go test -run 'TestPLC|TestRecoveryEnergy' ./internal/celt
```

Recorded SHA-256:

- Uncompressed corpus, plain **and** traced:
  `3500d49486124ff7e786e0ce03a1df56e194fcee41bc4898558b935d84776f23`.
- `corpus.json.gz`:
  `7ddc981676df11fb0b01764fed3b63862cf371ccb82ccacabc18142027a17997`.
- Original `libopus.json`:
  `0e892bfe2ea415efe277d9f063d46d6bd13c244f5c1e3d9da076c90616ddf426`.

### Quality gates and measurements

`baseline.json` was generated by running the corpus against the unchanged
`c0d7ee63cecdc35aa81b83cbde40c70148da9e74` source in a separate worktree with
only `decoder_plc_corpus_test.go` added. Each step records RMSE, peak difference,
output energy, first-channel boundary delta and a PCM SHA-256.
Tests require every step's RMSE to stay below baseline + one int16 unit,
unchanged pre-loss/no-loss PCM hashes, and at least halved aggregate RMSE in
each periodic-signal scenario. They do not weaken this gate for transitions.
Core state tests require exact mode/duration/skip, energy agreement within
0.001 log2 units, and pitch within one sample (floating-point interpolation).

`baseline-arm64.json.gz` records the **same unchanged base commit** compiled
for Linux/arm64 with Go 1.26.1, run under QEMU 8.2.2. ARM64 floating-point
code generation already changes 280 pre-loss/no-loss hashes relative to amd64
on the base commit. Comparing base and current on ARM64 gives zero changed
pre-loss/no-loss hashes, zero per-step regressions, and passes the same halved
periodic-error gate. ARM64 tests select this baseline, keeping exact hash
checks rather than tolerating differences or changing production rounding.

Reproduce baseline in that clean source tree, adding the test file only:

```sh
PLC_CORPUS_PATH=/path/to/current/testdata/short-plc/corpus.json.gz \
PLC_BASELINE_OUTPUT=/path/to/baseline.json go test -run '^TestPLCCorpus$' -count=1 .
```

For the ARM64 baseline, in the same unchanged base source plus test file:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o /tmp/plc-base-arm64.test .
PLC_CORPUS_PATH=/path/to/current/testdata/short-plc/corpus.json.gz \
PLC_BASELINE_OUTPUT=/tmp/plc-base-arm64.json \
qemu-aarch64 /tmp/plc-base-arm64.test -test.run='^TestPLCCorpus$' -test.count=1
gzip -n -c /tmp/plc-base-arm64.json > /path/to/baseline-arm64.json.gz
```

For a measurement report on current code, write to a **different** output file
with `PLC_BASELINE_OUTPUT` and pass that path to
`node testdata/short-plc/summarize.mjs /path/to/current.json`.
Never set the recording environment variable in current-code acceptance tests.
The PLC quality workflow records the unchanged base in a separate checkout,
then runs the current-code acceptance test with `PLC_BASELINE_PATH` pointing
to that measurement. Both builds use the same compiler, architecture, and
race mode; the current-code test retains every quality and exact-hash gate.

Weighted RMSE in int16 units across the 502-case corpus, Go 1.26.1 Linux/amd64:

| Signal/profile | Lost PCM: before → after | Recovery PCM: before → after |
| --- | ---: | ---: |
| Periodic, changing frequency, decay | 1562.55 → 3.56 | 1020.25 → 0.74 |
| Noise, impulse, silence | 672.43 → 32.81 | 544.18 → 11.07 |
| SILK→CELT | 1868.41 → 515.58 | 675.84 → 0.05 |
| Hybrid | 1150.47 → 1150.47 | 887.85 → 887.85 |
| CELT→SILK | 1601.58 → 1601.58 | 973.33 → 973.33 |

All 502 scenarios pass the per-step non-regression gate. No-loss hashes are
unchanged. These aggregate values do not hide the remaining peak differences:
periodic loss peak is 440, noise/impulse loss peak 2459; SILK-based loss remains
substantially different from libopus. This is not bit-exact PLC, a perceptual
quality score, a SILK PLC improvement, or a claim of equivalence to neural PLC.

### Cost

Median of three 200-iteration runs, 48 kHz / 20 ms, AMD Ryzen 9 9950X3D,
Go 1.26.1, Linux/amd64. Fixture priming is outside the timed region.

| Operation | Mono before → after (µs) | Stereo before → after (µs) |
| --- | ---: | ---: |
| Normal decode | 25.43 → 25.20 | 45.00 → 43.53 |
| First loss | 12.23 → 60.69 | 22.90 → 103.12 |
| Repeated loss | 12.76 → 23.15 | 22.67 → 51.24 |
| Noise fallback | 12.33 → 12.04 | 23.30 → 22.71 |

Every measured path is 0 B/op, 0 allocs/op after warm-up. The first-loss CPU
increase is the cost of pitch/LPC analysis, not a speed improvement. Small
normal-decode differences are measurement noise, not a performance claim.

The periodic synthesis and recovery predictor retain the BSD-2-Clause notices
of libopus authors alongside Pion's MIT contributions; see the source headers
and `LICENSES/BSD-2-Clause.txt`.
