/**
 * The editor's two table checks, keyed by what they read.
 *
 * Both used to run on every keystroke anywhere in the grid: typing a result or
 * a note re-ran the overlap check on a table whose conditions had not changed,
 * and on a long table that was a pause per character. Each check is asked here
 * for a key made of exactly what it reads — the conditions, the columns, the
 * hit policy, and the results only where the policy compares them — so the
 * editor runs it again only when that key changes, and can hand the key to
 * useDeferredValue so that typing never waits for it.
 *
 * The key is the check's input written out, not a hash of it: the check is run
 * from the key itself, so the result can never belong to a table other than
 * the one the key describes.
 */
import { findCoverageGaps, type CoverageReport } from './decisionCoverage';
import { findOverlaps } from './decisionOverlaps';
import type { TableProblem } from './decisionProblems';
import type { DecisionInputColumn, DecisionOutputColumn, DecisionRuleRow } from './decisionTable';

type Column = [label: string, expression: string, type: string];

interface OverlapCheck {
  hitPolicy: string;
  columns: Column[];
  /** Only under ANY, the one policy that compares results. */
  results: [name: string, type: string][];
  lines: [conditions: string[], results: string[]][];
}

interface CoverageCheck {
  columns: Column[];
  conditions: string[][];
}

const columnsOf = (inputs: DecisionInputColumn[]): Column[] =>
  inputs.map((input) => [input.label, input.expression, input.type]);

/** Whether a policy's overlap check reads the results of the lines it compares. */
const readsResults = (hitPolicy: string) => hitPolicy === 'ANY';

export function overlapCheckKey(
  hitPolicy: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
): string {
  const results = readsResults(hitPolicy);
  const check: OverlapCheck = {
    hitPolicy,
    columns: columnsOf(inputs),
    results: results ? outputs.map((output) => [output.name, output.type]) : [],
    lines: rules.map((rule) => [rule.input_entries, results ? rule.output_entries : []]),
  };
  return JSON.stringify(check);
}

export function overlapsFor(key: string): TableProblem[] {
  const check = JSON.parse(key) as OverlapCheck;
  return findOverlaps(
    check.hitPolicy,
    check.columns.map(([label, expression, type], index) => ({ id: String(index), label, expression, type })),
    check.results.map(([name, type], index) => ({ id: String(index), label: name, name, type })),
    check.lines.map(([conditions, results], index) => ({
      id: String(index),
      input_entries: conditions,
      output_entries: results,
    })),
  );
}

export function coverageCheckKey(inputs: DecisionInputColumn[], rules: DecisionRuleRow[]): string {
  const check: CoverageCheck = { columns: columnsOf(inputs), conditions: rules.map((rule) => rule.input_entries) };
  return JSON.stringify(check);
}

export function coverageFor(key: string): CoverageReport {
  const check = JSON.parse(key) as CoverageCheck;
  return findCoverageGaps(
    check.columns.map(([label, expression, type], index) => ({ id: String(index), label, expression, type })),
    check.conditions.map((conditions, index) => ({ id: String(index), input_entries: conditions, output_entries: [] })),
  );
}
