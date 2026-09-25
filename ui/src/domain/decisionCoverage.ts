/**
 * What a decision table forgot.
 *
 * A table is a promise that every case has an answer, and the way that promise
 * breaks is quiet: some combination of inputs matches no line, the decision
 * returns nothing, and a process carries on with an empty variable until
 * something downstream fails for an unrelated-looking reason.
 *
 * Nobody finds those by reading the grid. They are found by trying values, and
 * the values worth trying are the ones on and around the boundaries the table
 * itself mentions — if a line says `> 1000`, then 1000 and 1001 are where the
 * interesting behaviour is.
 *
 * This is a deliberately partial analysis. It understands the cell notations
 * people actually write, and refuses to report on a table containing anything
 * else — erring towards saying nothing rather than towards a false alarm,
 * because a coverage warning that is wrong teaches people to ignore coverage
 * warnings.
 */
import { ANY_VALUE, newRuleRow, type DecisionInputColumn, type DecisionRuleRow } from './decisionTable';

/** One combination of inputs that no line covers. */
export interface CoverageGap {
  /** The values, in column order. */
  values: string[];
  /** The gap in words, for someone who did not write the table. */
  description: string;
  /**
   * Per column, the condition that covers this gap and nothing more: a line
   * made of them decides exactly the cases that are missing, so it overlaps no
   * existing line under any hit policy.
   */
  conditions: string[];
}

export interface CoverageReport {
  gaps: CoverageGap[];
  /**
   * True when the search was cut short. Reported rather than hidden: a silent
   * cap reads as "your table is fine", which is the opposite of what it means.
   */
  truncated: boolean;
  /** Columns whose notation this analysis did not understand. */
  notAnalysed: string[];
}

const MAX_COMBINATIONS = 400;
const MAX_REPORTED_GAPS = 10;

/** A value to try, and how to say it. */
export interface Sample {
  /** What the matcher sees. */
  value: string | number | boolean;
  /** What a person reads. */
  label: string;
  /**
   * Every value the cells cannot tell apart from this one, in words: "more
   * than 10 but less than 20". What holds for the sample holds for all of them.
   */
  standsFor: string;
  /** The condition that matches exactly what the sample stands for. */
  condition: string;
}

/**
 * Finds combinations of inputs that no line matches.
 *
 * Only whether a line applies matters to coverage, so outputs are not
 * considered.
 */
export function findCoverageGaps(inputs: DecisionInputColumn[], rules: DecisionRuleRow[]): CoverageReport {
  const empty: CoverageReport = { gaps: [], truncated: false, notAnalysed: [] };
  if (inputs.length === 0 || rules.length === 0) return empty;

  const notAnalysed: string[] = [];
  const columns: Sample[][] = [];

  for (let index = 0; index < inputs.length; index += 1) {
    const cells = rules.map((rule) => rule.input_entries[index] ?? '');
    if (cells.some((cell) => !understandsCell(cell))) {
      notAnalysed.push(inputs[index].label || inputs[index].expression);
      return { ...empty, notAnalysed };
    }
    columns.push(columnSamples(inputs[index], cells));
  }

  const total = columns.reduce((product, column) => product * Math.max(column.length, 1), 1);

  const gaps: CoverageGap[] = [];
  let examined = 0;
  // Every cell is read once, not once per combination it is asked about.
  const lines = rules.map((rule) => inputs.map((input, index) => cellMatcher(rule.input_entries[index] ?? '', input.type)));

  const walk = (position: number, chosen: Sample[]) => {
    if (gaps.length >= MAX_REPORTED_GAPS || examined >= MAX_COMBINATIONS) return;
    if (position === columns.length) {
      examined += 1;
      if (!lines.some((tests) => chosen.every((sample, index) => tests[index](sample.value)))) {
        gaps.push({
          values: chosen.map((sample) => sample.label),
          description: describeGap(inputs, chosen),
          conditions: chosen.map((sample) => sample.condition),
        });
      }
      return;
    }
    for (const sample of columns[position]) {
      walk(position + 1, [...chosen, sample]);
    }
  };
  walk(0, []);

  // Cut short means some combination was never looked at, whichever limit
  // stopped the walk. Each column's values used to be trimmed to the first
  // eight as well, and that was the one limit nobody was told about.
  return { gaps, truncated: examined < total, notAnalysed };
}

/** Whether this analysis understands a cell well enough to trust its verdict. */
export function understandsCell(cell: string): boolean {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return true;
  if (/^(>=|<=|>|<|!=|=)?\s*-?\d+(\.\d+)?$/.test(text)) return true;
  if (/^[[\]]\s*-?[\d.]+\s*\.\.\s*-?[\d.]+\s*[[\]]$/.test(text)) return true;
  if (/^(true|false)$/i.test(text)) return true;
  // not( ) is only as readable as what it negates. It used to be accepted
  // whatever it held, and `not(sum(items) > 10)` read as "anything but that
  // text": a condition that matches nearly everything, and an overlap error
  // that blocked Save over a table the check could not read.
  const negated = text.match(/^not\((.+)\)$/);
  if (negated) return understandsCell(negated[1]) && !isWildcardText(negated[1]);
  // A list, or a bare or quoted literal.
  return text.split(',').every((part) => /^\s*("[^"]*"|'[^']*'|[\w .-]+)\s*$/.test(part));
}

function isWildcardText(text: string): boolean {
  return text.trim() === '' || text.trim() === ANY_VALUE;
}

/**
 * The values worth trying for one column: one from every group of values its
 * cells cannot tell apart, so that what holds for the sample holds for the
 * group. The overlap check reads cells with the same samples.
 */
export function columnSamples(column: DecisionInputColumn, cells: string[]): Sample[] {
  if (column.type === 'boolean') {
    return [
      { value: true, label: 'yes', standsFor: 'yes', condition: 'true' },
      { value: false, label: 'no', standsFor: 'no', condition: 'false' },
    ];
  }

  if (column.type === 'number') {
    return numberSamples(thresholdsIn(cells));
  }

  // Text: every literal the table mentions, plus one value it does not, which is
  // how a missing catch-all shows up.
  const literals = new Set<string>();
  for (const cell of cells) {
    for (const part of cell.split(',')) {
      const literal = unquote(part.trim().replace(/^not\(/, '').replace(/\)$/, ''));
      if (literal && literal !== ANY_VALUE) literals.add(literal);
    }
  }
  const named = [...literals];
  const samples: Sample[] = named.map((value) => ({ value, label: value, standsFor: value, condition: quoted(value) }));
  samples.push({
    value: 'anything-else-entirely',
    label: 'anything else',
    standsFor: 'anything else',
    condition: named.length > 0 ? `not(${named.map(quoted).join(', ')})` : ANY_VALUE,
  });
  return samples;
}

/**
 * Text as a cell spells it. Quoted, so that a word which happens to name a
 * process variable is still read as the word; the other kind of quote when the
 * text holds a double quote, which it can only have come from a cell quoted
 * that way.
 */
function quoted(text: string): string {
  return text.includes('"') ? `'${text}'` : `"${text}"`;
}

/** Every number a column's cells mention, lowest first. */
function thresholdsIn(cells: string[]): number[] {
  const thresholds = new Set<number>();
  for (const cell of cells) {
    for (const match of cell.matchAll(/-?\d+(\.\d+)?/g)) {
      thresholds.add(Number(match[0]));
    }
  }
  return [...thresholds].sort((a, b) => a - b);
}

/**
 * One value from each stretch of the number line that no cell tells apart:
 * below every threshold, each threshold itself, one value strictly between
 * each pair of neighbours, and one above them all.
 *
 * Every cell this analysis reads changes its answer only at a threshold, so a
 * value from a stretch speaks for the whole stretch. The threshold itself is
 * there because that is where an off-by-one hides, the commonest gap there is.
 */
function numberSamples(thresholds: number[]): Sample[] {
  if (thresholds.length === 0) return [numberSample(-1, 'any number', ANY_VALUE)];
  const lowest = thresholds[0];
  const samples = [numberSample(lowest - 1, `less than ${lowest}`, `< ${lowest}`)];
  thresholds.forEach((threshold, index) => {
    samples.push(numberSample(threshold, String(threshold), String(threshold)));
    const next = thresholds[index + 1];
    samples.push(
      next === undefined
        ? numberSample(threshold + 1, `more than ${threshold}`, `> ${threshold}`)
        : numberSample(
            strictlyBetween(threshold, next),
            `more than ${threshold} but less than ${next}`,
            `]${threshold}..${next}[`,
          ),
    );
  });
  return samples;
}

function numberSample(value: number, standsFor: string, condition: string): Sample {
  return { value, label: String(value), standsFor, condition };
}

/**
 * The next whole number when there is room for one, which reads naturally,
 * and the midpoint when there is not. Plus one used to be tried regardless, and
 * past a threshold less than one away it skipped the stretch in between.
 */
function strictlyBetween(low: number, high: number): number {
  if (low + 1 < high) return low + 1;
  // Rounded so that 0.1 and 0.2 give 0.15 rather than 0.15000000000000002.
  return Number(((low + high) / 2).toPrecision(12));
}

/** Whether a cell accepts a value. */
export type CellTest = (value: string | number | boolean) => boolean;

/**
 * Reads a cell into a test of whether it accepts a value.
 *
 * A partial reimplementation of the unary tests the engine runs, covering what
 * understandsCell admits and nothing more.
 *
 * The cell is read once and the test put to many values: the overlap check
 * asks every cell of a column about every value worth trying, which for a long
 * banded table is hundreds per cell, and reading the cell again for each of
 * them was most of the check's time.
 */
export function cellMatcher(cell: string, type: string): CellTest {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return () => true;

  if (type === 'boolean') {
    // Only the lower-case words are yes and no to the engine. `TRUE` is a bare
    // word, which it reads as text, and text is never equal to a boolean.
    if (text === 'true') return (value) => value === true;
    if (text === 'false') return (value) => value === false;
    return () => false;
  }

  const negated = text.match(/^not\((.+)\)$/);
  if (negated) {
    const inner = cellMatcher(negated[1], type);
    return (value) => !inner(value);
  }

  const numeric = type === 'number' ? numberTest(text) : undefined;
  // A list, or a single literal. Compared with its type, as the engine
  // compares: `"10"` is text and never equals the number 10, and a bare `10`
  // is a number and never equals the text "10".
  const literals = text.split(',').map(literalValue);
  return (value) => (numeric && typeof value === 'number' ? numeric(value) : literals.includes(value));
}

/** One literal as the engine reads it: quoted text, a number, true or false, or a bare word read as text. */
function literalValue(part: string): string | number | boolean {
  const text = part.trim();
  if (text.length >= 2 && (text.startsWith('"') || text.startsWith("'")) && text.endsWith(text[0])) {
    return text.slice(1, -1);
  }
  if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
  if (text === 'true' || text === 'false') return text === 'true';
  return text;
}

/** A range or a comparison, when that is what the cell is. */
function numberTest(text: string): ((value: number) => boolean) | undefined {
  const range = text.match(/^([[\]])\s*(-?[\d.]+)\s*\.\.\s*(-?[\d.]+)\s*([[\]])$/);
  if (range) {
    const [, open, low, high, close] = range;
    const [from, to] = [Number(low), Number(high)];
    // `]` closes inclusively and `[` closes exclusively — the DMN spelling.
    return (value) => (open === '[' ? value >= from : value > from) && (close === ']' ? value <= to : value < to);
  }
  const comparison = text.match(/^(>=|<=|>|<|!=|=)?\s*(-?[\d.]+)$/);
  if (!comparison) return undefined;
  const [, operator = '=', operand] = comparison;
  const bound = Number(operand);
  switch (operator) {
    case '>':
      return (value) => value > bound;
    case '<':
      return (value) => value < bound;
    case '>=':
      return (value) => value >= bound;
    case '<=':
      return (value) => value <= bound;
    case '!=':
      return (value) => value !== bound;
    default:
      return (value) => value === bound;
  }
}

function unquote(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length >= 2 && (trimmed.startsWith('"') || trimmed.startsWith("'")) && trimmed.endsWith(trimmed[0])) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function describeGap(inputs: DecisionInputColumn[], chosen: Sample[]): string {
  const parts = chosen.map((sample, index) => `${inputs[index].label || inputs[index].expression} is ${sample.standsFor}`);
  return `Nothing decides when ${parts.join(' and ')}`;
}

/**
 * The line that decides a gap: its conditions cover the missing cases and
 * nothing else, and its results are left empty, because what the table should
 * decide there is the author's call — the editor points out a line with no
 * result until it has one.
 */
export function ruleForGap(gap: CoverageGap, id: string, outputCount: number): DecisionRuleRow {
  return { ...newRuleRow(id, gap.conditions.length, outputCount), input_entries: [...gap.conditions] };
}
