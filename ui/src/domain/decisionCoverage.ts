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
import { cellMatcher, columnNamesOf, comparedNames, comparesNumbers, needsQuotes, understandsCell } from './decisionCells';
import { columnSamples, type Sample } from './decisionSamples';
import { newRuleRow, type DecisionInputColumn, type DecisionRuleRow } from './decisionTable';

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
  /**
   * Of those, the ones that compare numbers without being a number column —
   * a new column is Text — which a change of type makes readable.
   */
  needsNumberType: string[];
  /** Of those, the ones holding unquoted text the engine cannot read either. */
  needsQuotes: string[];
  /**
   * What the unchecked column is compared with: other columns, by their
   * headings, and variables of the decision, by name. Nothing is wrong with
   * such a cell; which cases it decides depends on a value no check can know.
   */
  comparedWith: string[];
  /**
   * Of those, the variables no condition column reads. Try it has a box for
   * each condition and nothing else, so it cannot give them a value.
   */
  outsideTable: string[];
}

const MAX_COMBINATIONS = 400;
const MAX_REPORTED_GAPS = 10;

/**
 * Finds combinations of inputs that no line matches.
 *
 * Only whether a line applies matters to coverage, so outputs are not
 * considered.
 */
export function findCoverageGaps(inputs: DecisionInputColumn[], rules: DecisionRuleRow[]): CoverageReport {
  const empty: CoverageReport = {
    gaps: [],
    truncated: false,
    notAnalysed: [],
    needsNumberType: [],
    needsQuotes: [],
    comparedWith: [],
    outsideTable: [],
  };
  if (inputs.length === 0 || rules.length === 0) return empty;

  const names = columnNamesOf(inputs);
  const columns: Sample[][] = [];

  for (let index = 0; index < inputs.length; index += 1) {
    const input = inputs[index];
    const cells = rules.map((rule) => rule.input_entries[index] ?? '');
    if (cells.some((cell) => !understandsCell(cell, input.type, names))) {
      return { ...empty, ...whyUnread(input, cells, inputs) };
    }
    columns.push(columnSamples(input, cells));
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
  return { ...empty, gaps, truncated: examined < total };
}

/** The parts of a report that say why a column went unread. */
function whyUnread(
  input: DecisionInputColumn,
  cells: string[],
  inputs: DecisionInputColumn[],
): Pick<CoverageReport, 'notAnalysed' | 'needsNumberType' | 'needsQuotes' | 'comparedWith' | 'outsideTable'> {
  const label = input.label || input.expression;
  const names = columnNamesOf(inputs);
  const needsNumber = input.type !== 'number' && cells.some(comparesNumbers);
  const unquoted = cells.some((cell) => needsQuotes(cell, names));
  const compared = [...new Set(cells.flatMap((cell) => comparedNames(cell, names)))];
  const headingOf = (name: string) => {
    const column = inputs.find((candidate) => candidate.expression.trim() === name);
    return column?.label || name;
  };
  return {
    notAnalysed: [label],
    needsNumberType: needsNumber ? [label] : [],
    needsQuotes: unquoted ? [label] : [],
    comparedWith: compared.map(headingOf),
    outsideTable: compared.filter((name) => !names.has(name)),
  };
}

/**
 * Why the check said nothing, and what would let it: "not checked" alone leaves
 * the author guessing, and the commonest reason — comparing numbers in a
 * column that is not a number column — has a one-click fix.
 */
export function whyNotChecked(report: CoverageReport): string {
  const columns = report.notAnalysed.join(', ');
  if (report.needsNumberType.length > 0) {
    return `Not checked: ${report.needsNumberType.join(', ')} compares numbers but is not a Number column. Make it a Number column and the check can read it.`;
  }
  if (report.needsQuotes.length > 0) {
    return `Not checked: ${report.needsQuotes.join(', ')} has text the engine cannot read without quotes. Put it in quotes, as in "Gold Member".`;
  }
  // Not a fault in the table: naming another column is how a condition
  // compares two inputs. Only this check cannot follow it.
  if (report.comparedWith.length > 0) {
    const why = `Not checked: ${columns} is compared with ${joinWords(report.comparedWith)}, whose value changes from case to case, so this check cannot tell whether every case is decided.`;
    // Try it has a box for each condition and nothing else, so it is only
    // worth suggesting when every value compared with is one of them.
    return report.outsideTable.length === 0
      ? `${why} Try it with the values you care about.`
      : `${why} Try it can only set the conditions of this table, not ${joinWords(report.outsideTable)}.`;
  }
  return `Not checked: ${columns} uses a condition this check cannot read, so it cannot tell whether every case is decided.`;
}

function joinWords(words: string[]): string {
  if (words.length === 1) return words[0];
  return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
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
