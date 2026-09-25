/**
 * "Try it" in the decision editor: which stored table it runs, with which
 * values, and which lines of the grid its answer points at.
 *
 * Try it runs what the server holds, not what is on screen, so each of those
 * answers has to be pinned to the stored table. The page used to work them out
 * inline from whatever was on screen at the time, and got each of them wrong in
 * a different way.
 */
import type { ProcessVariables } from '../services/types';
import type { DecisionInputColumn } from './decisionTable';

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
