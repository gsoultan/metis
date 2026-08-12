#!/usr/bin/env node
/**
 * Fails the build when the first-paint payload grows past its budget.
 *
 * "Performance" as a review comment does not survive contact with a deadline.
 * A number that fails CI does. This measures what the browser must download
 * before it can render anything — the assets referenced directly by
 * index.html — because that is what a user waits through, not the total size
 * of dist/.
 *
 * Budgets are gzip kB and deliberately set just above the current measurement:
 * close enough that a careless import trips them, loose enough that a genuine
 * feature does not. Raise one only with a note in the commit saying what was
 * added and why it belongs in the first paint.
 */
import { readFileSync, readdirSync } from 'node:fs';
import { gzipSync } from 'node:zlib';
import { join } from 'node:path';

const DIST = new URL('../dist/', import.meta.url).pathname;

const BUDGETS = {
  /** Everything index.html pulls in before the app can paint. */
  initialTotal: 330,
  /** The single largest eager chunk, to catch one dependency swallowing the app. */
  largestChunk: 110,
  /** CSS blocks rendering, so it gets its own ceiling. */
  initialCss: 45,
};

function gzipKb(path) {
  return gzipSync(readFileSync(path)).length / 1024;
}

const html = readFileSync(join(DIST, 'index.html'), 'utf8');
const eager = [...html.matchAll(/assets\/([^"']+\.(?:js|css))/g)].map((m) => m[1]);

if (eager.length === 0) {
  console.error('bundle-budget: no assets referenced from index.html — did the build run?');
  process.exit(1);
}

let total = 0;
let largest = { name: '', kb: 0 };
let css = 0;

const rows = eager.map((name) => {
  const kb = gzipKb(join(DIST, 'assets', name));
  total += kb;
  if (name.endsWith('.css')) css += kb;
  else if (kb > largest.kb) largest = { name, kb };
  return { name, kb };
});

rows.sort((a, b) => b.kb - a.kb);
console.log('\nFirst-paint payload (gzip):');
for (const { name, kb } of rows) {
  console.log(`  ${kb.toFixed(1).padStart(7)} kB  ${name}`);
}

const checks = [
  ['initial total', total, BUDGETS.initialTotal],
  ['largest chunk', largest.kb, BUDGETS.largestChunk, largest.name],
  ['initial CSS', css, BUDGETS.initialCss],
];

let failed = false;
console.log('');
for (const [label, actual, budget, detail] of checks) {
  const ok = actual <= budget;
  if (!ok) failed = true;
  const status = ok ? 'ok  ' : 'OVER';
  const suffix = detail ? ` (${detail})` : '';
  console.log(`  ${status} ${label.padEnd(14)} ${actual.toFixed(1).padStart(7)} / ${budget} kB${suffix}`);
}

// Chunks that are NOT in the first paint are the point of code splitting;
// report them so a regression that makes one eager is visible.
const lazy = readdirSync(join(DIST, 'assets'))
  .filter((f) => f.endsWith('.js') && !eager.includes(f))
  .map((f) => ({ f, kb: gzipKb(join(DIST, 'assets', f)) }))
  .sort((a, b) => b.kb - a.kb)
  .slice(0, 3);

if (lazy.length) {
  console.log('\n  Loaded on demand (largest):');
  for (const { f, kb } of lazy) console.log(`    ${kb.toFixed(1).padStart(7)} kB  ${f}`);
}

if (failed) {
  console.error('\nbundle-budget: first-paint payload exceeds its budget.');
  console.error('Move the addition behind a lazy route, or raise the budget with a note saying why.\n');
  process.exit(1);
}

console.log('\nbundle-budget: within budget.\n');
