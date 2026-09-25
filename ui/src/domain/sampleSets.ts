/**
 * Which of a column's samples a cell accepts, as bits, and what comparing two
 * of them costs.
 *
 * The overlap check compares every pair of lines on every column. A banded
 * column has a sample either side of every threshold, so a long table has
 * thousands of them: bits make a comparison a walk over machine words rather
 * than over arrays, and the span — the first and last word holding a bit —
 * settles most pairs without the walk. Two bands of a banded table sit in
 * different words, so asking whether they share a value is two comparisons of
 * word numbers. Without the span the check took most of a second on a table of
 * two thousand lines, on every keystroke.
 */
import { thresholdsIn, type CellTest, type Sample } from './decisionCoverage';

/** The samples one cell accepts: a bit per sample, and the words that hold any. */
export interface SampleSet {
  bits: Uint32Array;
  /** The first word holding a bit; past `last` when the set is empty. */
  first: number;
  /** The last word holding a bit. */
  last: number;
}

/**
 * How much pairwise work the check may do before it says it stopped short.
 *
 * Counted in words compared, one per pair at the least, because that is what
 * the time goes on. Past it the check reports how far it got rather than
 * holding up the editor: a long table of lines that all overlap costs a word or
 * two per pair, and there are millions of pairs.
 */
export class WorkBudget {
  private remaining: number;

  constructor(units: number) {
    this.remaining = units;
  }

  spend(units: number): void {
    this.remaining -= units;
  }

  get exhausted(): boolean {
    return this.remaining < 0;
  }
}

export function sampleSet(samples: Sample[], accepts: (value: Sample['value']) => boolean): SampleSet {
  const bits = new Uint32Array(Math.max(1, Math.ceil(samples.length / 32)));
  let first = bits.length;
  let last = -1;
  samples.forEach((sample, index) => {
    if (!accepts(sample.value)) return;
    const word = index >>> 5;
    bits[word] |= 1 << (index & 31);
    first = Math.min(first, word);
    last = Math.max(last, word);
  });
  return { bits, first, last };
}

/**
 * The same set, for a cell of a number column, asked far fewer questions.
 *
 * A number column's samples ascend: one at every number any cell mentions and
 * one in each stretch between. A cell's answer can change only at the numbers
 * it mentions itself, so it is asked once per stretch between those and the
 * answer written across the stretch a word at a time. Asking every cell about
 * every sample made reading a banded table of thousands of lines take longer
 * than comparing its pairs.
 */
export function numberSampleSet(samples: Sample[], positions: Map<number, number>, cell: string, accepts: CellTest): SampleSet {
  const marks = thresholdsIn([cell]).map((value) => positions.get(value));
  if (marks.some((position) => position === undefined)) return sampleSet(samples, accepts);
  const bits = new Uint32Array(Math.max(1, Math.ceil(samples.length / 32)));
  const set: SampleSet = { bits, first: bits.length, last: -1 };
  let from = 0;
  for (const mark of [...(marks as number[]), samples.length]) {
    fillIf(set, samples, from, mark - 1, accepts);
    if (mark < samples.length) fillIf(set, samples, mark, mark, accepts);
    from = mark + 1;
  }
  return set;
}

/** Sets samples from..to when the cell accepts the first of them, and so all of them. */
function fillIf(set: SampleSet, samples: Sample[], from: number, to: number, accepts: CellTest): void {
  if (from > to || !accepts(samples[from].value)) return;
  for (let word = from >>> 5; word <= to >>> 5; word += 1) {
    const low = word === from >>> 5 ? from & 31 : 0;
    const high = word === to >>> 5 ? to & 31 : 31;
    set.bits[word] |= (high === 31 ? 0xffffffff : (1 << (high + 1)) - 1) & ~((1 << low) - 1);
  }
  set.first = Math.min(set.first, from >>> 5);
  set.last = Math.max(set.last, to >>> 5);
}

/** Where each sample value sits, for numberSampleSet. */
export function samplePositions(samples: Sample[]): Map<number, number> {
  return new Map(samples.flatMap((sample, index) => (typeof sample.value === 'number' ? [[sample.value, index]] : [])));
}

export function isEmpty(set: SampleSet): boolean {
  return set.first > set.last;
}

/** The first sample both sets hold, or -1 when they hold none in common. */
export function firstShared(a: SampleSet, b: SampleSet, budget: WorkBudget): number {
  const from = Math.max(a.first, b.first);
  const to = Math.min(a.last, b.last);
  for (let word = from; word <= to; word += 1) {
    const both = a.bits[word] & b.bits[word];
    if (both !== 0) {
      budget.spend(1 + word - from);
      return word * 32 + (31 - Math.clz32(both & -both));
    }
  }
  budget.spend(1 + Math.max(0, to - from + 1));
  return -1;
}

/** Whether every sample the inner set holds, the outer one holds too. */
export function isSubset(inner: SampleSet, outer: SampleSet, budget: WorkBudget): boolean {
  if (isEmpty(inner)) return true;
  if (inner.first < outer.first || inner.last > outer.last) {
    budget.spend(1);
    return false;
  }
  for (let word = inner.first; word <= inner.last; word += 1) {
    if ((inner.bits[word] & ~outer.bits[word]) !== 0) {
      budget.spend(1 + word - inner.first);
      return false;
    }
  }
  budget.spend(1 + inner.last - inner.first);
  return true;
}
