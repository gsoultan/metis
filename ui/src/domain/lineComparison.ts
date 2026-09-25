/**
 * Two lines of a decision table, compared by what they match.
 *
 * Every condition column is read once, for every line, into the set of the
 * column's samples each cell accepts: the samples stand for every value the
 * table can tell apart, so two cells that share no sample share no value. The
 * overlap check asks two questions of a pair of lines — do they both apply to
 * some case, and does one apply to every case the other does — and both are
 * answered here, a column at a time.
 *
 * A cell in notation the matcher does not read makes an answer unknown rather
 * than false, and the check reports nothing it cannot back.
 */
import { cellMatcher, columnSamples, understandsCell, type Sample } from './decisionCoverage';
import { ANY_VALUE, normalizeCell, type DecisionInputColumn, type DecisionRuleRow } from './decisionTable';
import {
  firstShared,
  isEmpty,
  isSubset,
  numberSampleSet,
  samplePositions,
  sampleSet,
  WorkBudget,
  type SampleSet,
} from './sampleSets';

/** One condition column, read once for every line of the table. */
export interface ColumnReading {
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
export type Verdict = boolean | undefined;

export function readColumns(inputs: DecisionInputColumn[], rules: DecisionRuleRow[]): ColumnReading[] {
  return inputs.map((input, index) => readColumn(input, index, rules));
}

function readColumn(input: DecisionInputColumn, index: number, rules: DecisionRuleRow[]): ColumnReading {
  const cells = rules.map((rule) => rule.input_entries[index] ?? '');
  const readable = (cell: string) => understandsCell(cell, input.type);
  const samples = columnSamples(input, cells.filter(readable));
  const positions = input.type === 'number' ? samplePositions(samples) : undefined;
  const read = (cell: string): SampleSet => {
    const accepts = cellMatcher(cell, input.type);
    return positions ? numberSampleSet(samples, positions, cell, accepts) : sampleSet(samples, accepts);
  };
  return {
    label: input.label || input.expression,
    samples,
    accepts: cells.map((cell) => (readable(cell) ? read(cell) : null)),
    wild: cells.map(isWildcard),
    normalized: cells.map(normalizeCell),
  };
}

export function isWildcard(cell: string): boolean {
  return cell.trim() === '' || cell.trim() === ANY_VALUE;
}

/** Whether a line can match anything at all, as far as the matcher can tell. */
export function canMatch(columns: ColumnReading[], line: number): boolean {
  return columns.every((column) => {
    const accepts = column.accepts[line];
    return accepts === null || !isEmpty(accepts);
  });
}

/**
 * Whether some column keeps two lines apart without looking inside the sets:
 * the words holding one cell's samples all come before the other's. In a
 * banded table that is nearly every pair, and it costs two comparisons.
 */
function apartAtAGlance(columns: ColumnReading[], a: number, b: number): boolean {
  for (const column of columns) {
    const first = column.accepts[a];
    const second = column.accepts[b];
    if (first && second && (first.last < second.first || second.last < first.first)) return true;
  }
  return false;
}

/** Whether some value satisfies both cells, and the first sample that does when the matcher can say. */
function meetInColumn(column: ColumnReading, a: number, b: number, budget: WorkBudget): { verdict: Verdict; shared?: number } {
  const first = column.accepts[a];
  const second = column.accepts[b];
  if (first && second) {
    const shared = firstShared(first, second, budget);
    return shared < 0 ? { verdict: false } : { verdict: true, shared };
  }
  // One side is unreadable. A wildcard still meets it, and so does the same text.
  if (column.wild[a] || column.wild[b]) return { verdict: true };
  if (!first && !second && column.normalized[a] === column.normalized[b]) return { verdict: true };
  return { verdict: undefined };
}

/**
 * Whether two lines both apply to at least one case.
 *
 * A line applies when every one of its cells does, so they meet only if they
 * meet in every column; one column known to keep them apart settles it, however
 * unreadable the others are.
 */
export function linesMeet(columns: ColumnReading[], a: number, b: number, budget: WorkBudget): Verdict {
  if (apartAtAGlance(columns, a, b)) {
    budget.spend(1);
    return false;
  }
  let unknown = false;
  for (const column of columns) {
    const { verdict } = meetInColumn(column, a, b, budget);
    if (verdict === false) return false;
    if (verdict === undefined) unknown = true;
  }
  return unknown ? undefined : true;
}

/**
 * A case two meeting lines both apply to, in words — "Amount is 21 and Tier is
 * GOLD" — or undefined when some column cannot name one. Asked only of the few
 * pairs that are listed, rather than of every pair that meets.
 */
export function meetingExample(columns: ColumnReading[], a: number, b: number): string | undefined {
  const unmetered = new WorkBudget(Number.POSITIVE_INFINITY);
  const parts: (string | undefined)[] = [];
  for (const column of columns) {
    // A column neither line constrains adds nothing to the example.
    if (column.wild[a] && column.wild[b]) continue;
    const { shared } = meetInColumn(column, a, b, unmetered);
    parts.push(shared === undefined ? undefined : `${column.label} is ${column.samples[shared].label}`);
  }
  const complete = parts.length > 0 && parts.every((part) => part !== undefined);
  return complete ? parts.join(' and ') : undefined;
}

/** Whether every case the inner line's cell accepts, the outer line's accepts too. */
function coversInColumn(column: ColumnReading, outer: number, inner: number, budget: WorkBudget): Verdict {
  const wide = column.accepts[outer];
  const narrow = column.accepts[inner];
  if (wide && narrow) return isSubset(narrow, wide, budget);
  if (column.wild[outer]) return true;
  if (!wide && !narrow && column.normalized[outer] === column.normalized[inner]) return true;
  return undefined;
}

/** Whether the outer line applies to every case the inner one does, in every column. */
export function covers(columns: ColumnReading[], outer: number, inner: number, budget: WorkBudget): boolean {
  return columns.every((column) => coversInColumn(column, outer, inner, budget) === true);
}
