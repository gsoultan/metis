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
import { ANY_VALUE, type DecisionInputColumn, type DecisionRuleRow } from './decisionTable';

/** One combination of inputs that no line covers. */
export interface CoverageGap {
  /** The values, in column order. */
  values: string[];
  /** The gap in words, for someone who did not write the table. */
  description: string;
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

const MAX_SAMPLES_PER_COLUMN = 8;
const MAX_COMBINATIONS = 400;
const MAX_REPORTED_GAPS = 10;

/** A value to try, and how to say it. */
interface Sample {
  /** What the matcher sees. */
  value: string | number | boolean;
  /** What a person reads. */
  label: string;
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
    if (cells.some((cell) => !isUnderstood(cell))) {
      notAnalysed.push(inputs[index].label || inputs[index].expression);
      return { ...empty, notAnalysed };
    }
    columns.push(samplesFor(inputs[index], cells));
  }

  const total = columns.reduce((product, column) => product * Math.max(column.length, 1), 1);
  const truncated = total > MAX_COMBINATIONS;

  const gaps: CoverageGap[] = [];
  let examined = 0;

  const walk = (position: number, chosen: Sample[]) => {
    if (gaps.length >= MAX_REPORTED_GAPS || examined >= MAX_COMBINATIONS) return;
    if (position === columns.length) {
      examined += 1;
      if (!rules.some((rule) => ruleMatches(rule, chosen, inputs))) {
        gaps.push({
          values: chosen.map((sample) => sample.label),
          description: describeGap(inputs, chosen),
        });
      }
      return;
    }
    for (const sample of columns[position]) {
      walk(position + 1, [...chosen, sample]);
    }
  };
  walk(0, []);

  return { gaps, truncated, notAnalysed };
}

/** Whether this analysis understands a cell well enough to trust its verdict. */
function isUnderstood(cell: string): boolean {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return true;
  if (/^(>=|<=|>|<|!=|=)?\s*-?\d+(\.\d+)?$/.test(text)) return true;
  if (/^[[\]]\s*-?[\d.]+\s*\.\.\s*-?[\d.]+\s*[[\]]$/.test(text)) return true;
  if (/^(true|false)$/i.test(text)) return true;
  if (/^not\(.+\)$/.test(text)) return true;
  // A list, or a bare or quoted literal.
  return text.split(',').every((part) => /^\s*("[^"]*"|'[^']*'|[\w .-]+)\s*$/.test(part));
}

/** The values worth trying for one column. */
function samplesFor(column: DecisionInputColumn, cells: string[]): Sample[] {
  if (column.type === 'boolean') {
    return [
      { value: true, label: 'yes' },
      { value: false, label: 'no' },
    ];
  }

  if (column.type === 'number') {
    const bounds = new Set<number>();
    for (const cell of cells) {
      for (const match of cell.matchAll(/-?\d+(\.\d+)?/g)) {
        bounds.add(Number(match[0]));
      }
    }
    const sorted = [...bounds].sort((a, b) => a - b);
    const points = new Set<number>();
    // Below everything, then each boundary and just past it. That set catches an
    // off-by-one at a threshold, which is the commonest gap there is.
    points.add((sorted[0] ?? 0) - 1);
    for (const bound of sorted) {
      points.add(bound);
      points.add(bound + 1);
    }
    return [...points]
      .sort((a, b) => a - b)
      .slice(0, MAX_SAMPLES_PER_COLUMN)
      .map((value) => ({ value, label: String(value) }));
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
  const samples: Sample[] = [...literals]
    .slice(0, MAX_SAMPLES_PER_COLUMN - 1)
    .map((value) => ({ value, label: value }));
  samples.push({ value: 'anything-else-entirely', label: 'anything else' });
  return samples;
}

function ruleMatches(rule: DecisionRuleRow, chosen: Sample[], inputs: DecisionInputColumn[]): boolean {
  return chosen.every((sample, index) => cellMatches(rule.input_entries[index] ?? '', sample.value, inputs[index].type));
}

/**
 * Whether one cell accepts one value.
 *
 * A partial reimplementation of the unary tests the engine runs, covering what
 * isUnderstood admits and nothing more.
 */
export function cellMatches(cell: string, value: string | number | boolean, type: string): boolean {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return true;

  if (type === 'boolean') {
    if (/^true$/i.test(text)) return value === true;
    if (/^false$/i.test(text)) return value === false;
    return false;
  }

  const negated = text.match(/^not\((.+)\)$/);
  if (negated) return !cellMatches(negated[1], value, type);

  if (type === 'number' && typeof value === 'number') {
    const range = text.match(/^([[\]])\s*(-?[\d.]+)\s*\.\.\s*(-?[\d.]+)\s*([[\]])$/);
    if (range) {
      const [, open, low, high, close] = range;
      const lowOk = open === '[' ? value >= Number(low) : value > Number(low);
      // `]` closes inclusively and `[` closes exclusively — the DMN spelling.
      const highOk = close === ']' ? value <= Number(high) : value < Number(high);
      return lowOk && highOk;
    }
    const comparison = text.match(/^(>=|<=|>|<|!=|=)?\s*(-?[\d.]+)$/);
    if (comparison) {
      const [, operator = '=', operand] = comparison;
      const bound = Number(operand);
      switch (operator) {
        case '>':
          return value > bound;
        case '<':
          return value < bound;
        case '>=':
          return value >= bound;
        case '<=':
          return value <= bound;
        case '!=':
          return value !== bound;
        default:
          return value === bound;
      }
    }
  }

  // A list, or a single literal.
  return text
    .split(',')
    .map((part) => unquote(part.trim()))
    .some((literal) => literal === String(value));
}

function unquote(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length >= 2 && (trimmed.startsWith('"') || trimmed.startsWith("'")) && trimmed.endsWith(trimmed[0])) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function describeGap(inputs: DecisionInputColumn[], chosen: Sample[]): string {
  const parts = chosen.map((sample, index) => `${inputs[index].label || inputs[index].expression} is ${sample.label}`);
  return `Nothing decides when ${parts.join(' and ')}`;
}
