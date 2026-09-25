/**
 * What is wrong with a decision table, in the words of whoever has to fix it.
 *
 * Kept apart from decisionTable.ts, which is the table's vocabulary: the checks
 * need more than the vocabulary — the overlap check reads cells with the same
 * matcher the coverage analysis uses — and that matcher itself depends on the
 * vocabulary. Living here keeps the dependencies pointing one way.
 */
import {
  ANY_VALUE,
  hitPolicyOf,
  normalizeCell,
  parseOutputValue,
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

  problems.push(...catchAllProblems(hitPolicy, outputs, rules));

  rules.forEach((rule, index) => {
    if (rule.output_entries.every((cell) => cell.trim() === '')) {
      problems.push({ severity: 'warning', message: `Line ${index + 1} has no result.` });
    }
  });

  // Two lines with identical conditions are a copy-paste, and under UNIQUE they
  // are a runtime failure rather than a smell.
  const seen = new Map<string, number>();
  rules.forEach((rule, index) => {
    const signature = rule.input_entries.map(normalizeCell).join('\u0000');
    const previous = seen.get(signature);
    if (previous !== undefined) {
      problems.push({
        severity: hitPolicy === 'UNIQUE' ? 'error' : 'warning',
        message: `Lines ${previous + 1} and ${index + 1} test the same conditions.`,
      });
    } else {
      seen.set(signature, index);
    }
  });

  return problems;
}

/** A line with nothing in any condition: it applies to every case. */
function isCatchAll(rule: DecisionRuleRow): boolean {
  return rule.input_entries.every((cell) => cell.trim() === '' || cell.trim() === ANY_VALUE);
}

/**
 * What a catch-all line costs, which depends on the hit policy.
 *
 * Only when the first match wins does it hide the lines below it. Under UNIQUE
 * it applies alongside every other line, so any case another line decides
 * fails the decision, wherever the catch-all sits. Under ANY the lines it
 * applies alongside must agree with it. A policy that ranks or collects its
 * matches simply counts it among them, which is what a default is for.
 */
function catchAllProblems(hitPolicy: string, outputs: DecisionOutputColumn[], rules: DecisionRuleRow[]): TableProblem[] {
  if (rules.length < 2) return [];
  const catchAlls = rules.flatMap((rule, index) => (isCatchAll(rule) ? [index] : []));

  switch (hitPolicy) {
    case 'FIRST': {
      // Only the first one: everything below it, other catch-alls included, is
      // already out of reach.
      const first = catchAlls[0];
      if (first === undefined || first === rules.length - 1) return [];
      return [
        {
          severity: 'warning',
          message: `Line ${first + 1} matches everything, so no line below it can ever be reached. Catch-all lines belong last.`,
        },
      ];
    }
    case 'UNIQUE': {
      const firstWins = hitPolicyOf('FIRST')?.label ?? 'FIRST';
      return catchAlls.map((index) => ({
        severity: 'error',
        message: `Line ${index + 1} matches everything, so it applies alongside every other line, and only one line may match: every case another line decides fails the decision. Give it conditions of its own, or choose “${firstWins}” and keep it last.`,
      }));
    }
    case 'ANY':
      return catchAlls.flatMap((index) => disagreementWithCatchAll(index, outputs, rules));
    default:
      return [];
  }
}

function disagreementWithCatchAll(catchAll: number, outputs: DecisionOutputColumn[], rules: DecisionRuleRow[]): TableProblem[] {
  const disagreeing = rules.flatMap((rule, index) =>
    index !== catchAll && !sameResults(rules[catchAll], rule, outputs) ? [index + 1] : [],
  );
  if (disagreeing.length === 0) return [];
  const which = disagreeing.length === 1 ? `line ${disagreeing[0]} gives` : `lines ${joinNumbers(disagreeing)} give`;
  return [
    {
      severity: 'error',
      message: `Line ${catchAll + 1} matches everything, so it applies alongside every other line, and ${which} a different result. Lines that apply together must agree, so those cases fail the decision.`,
    },
  ];
}

/**
 * Whether two lines produce the same values, compared as the engine compares
 * them: as stored, so `"LOW"` and `LOW` are the same result.
 */
function sameResults(a: DecisionRuleRow, b: DecisionRuleRow, outputs: DecisionOutputColumn[]): boolean {
  return outputs.every(
    (output, index) =>
      parseOutputValue(a.output_entries[index] ?? '', output.type) ===
      parseOutputValue(b.output_entries[index] ?? '', output.type),
  );
}

function joinNumbers(numbers: number[]): string {
  if (numbers.length === 1) return String(numbers[0]);
  return `${numbers.slice(0, -1).join(', ')} and ${numbers[numbers.length - 1]}`;
}
