<!-- SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly> -->
<!-- SPDX-License-Identifier: MIT -->

# Decoder and PLC reference fixtures

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
bash /path/to/pion/opus/testdata/short-plc/regenerate.sh \
  /path/to/prepared/libopus \
  /path/to/rfc8251/opus_newvectors/testvector02.dec
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

The first 502 rows from `corpus.c` preserve the #246 deterministic matrix:
sawtooth, frequency change, decay, seeded noise, impulses and silence; all five
API rates, mono/stereo and channel conversion; short and mixed frames, long
bursts through the noise threshold, losses after one/two received packets,
channel switching, SILK-to-CELT, CELT-to-SILK and Hybrid. The bit-exact follow-up
extends that matrix to 1300 rows with pure SILK and Hybrid duration/channel
coverage plus 50 rows generated from RFC 8251 `testvector02.dec`. Its published
SHA-1 is `48ac1ff1995250a756e1e17bd32acefa8cd2b820`; regeneration checks it before use.
The encoder advances even for lost frames.
`corpus.json.gz` contains the public API reference PCM and packets.
`corpus-state.jsonl.gz` contains per-step type, loss duration, skip flag, pitch,
and four two-channel energy histories from the reference.
`primitives.c` / `primitives.json.gz` independently exercise downsampling,
pitch selection, windowed 24-lag autocorrelation, and LPC on six mono/stereo
signals. These isolate float64 accumulation in Go from float32 in generic C;
they do not substitute for the public PCM quality gate.
The exactly periodic square-wave primitive can select different harmonic
aliases under ARM floating-point arithmetic. Its test requires both selected
periods to reproduce the entire input exactly; other cases require the same
pitch. LPC coefficients allow 0.00002 absolute numerical difference. Public
PCM RMSE thresholds and state-trace tolerances are unchanged.

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
bash testdata/short-plc/regenerate.sh \
  /path/to/prepared/pinned/libopus \
  /path/to/rfc8251/opus_newvectors/testvector02.dec
go test -run 'TestDecodePLCShortLibopus|TestPLCCorpus|TestPLCBitExactCorpus|TestCELTPLCWarmAllocations|TestSILKAndHybridPLCWarmAllocations' .
go test -run 'TestPLC|TestRecoveryEnergy' ./internal/celt
```

Recorded SHA-256:

- Uncompressed corpus, plain **and** traced:
  `673519e90be9ca1e65cd55aa0ee764043baeec3a05a0328d74be41a89b60477c`.
- `corpus.json.gz`:
  `525df230044aa05e0cb6ea0a99ac9c6d7551464d99d300a3e49153c1c0a09f3e`.
- `corpus-state.jsonl.gz`:
  `0a48c63b8b88f5ede45604697496cdc31f33cb5127cb06ee95cecf441270c9d1`.
- Original `libopus.json`:
  `0e892bfe2ea415efe277d9f063d46d6bd13c244f5c1e3d9da076c90616ddf426`.

### Quality gates and measurements

`baseline.json` was generated by running the corpus against the unchanged
`c0d7ee63cecdc35aa81b83cbde40c70148da9e74` source in a separate worktree with
only `decoder_plc_corpus_test.go` added. Each step records RMSE, peak difference,
and a PCM SHA-256. All three baselines have exactly 1300 rows and the same
28,600-step shape as the corpus; tests reject a truncated corpus, baseline, or
state trace. Tests require every step's RMSE to stay below baseline + one int16
unit, exact reference PCM before loss and in no-loss scenarios, and at least
halved aggregate RMSE in each periodic-signal scenario. They do not weaken this
gate for transitions. Core state tests require exact mode/duration/skip/pitch
and energy agreement within 0.001 log2 units.

Baseline SHA-256:

- `baseline.json`:
  `ce74288c8bed310acca0caa07136d164917ccb137816a7086ba9b516ac5c9798`.
- `baseline-arm64.json.gz`:
  `fb13a0b68ffd33d7b99ec279f509c2f5331ea903591241855c8642390a0fcd6f`.
- `baseline-arm64-race.json.gz`:
  `a653a2e90b70166ecb608940cf512ad473d621d50d6683e20ea5808e0f82bcaf`.

`baseline-arm64.json.gz` records the **same unchanged base commit** compiled
for Linux/arm64 with Go 1.26.1 and run under QEMU. ARM64 floating-point code
generation changes portions of the historical base output relative to amd64;
the current decoder is nevertheless required to match every reference sample
exactly before loss and passes the same per-step and aggregate quality gates.
`baseline-arm64-race.json.gz` is the same base built with `-race` using Go
1.25.14 on the `macos-26-arm64` runner in successful
[run 34674862562](https://github.com/pion/opus/actions/runs/34674862562). The
uncompressed workflow artifact has SHA-256
`f42a906f00d17a42377a679632c7ccb1039258579e9c1eacb84382ae8840fa63`.
Race instrumentation changes floating-point code generation on ARM64 too, so
race builds select this measurement. Neither snapshot contains new-code output.
The extra CI comparison also rebuilds the base with the current runner/compiler
instead of trusting only the recorded files.

Golden resolution is explicit: amd64 and every architecture other than arm64
use `baseline.json`; ordinary arm64 uses `baseline-arm64.json.gz`; arm64 under
`-race` uses `baseline-arm64-race.json.gz`. A future compiler or architecture
that changes the historical base measurement must add a base-built golden
rather than relax the current decoder's exact reference-PCM requirement.

Reproduce baseline in that clean source tree, adding only the corpus test and
its two `decoder_plc_*race_test.go` build-flag files:

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
race mode; the current-code test retains every quality and exact-reference gate.

Historical weighted RMSE in int16 units across the original 502-case portion
of the expanded corpus, before and after merged #246, Go 1.26.1 Linux/amd64:

| Signal/profile | Lost PCM: before → after | Recovery PCM: before → after |
| --- | ---: | ---: |
| Periodic, changing frequency, decay | 1562.55 → 3.56 | 1020.25 → 0.74 |
| Noise, impulse, silence | 672.43 → 32.81 | 544.18 → 11.07 |
| SILK→CELT | 1868.41 → 515.58 | 675.84 → 0.05 |
| Hybrid | 1150.47 → 1150.47 | 887.85 → 887.85 |
| CELT→SILK | 1601.58 → 1601.58 | 973.33 → 973.33 |

The historical aggregates do not hide #246's remaining peak differences:
periodic loss peak was 440 and noise/impulse loss peak was 2459; SILK-based
loss remained substantially different from libopus. The exact follow-up makes
all 1300 scenarios match the reference before, during, and after loss. Neither
table is a perceptual-quality score or a claim about neural PLC.

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

The resident-memory tradeoff on amd64 is separate from per-call allocation:
`Decoder` grows from 744 to 18,472 bytes, `decoderScratch` from 117,696 to
140,936 bytes, and `encoderScratch` from 8,488 to 9,640 bytes. A decoder after
materializing its scratch therefore retains about 41 KiB more state, primarily
for bounded PLC history and work buffers.

### Bit-exact follow-up

The PLC exactness change is stacked on the ordinary-decoder exactness change,
which starts from merged periodic PLC commit
`b8ebd659d671eae11ea7262b3f66d79f103539ec`.
Its reference is libopus `22244de5a79bd1d6d623c32e72bf1954b56235be`,
generic floating point, fast float approximations enabled, intrinsics/RTCD and
neural PLC/DRED disabled, with `OPUS_SET_PHASE_INVERSION_DISABLED(0)`.

`TestPLCBitExactCorpus` checks 1300 scenarios, 28,600 decode steps and 10,620
loss requests. It requires exact sample counts, final ranges and every int16
sample before loss, during concealment and after recovery. Current result is
zero unequal samples across CELT, SILK, Hybrid, both forced transition
directions, all five public output rates, mono/stereo conversion, channel-count
changes, supported short/mixed/40/60 ms durations, the 100 ms noise boundary,
recovery and a new loss after received packets. Modes are recorded as 0 CELT,
1 SILK→CELT, 2 Hybrid, 3 CELT→SILK and 4 SILK. Signal 6 is the RFC 8251 PCM;
signals 0–5 are the deterministic synthetic classes above.

The first historical CELT mismatch was sample 28 (`-734` versus `-733`), and
its float values already differed before int16 conversion. Observation-only
fixtures localize the fixes through PVQ normalization/rotation, decoder-only
amplitude math, inverse MDCT, synthesis, pitch/LPC and overlap. SILK fixtures
compare fixed-point excitation, LPC/LTP state, gain, CNG, PRNG, stereo and
resampling. CELT PLC fixtures compare periodic and noise state, exact pitch,
autocorrelation, FIR/IIR order, energy history and recovery overlap. The
high-amplitude RFC rows additionally caught a missing integer-path soft clip;
all integer entry points now share its inter-frame state while float entry
points clear it like `opus_decode_float`.

Every diagnostic corpus build is compared byte-for-byte with the plain build
before a fixture is accepted. Diagnostic observation therefore has zero PCM
effect. Decoder-specific arithmetic is isolated from encoder helpers.
`pitchScratch` remains shared bounded storage, while CELT PLC deliberately owns
`celtPLCPitchSearch`, `celtPLCPitchDownsample`, `celtPLCPitchXcorr`,
`celtPLCRefineXcorr`, and its normalized-correlation selection. Those copies
preserve reference-ordered float32 accumulation and prevent contraction;
encoder `pitchSearch`/`pitchDownsample` arithmetic and encoded packets remain
unchanged. The `//nolint:dupl` boundary is therefore intentional.

Decoder float32 products that feed reference-ordered additions use explicit
rounding barriers. This prevents Go's ARM64 backend from contracting them into
FMA instructions and keeps the accepted PCM identical to amd64; encoder
analysis helpers retain their existing arithmetic.

Run the strict and broad gates with:

```sh
go test . -run '^TestPLCBitExactCorpus$' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
go mod verify
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.1 run
```

The strict deterministic corpus and broad quality corpus are skipped in the
generic race+coverage job. The PLC quality workflow runs the strict corpus
explicitly without `-race`, then runs the broad historical/current comparison
with race instrumentation. The generic job still races all remaining PLC tests.

The RFC 6716/8251 conformance test also passes all 120 combinations of 12
published bitstreams, five output rates and mono/stereo output against
`opus_compare`. Fuzz runs cover the public decoder, CELT PLC sequences and the
range coder. This exact result is scoped to the pinned profile and accepted
corpus; it is not a claim for every libopus build option or neural PLC.

### Current cost

Median of three 200-iteration runs, Windows/amd64, Go 1.26.1, AMD Ryzen 9
9950X3D. Times are microseconds per operation after fixture setup.

| Mode/path | Mono | Stereo |
| --- | ---: | ---: |
| CELT normal | 14.71 | 28.12 |
| CELT first periodic loss | 64.97 | 109.01 |
| CELT repeated periodic loss | 23.81 | 57.72 |
| CELT noise PLC | 8.22 | 14.88 |
| Hybrid normal | 31.65 | 63.49 |
| Hybrid first loss | 24.54 | 51.66 |
| Hybrid repeated loss | 24.89 | 53.21 |
| SILK normal | 19.59 | 47.56 |
| SILK first loss | 19.22 | 41.18 |
| SILK repeated loss | 19.25 | 42.25 |

`AllocsPerRun(100)` is zero for warmed CELT, SILK and Hybrid PLC, including a
series that reaches noise fallback. The ordinary SILK benchmark currently
reports 24 B and one allocation per packet; this is recorded rather than
misstated as a PLC regression. First-loss CELT CPU remains the explicit cost of
pitch/LPC analysis. CPU benchmarks skip race-instrumented runs because those
timings are not representative; the same PLC paths remain covered by race
tests.

### Listening examples

`audio/` contains sample-aligned 48 kHz WAVs for CELT periodic mono, Hybrid RFC
stereo and SILK RFC stereo. Each has `base-9e3044b`, `reference` and `current`
versions. Current/reference SHA-256 values are identical for all three; base
differs. These files permit manual listening but do not turn listening into the
objective acceptance criterion.

The CELT ports retain BSD-2-Clause notices and the SILK fixed-point ports retain
BSD-3-Clause notices alongside Pion's MIT contributions; see the source headers
and `LICENSES/`.
