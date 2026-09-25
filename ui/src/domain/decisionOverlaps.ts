/**
 * Which lines of a decision table apply to the same case, and what that costs
 * under the table's hit policy.
 *
 * The check this replaces compared lines by their text, so it caught a line
 * pasted twice and nothing else. `> 10` beside `> 20` both apply to 25, and
 * under "Only one line may match" that fails the decision for every such case:
 * at runtime, in an instance, long after the table was saved. Lines are
 * compared here by what they match, read with the coverage analysis's matcher
 * and its samples, which stand for every value a table can tell apart.
 *
 * Like that analysis it says nothing it cannot back. A cell in notation the
 * matcher does not read makes the answer unknown, and unknown is not reported:
 * a false alarm teaches people to ignore the check.
 */
import { cellMatcher, columnSamples, understandsCell, type Sample } from './decisionCoverage';
import type { TableProblem } from './decisionProblems';
import {
  ANY_VALUE,
  hitPolicyOf,
  normalizeCell,
  parseOutputValue,
  type DecisionInputColumn,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from './decisionTable';

/**
 * Pairs are listed a few at a time. A table where every line overlaps every
 * other has a problem per pair, and fifty alerts bury the one sentence that
 * says what to do about them.
 */
const MAX_REPORTED_PAIRS = 5;

/**
 * Which of a column's samples one cell accepts, a bit per sample.
 *
 * A banded column has a sample either side of every threshold, so a long table
 * has hundreds of them, and every pair of lines is compared on each column.
 * Bits make that a few machine words per pair rather than a walk over arrays.
 */
type SampleSet = Uint32Array;

/** One condition column, read once for every line of the table. */
interface ColumnReading {
  label: string;
  samples: Sample[];
  /** Per line, which samples its cell accepts; null when the matcher cannot read the cell. */
  accepts: (SampleSet | null)[];
  /** Per line, true when the cell accepts anything. */
  wild: boolean[];
  /** Per line, the cell with its spelling differences removed. */
  normalized: string[];
}

/** Whether something holds; undefined when the matcher cannot tell. */
type Verdict = boolean | undefined;

export function findOverlaps(
  hitPolicy: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
): TableProblem[] {
  if (rules.length < 2) return [];
  const catchAlls = rules.flatMap((rule, index) => (isCatchAll(rule) ? [index] : []));
  const read = () => inputs.map((input, index) => readColumn(input, index, rules));

  switch (hitPolicy) {
    case 'FIRST':
      return [...catchAllHidesTheRest(catchAlls, rules.length), ...shadowedLines(read(), rules, catchAlls[0])];
    case 'UNIQUE':
      return [...catchAllsApplyWithEverything(catchAlls), ...overlappingPairs(read(), rules, catchAlls)];
    case 'ANY':
      return [
        ...catchAllDisagreements(catchAlls, outputs, rules),
        ...disagreeingPairs(read(), outputs, rules, catchAlls),
        ...repeatedConditions(rules, (a, b) => sameResults(rules[a], rules[b], outputs)),
      ];
    default:
      // Ranking and collecting policies are built for lines that overlap. A
      // line written twice is still worth a word: it is usually a paste.
      return repeatedConditions(rules, () => true);
  }
}

function readColumn(input: DecisionInputColumn, index: number, rules: DecisionRuleRow[]): ColumnReading {
  const cells = rules.map((rule) => rule.input_entries[index] ?? '');
  const readable = (cell: string) => understandsCell(cell, input.type);
  const samples = columnSamples(input, cells.filter(readable));
  return {
    label: input.label || input.expression,
    samples,
    accepts: cells.map((cell) => (readable(cell) ? acceptedSamples(cell, input.type, samples) : null)),
    wild: cells.map(isWildcard),
    normalized: cells.map(normalizeCell),
  };
}

function acceptedSamples(cell: string, type: string, samples: Sample[]): SampleSet {
  const accepts = cellMatcher(cell, type);
  const set: SampleSet = new Uint32Array(Math.max(1, Math.ceil(samples.length / 32)));
  samples.forEach((sample, index) => {
    if (accepts(sample.value)) set[index >>> 5] |= 1 << (index & 31);
  });
  return set;
}

/** The first sample both sets hold, or -1 when they hold none in common. */
function firstShared(a: SampleSet, b: SampleSet): number {
  for (let word = 0; word < a.length; word += 1) {
    const both = a[word] & b[word];
    if (both !== 0) return word * 32 + (31 - Math.clz32(both & -both));
  }
  return -1;
}

function isSubset(inner: SampleSet, outer: SampleSet): boolean {
  return inner.every((word, index) => (word & ~outer[index]) === 0);
}

function isWildcard(cell: string): boolean {
  return cell.trim() === '' || cell.trim() === ANY_VALUE;
}

/** A line with nothing in any condition: it applies to every case. */
function isCatchAll(rule: DecisionRuleRow): boolean {
  return rule.input_entries.every(isWildcard);
}

/** Whether a line can match anything at all, as far as the matcher can tell. */
function canMatch(columns: ColumnReading[], line: number): boolean {
  return columns.every((column) => column.accepts[line]?.some((word) => word !== 0) ?? true);
}

/** Whether some value satisfies both cells, and the first one that does. */
function meetInColumn(column: ColumnReading, a: number, b: number): { verdict: Verdict; example?: Sample } {
  const first = column.accepts[a];
  const second = column.accepts[b];
  if (first && second) {
    const index = firstShared(first, second);
    return index < 0 ? { verdict: false } : { verdict: true, example: column.samples[index] };
  }
  // One side is unreadable. A wildcard still meets it, and so does the same text.
  if (column.wild[a] || column.wild[b]) return { verdict: true };
  if (!first && !second && column.normalized[a] === column.normalized[b]) return { verdict: true };
  return { verdict: undefined };
}

/**
 * Whether two lines both apply to at least one case, and that case in words.
 *
 * A line applies when every one of its cells does, so they meet only if they
 * meet in every column; one column known to keep them apart settles it, however
 * unreadable the others are.
 */
function linesMeet(columns: ColumnReading[], a: number, b: number): { verdict: Verdict; example?: string } {
  let unknown = false;
  const parts: (string | undefined)[] = [];
  for (const column of columns) {
    const { verdict, example } = meetInColumn(column, a, b);
    if (verdict === false) return { verdict: false };
    if (verdict === undefined) unknown = true;
    // A column neither line constrains adds nothing to the example.
    else if (!(column.wild[a] && column.wild[b])) parts.push(example && `${column.label} is ${example.label}`);
  }
  if (unknown) return { verdict: undefined };
  const complete = parts.length > 0 && parts.every((part) => part !== undefined);
  return { verdict: true, example: complete ? parts.join(' and ') : undefined };
}

/** Whether every case the inner line's cell accepts, the outer line's accepts too. */
function coversInColumn(column: ColumnReading, outer: number, inner: number): Verdict {
  const wide = column.accepts[outer];
  const narrow = column.accepts[inner];
  if (wide && narrow) return isSubset(narrow, wide);
  if (column.wild[outer]) return true;
  if (!wide && !narrow && column.normalized[outer] === column.normalized[inner]) return true;
  return undefined;
}

function covers(columns: ColumnReading[], outer: number, inner: number): boolean {
  return columns.every((column) => coversInColumn(column, outer, inner) === true);
}

/** Every pair of lines, earlier first, that are both real candidates for a case. */
function candidatePairs(columns: ColumnReading[], rules: DecisionRuleRow[], excluded: number[]): [number, number][] {
  const lines = rules.flatMap((_, index) => (!excluded.includes(index) && canMatch(columns, index) ? [index] : []));
  return lines.flatMap((a, position) => lines.slice(position + 1).map((b): [number, number] => [a, b]));
}

function when(example: string | undefined): string {
  return example ? `when ${example}` : 'to some of the same cases';
}

function overlappingPairs(columns: ColumnReading[], rules: DecisionRuleRow[], catchAlls: number[]): TableProblem[] {
  const problems = candidatePairs(columns, rules, catchAlls).flatMap(([a, b]): TableProblem[] => {
    const meeting = linesMeet(columns, a, b);
    if (meeting.verdict !== true) return [];
    return [
      {
        severity: 'error',
        message: `Lines ${a + 1} and ${b + 1} both apply ${when(meeting.example)}, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.`,
      },
    ];
  });
  return listFew(problems, (rest) => `${rest} more ${rest === 1 ? 'pair' : 'pairs'} of lines overlap the same way.`);
}

function disagreeingPairs(
  columns: ColumnReading[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
  catchAlls: number[],
): TableProblem[] {
  const problems = candidatePairs(columns, rules, catchAlls).flatMap(([a, b]): TableProblem[] => {
    if (sameResults(rules[a], rules[b], outputs)) return [];
    const meeting = linesMeet(columns, a, b);
    if (meeting.verdict !== true) return [];
    return [
      {
        severity: 'error',
        message: `Lines ${a + 1} and ${b + 1} both apply ${when(meeting.example)} but give different results. Lines that apply together must agree, so the decision fails there.`,
      },
    ];
  });
  return listFew(problems, (rest) => `${rest} more ${rest === 1 ? 'pair' : 'pairs'} of lines disagree the same way.`);
}

/**
 * Lines an earlier line hides completely, when the first match wins.
 *
 * Lines below a catch-all are left to its own warning, which already says none
 * of them can be reached.
 */
function shadowedLines(columns: ColumnReading[], rules: DecisionRuleRow[], firstCatchAll = rules.length): TableProblem[] {
  const problems = rules.slice(1, firstCatchAll).flatMap((_, offset): TableProblem[] => {
    const line = offset + 1;
    if (!canMatch(columns, line)) return [];
    const hider = rules.slice(0, line).findIndex((__, earlier) => covers(columns, earlier, line));
    if (hider < 0) return [];
    return [
      {
        severity: 'warning',
        message: `Line ${line + 1} can never be reached: line ${hider + 1} comes before it and applies to every case it does. Move it above line ${hider + 1}, or remove it.`,
      },
    ];
  });
  return listFew(problems, (rest) => `${rest} more ${rest === 1 ? 'line' : 'lines'} can never be reached for the same reason.`);
}

/** Lines whose conditions say the same thing, where the policy tolerates overlap. */
function repeatedConditions(rules: DecisionRuleRow[], alsoSame: (a: number, b: number) => boolean): TableProblem[] {
  const seen = new Map<string, number>();
  const problems: TableProblem[] = [];
  rules.forEach((rule, index) => {
    const signature = rule.input_entries.map(normalizeCell).join('\u0000');
    const previous = seen.get(signature);
    if (previous === undefined) {
      seen.set(signature, index);
    } else if (alsoSame(previous, index)) {
      problems.push({ severity: 'warning', message: `Lines ${previous + 1} and ${index + 1} test the same conditions.` });
    }
  });
  return problems;
}

/** The first few problems, and one more that counts the rest. */
function listFew(problems: TableProblem[], countTheRest: (rest: number) => string): TableProblem[] {
  if (problems.length <= MAX_REPORTED_PAIRS) return problems;
  const rest = problems.length - MAX_REPORTED_PAIRS;
  return [...problems.slice(0, MAX_REPORTED_PAIRS), { severity: problems[0].severity, message: countTheRest(rest) }];
}

/**
 * Only when the first match wins does a catch-all hide the lines below it, and
 * only the first catch-all needs saying: everything under it, other catch-alls
 * included, is already out of reach.
 */
function catchAllHidesTheRest(catchAlls: number[], lineCount: number): TableProblem[] {
  const first = catchAlls[0];
  if (first === undefined || first === lineCount - 1) return [];
  return [
    {
      severity: 'warning',
      message: `Line ${first + 1} matches everything, so no line below it can ever be reached. Catch-all lines belong last.`,
    },
  ];
}

/** Under UNIQUE a catch-all applies alongside every other line, wherever it sits. */
function catchAllsApplyWithEverything(catchAlls: number[]): TableProblem[] {
  const firstWins = hitPolicyOf('FIRST')?.label ?? 'FIRST';
  return catchAlls.map((index) => ({
    severity: 'error',
    message: `Line ${index + 1} matches everything, so it applies alongside every other line, and only one line may match: every case another line decides fails the decision. Give it conditions of its own, or choose “${firstWins}” and keep it last.`,
  }));
}

/**
 * Under ANY the lines a catch-all applies alongside must give its result. Two
 * catch-alls that disagree are named once, by the first of them.
 */
function catchAllDisagreements(catchAlls: number[], outputs: DecisionOutputColumn[], rules: DecisionRuleRow[]): TableProblem[] {
  return catchAlls.flatMap((catchAll): TableProblem[] => {
    const alreadyNamed = (index: number) => index < catchAll && catchAlls.includes(index);
    const disagreeing = rules.flatMap((rule, index) =>
      index !== catchAll && !alreadyNamed(index) && !sameResults(rules[catchAll], rule, outputs) ? [index + 1] : [],
    );
    if (disagreeing.length === 0) return [];
    const which = disagreeing.length === 1 ? `line ${disagreeing[0]} gives` : `lines ${joinNumbers(disagreeing)} give`;
    return [
      {
        severity: 'error',
        message: `Line ${catchAll + 1} matches everything, so it applies alongside every other line, and ${which} a different result. Lines that apply together must agree, so those cases fail the decision.`,
      },
    ];
  });
}

/**
 * Whether two lines produce the same values, compared as the engine compares
 * them: as stored, so `"LOW"` and `LOW` are the same result.
 */
function sameResults(a: DecisionRuleRow, b: DecisionRuleRow, outputs: DecisionOutputColumn[]): boolean {
  return outputs.every(
    (output, index) =>
      parseOutputValue(a.output_entries[index] ?? '', output.type) ===
      parseOutputValue(b.output_entries[index] ?? '', output.type),
  );
}

function joinNumbers(numbers: number[]): string {
  if (numbers.length === 1) return String(numbers[0]);
  return `${numbers.slice(0, -1).join(', ')} and ${numbers[numbers.length - 1]}`;
}
