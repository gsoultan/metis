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
import type { DecisionInputColumn, DecisionRuleRow } from './decisionTable';

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

/** The variables Try it sends: one per condition column as the table stands, under the name it reads now. */
export function trialVariables(inputs: DecisionInputColumn[], values: TrialValues): ProcessVariables {
  return Object.fromEntries(inputs.map((input) => [input.expression, trialValue(input, trialValueOf(values, input))]));
}

function trialValue(column: DecisionInputColumn, raw: string): string | number | boolean {
  if (column.type === 'boolean') return raw === 'true';
  if (column.type === 'number' && raw.trim() !== '' && !Number.isNaN(Number(raw))) return Number(raw);
  return raw;
}

/** A Try-it answer, and the table that was on screen when it was asked. */
export interface TrialOutcome {
  values: Record<string, unknown>;
  /** The stored lines that decided, by id. */
  ruleIds: string[];
  /** The same lines, by position in the stored table. */
  positions: number[];
  /** tableFingerprint of the table on screen when it ran. */
  table: string;
  /** Whether that was the stored table, which is what ran. */
  ranAsShown: boolean;
}

/** The parts of a table that decide its answers, as one comparable string. */
export function tableFingerprint(payload: CreateDecisionPayload): string {
  const { hit_policy, aggregation, required_decisions, inputs, outputs, rules } = payload;
  return JSON.stringify({ hit_policy, aggregation, required_decisions, inputs, outputs, rules });
}

/**
 * Where a Try-it answer stands against the table on screen: current, an answer
 * about the saved version when the screen held changes that were not saved,
 * or stale once the table has changed since it ran.
 */
export type TrialStanding = 'current' | 'saved-only' | 'stale';

export function trialStanding(outcome: TrialOutcome, table: string): TrialStanding {
  if (outcome.table !== table) return 'stale';
  return outcome.ranAsShown ? 'current' : 'saved-only';
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
export function matchedLines(outcome: TrialOutcome | null, rules: DecisionRuleRow[], table: string): number[] {
  if (!outcome || trialStanding(outcome, table) === 'stale') return [];
  if (outcome.ruleIds.length > 0) {
    const decided = new Set(outcome.ruleIds);
    return rules.flatMap((rule, index) => (decided.has(rule.id) ? [index] : []));
  }
  return outcome.ranAsShown ? outcome.positions.filter((position) => position < rules.length) : [];
}

/** The lines an answer points at, as a heading: "Line 3 matched", "Lines 2 and 5 matched". */
export function describeMatchedLines(lines: number[]): string {
  if (lines.length === 0) return 'A line of the saved version matched';
  const numbers = lines.map((line) => line + 1);
  if (numbers.length === 1) return `Line ${numbers[0]} matched`;
  return `Lines ${numbers.slice(0, -1).join(', ')} and ${numbers[numbers.length - 1]} matched`;
}
