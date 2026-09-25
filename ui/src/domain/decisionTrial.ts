/**
 * "Try it" in the decision editor: which stored table it runs, with which
 * values, and which lines of the grid its answer points at.
 *
 * Try it runs what the server holds, not what is on screen, so each of those
 * answers has to be pinned to the stored table. The page used to work them out
 * inline from whatever was on screen at the time, and got each of them wrong in
 * a different way.
 */
import type { CreateDecisionPayload, ProcessVariables } from '../services/types';
import { parseOutputValue, type DecisionInputColumn, type DecisionRuleRow } from './decisionTable';

/** A stored decision, as much of one as Try it needs to name it. */
export interface TrialTarget {
  key: string;
  version: number;
}

/**
 * The table Try it runs: the one the editor loaded, at the version it loaded.
 *
 * The page sent the key typed into the editor, with no version. An edited key
 * names another table or none, and saving does not change a stored key; with
 * no version the server answers with the newest table of that key, which need
 * not be the one on screen. A table that was never saved has nothing to run.
 */
export function trialTarget(saved: TrialTarget | undefined): TrialTarget | null {
  return saved ? { key: saved.key, version: saved.version } : null;
}

/**
 * What has been typed into Try it, by column id.
 *
 * By id, not by the variable the column reads: the values were kept under the
 * variable name, so renaming a column's variable emptied its box while the old
 * value was still sent under the old name, and a removed column's value was
 * sent as well.
 */
export type TrialValues = Record<string, string>;

export function trialValueOf(values: TrialValues, column: DecisionInputColumn): string {
  return values[column.id] ?? '';
}

export function withTrialValue(values: TrialValues, column: DecisionInputColumn, text: string): TrialValues {
  return { ...values, [column.id]: text };
}

/**
 * The variables Try it sends: one per condition column as the table stands,
 * under the name it reads now.
 *
 * A column left blank is left out. A yes/no question nobody answered used to
 * be sent as "no", and an empty box as empty text: answers nobody gave, to a
 * table whose whole job is to tell those cases apart. Blank means "not given",
 * as it does for the examples saved with the table.
 */
export function trialVariables(inputs: DecisionInputColumn[], values: TrialValues): ProcessVariables {
  return Object.fromEntries(
    inputs
      .filter((input) => trialValueOf(values, input).trim() !== '')
      // Read the way an example saved with the table reads it (testsToPayload):
      // the same value typed in either place is the same question. Try it kept
      // the quotes and spaces around text, and read `1e3` and `0x10` as numbers
      // the engine's own notation does not have.
      .map((input) => [input.expression, parseOutputValue(trialValueOf(values, input), input.type)]),
  );
}

/** A Try-it answer, and the two tables it was an answer about. */
export interface TrialOutcome {
  values: Record<string, unknown>;
  /** The stored lines that decided, by id. */
  ruleIds: string[];
  /** The same lines, by position in the stored table. */
  positions: number[];
  /** tableFingerprint of the table on screen when it ran. */
  table: string;
  /**
   * tableFingerprint of the stored table when it ran — the one that answered.
   * A save replaces it, and the answer is then about a table nobody holds.
   */
  savedTable: string | null;
}

/** What the server answered, as much of it as an outcome keeps. */
export interface TrialResponse {
  result?: { values?: Record<string, unknown> };
  matchedRules: number[];
  matchedRuleIds: string[];
}

/** An answer, pinned to the table on screen and the stored table as they were when it ran. */
export function trialOutcome(response: TrialResponse, table: string, savedTable: string | null): TrialOutcome {
  return {
    values: response.result?.values ?? {},
    ruleIds: response.matchedRuleIds,
    positions: response.matchedRules,
    table,
    savedTable,
  };
}

function ranAsShown(outcome: TrialOutcome): boolean {
  return outcome.table === outcome.savedTable;
}

/**
 * The parts of a table that decide its answers, as one comparable string.
 *
 * Headings and notes are left out: they change nothing the table decides, and
 * relabelling a column or writing a note made an answer stale and cleared its
 * highlight.
 */
export function tableFingerprint(payload: CreateDecisionPayload): string {
  const { hit_policy, aggregation, required_decisions, inputs, outputs, rules } = payload;
  return JSON.stringify({
    hit_policy,
    aggregation,
    required_decisions,
    inputs: inputs?.map(({ id, expression, type }) => ({ id, expression, type })),
    outputs: outputs?.map(({ id, name, type, values }) => ({ id, name, type, values })),
    rules: rules?.map(({ id, inputs: conditions, outputs: results }) => ({ id, conditions, results })),
  });
}

/**
 * Where a Try-it answer stands against the table on screen: current, an answer
 * about the saved version when the screen held changes that were not saved,
 * or stale once either table has changed since it ran.
 *
 * The stored table counts as much as the one on screen. It was left out, so an
 * answer from before a save stayed up after it, still saying the changes were
 * not saved, about a stored table the save had replaced.
 */
export type TrialStanding = 'current' | 'saved-only' | 'stale';

export function trialStanding(outcome: TrialOutcome, table: string, savedTable: string | null): TrialStanding {
  if (outcome.table !== table || outcome.savedTable !== savedTable) return 'stale';
  return ranAsShown(outcome) ? 'current' : 'saved-only';
}

/** Why a stale answer is not shown, and what brings one back. */
export function staleNote(table: string, savedTable: string | null): string {
  return table === savedTable
    ? 'The saved table has changed since this ran, so its answer is no longer shown. Run it again.'
    : 'The table has changed since this ran, so its answer is no longer shown. Save, and run it again.';
}

/**
 * The lines on screen a Try-it answer points at.
 *
 * By id, because an id names the same line wherever it has moved, and a
 * position only counts lines in the stored table. Positions are used only for
 * lines stored without an id, and only when the stored table is what is on
 * screen. Nothing once the table has changed since it ran: the answer was
 * about a table that is no longer there.
 */
export function matchedLines(
  outcome: TrialOutcome | null,
  rules: DecisionRuleRow[],
  table: string,
  savedTable: string | null,
): number[] {
  if (!outcome || trialStanding(outcome, table, savedTable) === 'stale') return [];
  if (outcome.ruleIds.length > 0) {
    const decided = new Set(outcome.ruleIds);
    return rules.flatMap((rule, index) => (decided.has(rule.id) ? [index] : []));
  }
  return ranAsShown(outcome) ? outcome.positions.filter((position) => position < rules.length) : [];
}

/** The lines an answer points at, as a heading: "Line 3 matched", "Lines 2 and 5 matched". */
export function describeMatchedLines(lines: number[]): string {
  if (lines.length === 0) return 'A line of the saved version matched';
  const numbers = lines.map((line) => line + 1);
  if (numbers.length === 1) return `Line ${numbers[0]} matched`;
  return `Lines ${numbers.slice(0, -1).join(', ')} and ${numbers[numbers.length - 1]} matched`;
}
