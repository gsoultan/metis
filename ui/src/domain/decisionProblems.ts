/**
 * What is wrong with a decision table, in the words of whoever has to fix it.
 *
 * Kept apart from decisionTable.ts, which is the table's vocabulary: the checks
 * need more than the vocabulary — the overlap check reads cells with the same
 * matcher the coverage analysis uses — and that matcher itself depends on the
 * vocabulary. Living here keeps the dependencies pointing one way.
 */
import { findOverlaps } from './decisionOverlaps';
import {
  hitPolicyOf,
  type DecisionInputColumn,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from './decisionTable';

/** One thing wrong with the table, in the words of whoever has to fix it. */
export interface TableProblem {
  severity: 'error' | 'warning';
  message: string;
}

/**
 * What is wrong with the table, before it is saved.
 *
 * These are the failures that otherwise surface as an error from a running
 * process, hours later, attributed to the process rather than to the table.
 */
export function findProblems(
  hitPolicy: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
): TableProblem[] {
  // Lines that apply to the same case: an error or a warning depending on the
  // hit policy, and the catch-all line is the loudest case of it.
  return [...findStructureProblems(hitPolicy, inputs, outputs, rules), ...findOverlaps(hitPolicy, inputs, outputs, rules)];
}

/**
 * Everything findProblems finds except overlapping lines: a line at a time,
 * cheap enough to run on every keystroke, where the overlap check compares
 * pairs of lines and is run only when what it reads changes (decisionChecks).
 */
export function findStructureProblems(
  hitPolicy: string,
  inputs: DecisionInputColumn[],
  outputs: DecisionOutputColumn[],
  rules: DecisionRuleRow[],
): TableProblem[] {
  const problems: TableProblem[] = [];
  const policy = hitPolicyOf(hitPolicy);

  if (policy?.needsValueList && !(outputs[0]?.values?.length)) {
    problems.push({
      severity: 'error',
      message: `“${policy.label}” ranks results by the list of allowed values on ${
        outputs[0]?.label || 'the first result column'
      }, and that list is empty. Add the values in order of importance, or choose another policy.`,
    });
  }

  outputs.forEach((output) => {
    if (!output.name.trim()) {
      problems.push({
        severity: 'error',
        message: `The result column “${output.label}” has no process variable, so nothing downstream can read it. Give it a name under the column heading.`,
      });
    }
  });

  inputs.forEach((input) => {
    if (!input.expression.trim()) {
      problems.push({
        severity: 'error',
        message: `The condition column “${input.label}” does not say which process variable it tests. Give it a name under the column heading.`,
      });
    }
  });

  if (rules.length === 0) {
    problems.push({ severity: 'warning', message: 'The table has no lines, so it will never decide anything.' });
  }

  rules.forEach((rule, index) => {
    if (rule.output_entries.every((cell) => cell.trim() === '')) {
      problems.push({ severity: 'warning', message: `Line ${index + 1} has no result.` });
    }
  });

  return problems;
}
