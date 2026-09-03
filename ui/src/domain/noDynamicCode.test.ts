import { describe, expect, it } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Nothing in the UI may construct code from a string.
 *
 * `new Function` and `eval` were how a deployed process definition ran
 * JavaScript in the browser of whoever opened its task: a user task's
 * `form_definition` carries `logic.hiddenIf`, `logic.disabledIf` and a
 * `customJs` validation rule, and all three were compiled and run. The victim
 * is normally an approver, and it ran in their session.
 *
 * The replacement is a bounded evaluator, and this is what keeps it. The
 * tempting regression is small and reasonable-looking — one condition the
 * parser does not cover, one `new Function` to handle it — and it reopens the
 * whole thing.
 */
const FORBIDDEN = [
  { pattern: /\bnew\s+Function\s*\(/, name: 'new Function' },
  { pattern: /(^|[^.\w])eval\s*\(/, name: 'eval' },
  { pattern: /\bsetTimeout\s*\(\s*['"`]/, name: 'setTimeout with a string' },
  { pattern: /\bsetInterval\s*\(\s*['"`]/, name: 'setInterval with a string' },
];

function sourceFiles(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir)) {
    if (entry === 'node_modules' || entry === 'gen' || entry === 'dist') continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      found.push(...sourceFiles(path));
    } else if (/\.(ts|tsx)$/.test(entry)) {
      found.push(path);
    }
  }
  return found;
}

describe('the UI never builds code from a string', () => {
  it('has no dynamic code construction outside comments', () => {
    const offenders: string[] = [];

    for (const file of sourceFiles(join(import.meta.dir, '..'))) {
      const lines = readFileSync(file, 'utf8').split('\n');
      lines.forEach((line, index) => {
        const code = line.replace(/^\s*(\/\/|\*|\/\*).*$/, '');
        for (const { pattern, name } of FORBIDDEN) {
          if (pattern.test(code)) {
            offenders.push(`${file.replace(/.*\/src\//, 'src/')}:${index + 1} uses ${name}`);
          }
        }
      });
    }

    expect(offenders).toEqual([]);
  });
});
