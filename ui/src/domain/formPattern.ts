/**
 * A field's `validation.pattern`, made safe to run in an approver's browser.
 *
 * The pattern comes from a deployed definition — untrusted input — and was
 * compiled with `new RegExp` inside the change and submit handlers. A pattern
 * that does not compile threw there, so pressing Submit silently did nothing;
 * and a pattern like `(a+)+$` backtracks exponentially, which freezes the tab
 * on a few dozen characters of input. JavaScript offers no time limit on a
 * match, so the only defence is not to run such a pattern at all.
 *
 * A refused pattern is skipped, not failed: the person filling the form cannot
 * fix the modeller's pattern, and failing it would leave the task impossible
 * to complete. The server is still the authority on what it accepts.
 */

/** Longer than any format check a modeller writes by hand. */
export const MAX_PATTERN_LENGTH = 256;

const compiled = new Map<string, RegExp | null>();

/** The compiled pattern, or null when it is refused. Each refusal is logged once. */
export function compileFieldPattern(pattern: string): RegExp | null {
  const known = compiled.get(pattern);
  if (known !== undefined) return known;

  const refusal = refusePattern(pattern);
  let result: RegExp | null = null;
  if (refusal) {
    console.warn(`A field pattern was refused, so the field is not being checked by it (${refusal}): ${pattern.slice(0, 80)}`);
  } else {
    try {
      result = new RegExp(pattern);
    } catch (error) {
      console.warn(
        `A field pattern does not compile, so the field is not being checked by it: ${pattern.slice(0, 80)}`,
        error instanceof Error ? error.message : error,
      );
    }
  }
  compiled.set(pattern, result);
  return result;
}

/** Why a pattern is refused before compiling, or null when it may run. */
function refusePattern(pattern: string): string | null {
  if (pattern.length > MAX_PATTERN_LENGTH) {
    return `longer than ${MAX_PATTERN_LENGTH} characters`;
  }
  if (hasNestedQuantifier(pattern)) {
    return 'a repeated group that itself repeats can take exponential time';
  }
  return null;
}

/**
 * Whether a group containing an unbounded quantifier is itself quantified —
 * `(a+)+`, `(\w*)*`, `(x+y)+{2,}` — the shape behind most catastrophic
 * backtracking. A heuristic: it does not see overlapping alternatives such as
 * `(a|a)*`, but it catches the patterns people actually write by accident.
 */
function hasNestedQuantifier(pattern: string): boolean {
  // One entry per open group: whether an unbounded quantifier appeared in it.
  const groups: boolean[] = [];
  let inClass = false;
  for (let i = 0; i < pattern.length; i++) {
    const char = pattern[i];
    if (char === '\\') {
      i += 1;
      continue;
    }
    if (inClass) {
      if (char === ']') inClass = false;
      continue;
    }
    if (char === '[') {
      inClass = true;
    } else if (char === '(') {
      groups.push(false);
    } else if (char === ')') {
      const repeatsInside = groups.pop() ?? false;
      const repeated = isUnboundedQuantifier(pattern, i + 1);
      if (repeatsInside && repeated) return true;
      if ((repeatsInside || repeated) && groups.length > 0) groups[groups.length - 1] = true;
    } else if (isUnboundedQuantifier(pattern, i) && groups.length > 0) {
      groups[groups.length - 1] = true;
    }
  }
  return false;
}

/** Whether the quantifier at `at` is `+`, `*` or `{n,}` / `{n,m}` with a large m. */
function isUnboundedQuantifier(pattern: string, at: number): boolean {
  const char = pattern[at];
  if (char === '+' || char === '*') return true;
  if (char !== '{') return false;
  const match = /^\{(\d+)(,(\d*))?\}/.exec(pattern.slice(at));
  if (!match || match[2] === undefined) return false;
  return match[3] === '' || Number(match[3]) > 10;
}
