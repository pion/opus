// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT
import { readFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';
const corpus = JSON.parse(gunzipSync(readFileSync(new URL('corpus.json.gz', import.meta.url))));
const before = JSON.parse(readFileSync(new URL('baseline.json', import.meta.url)));
const after = JSON.parse(readFileSync(process.argv[2]));
const groups = {};
let regressions = 0, unchangedFailures = 0;
for (const [ci, c] of corpus.cases.entries()) {
  for (const [si, step] of c.steps.entries()) {
    const old = before[ci][si], now = after[ci][si];
    if (now.RMSE > old.RMSE + 1) regressions++;
    if ((c.sequence === 2 || si < 4) && old.Hash !== now.Hash) unchangedFailures++;
    if (c.sequence === 2 || si < 4) continue;
    const modeName = ['', 'SILK-to-CELT', 'hybrid', 'CELT-to-SILK'][c.mode];
    const key = (c.mode ? modeName : c.signal < 3 ? 'periodic' : 'noise-impulse-silence') + '/' + (step.packet ? 'recovery' : 'loss');
    const g = groups[key] ??= { samples: 0, before: 0, after: 0, maxPeak: 0 };
    const n = step.samples * c.output_channels;
    g.samples += n;
    g.before += old.RMSE ** 2 * n;
    g.after += now.RMSE ** 2 * n;
    g.maxPeak = Math.max(g.maxPeak, now.Peak);
  }
}
for (const g of Object.values(groups)) {
  g.before = Math.sqrt(g.before / g.samples);
  g.after = Math.sqrt(g.after / g.samples);
}
console.log(JSON.stringify({ cases: corpus.cases.length, regressions, unchangedFailures, groups }, null, 2));
